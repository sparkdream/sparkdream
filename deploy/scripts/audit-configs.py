#!/usr/bin/env python3
"""
Audit module-parameter consistency across:

  - config.yml                                        (local dev / ignite chain serve)
  - deploy/config/network/{devnet,testnet,mainnet}/config.yml
  - the CURRENT binary's schema, generated on the fly via `sparkdreamd init`

The schema reference is regenerated each run so a config.yml referencing a
newly-added param doesn't get falsely flagged as stale just because the
committed deploy genesis files haven't been regenerated yet. (That false
positive bites when prepare-release.sh runs the audit BEFORE the regen step,
which is the natural order — audit-then-mutate.) When `sparkdreamd` isn't
on PATH, the script falls back to deploy/config/network/devnet/genesis.json
and notes this in the report.

This script flags four kinds of drift:

  ERROR — config.yml or a deploy config references a parameter that the
          current binary doesn't expose (renamed or removed). Genesis
          regeneration would silently drop these on the floor.
  WARN  — testnet vs mainnet drift (one network customizes a param the
          other leaves at default — usually fine for tunables like
          `gov.voting_period`, but worth eyeballing).
  WARN  — A deploy config has a param key that root config.yml doesn't —
          unusual; root is normally a strict superset because every test
          override implies the binary default already differs from what we
          want for prod-realistic E2E.
  INFO  — root has a param key that deploy doesn't. Almost always
          intentional: root is the "fast test" mode (short cooldowns, large
          budgets) and prod inherits the binary default. Reported so a
          reviewer can sanity-check each one.

Optional `--check-snapshots` adds a fifth check: if config.yml has been
edited more recently than any module's `test/<module>/snapshots/post-setup/`
directory, that snapshot may not exercise the latest config and the
parallel runner will restore stale state. Reported as INFO per stale module.

Exit codes:
  0  no issues at the requested severity
  1  WARN or ERROR found (or ERROR in --strict mode)
  2  invocation error / missing files

Usage:
  deploy/scripts/audit-configs.py
  deploy/scripts/audit-configs.py --strict             # treat WARN as failure
  deploy/scripts/audit-configs.py --check-snapshots    # also flag stale per-module snapshots
  deploy/scripts/audit-configs.py --json               # machine-readable
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

import yaml

REPO_ROOT = Path(os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..")))


# --- color helpers --------------------------------------------------------

def _ansi(code: str, s: str) -> str:
    if not sys.stdout.isatty():
        return s
    return f"\033[{code}m{s}\033[0m"


def red(s):    return _ansi("31", s)
def green(s):  return _ansi("32", s)
def yellow(s): return _ansi("33", s)
def cyan(s):   return _ansi("36", s)
def bold(s):   return _ansi("1",  s)


# --- core comparison ------------------------------------------------------

CONFIGS = {
    "root":    REPO_ROOT / "config.yml",
    "devnet":  REPO_ROOT / "deploy/config/network/devnet/config.yml",
    "testnet": REPO_ROOT / "deploy/config/network/testnet/config.yml",
    "mainnet": REPO_ROOT / "deploy/config/network/mainnet/config.yml",
}

# Fallback schema reference when `sparkdreamd` isn't on PATH. devnet's
# committed genesis is closest to the testparams build's value set, but for
# schema (key-set) purposes any of the three works — proto fields are
# build-tag-agnostic.
DEPLOY_FALLBACK_GENESIS = REPO_ROOT / "deploy/config/network/devnet/genesis.json"


def _app_state(doc):
    """Extract genesis.app_state from an ignite config.yml shape."""
    return ((doc.get("genesis") or {}).get("app_state") or {})


def _params(state, mod):
    """Extract <module>.params from an app_state dict."""
    if not isinstance(state, dict):
        return None
    st = state.get(mod)
    if not isinstance(st, dict):
        return None
    p = st.get("params")
    return p if isinstance(p, dict) else None


def load_yaml(path: Path):
    if not path.exists():
        raise FileNotFoundError(f"missing required file: {path}")
    with path.open() as f:
        return yaml.safe_load(f)


def load_json(path: Path):
    if not path.exists():
        raise FileNotFoundError(f"missing required file: {path}")
    with path.open() as f:
        return json.load(f)


def generate_schema_reference():
    """Generate a fresh app_state by running `sparkdreamd init` in a tmpdir.

    The current binary on PATH is the authoritative source for what
    parameter keys the chain exposes RIGHT NOW. The committed deploy genesis
    files lag behind binary changes between releases — using them as the
    schema reference produces false-positive ERRORs for params that were
    just added to the proto and haven't been re-baked into the deploy
    artifacts yet.

    Build tag doesn't matter for schema purposes (proto-defined keys are
    identical across tags); we use whatever binary the developer happens to
    have built. Typical execution time is ~1s when the binary is already
    built.

    Returns (app_state_dict, source_label) on success, or (None, error_msg)
    if generation failed for any reason — caller falls back to the deploy
    genesis.
    """
    binary = shutil.which("sparkdreamd")
    if not binary:
        return None, "sparkdreamd not on PATH"

    tmp = tempfile.mkdtemp(prefix="audit-schema-")
    try:
        # `--overwrite` so we don't fail on a stale tmpdir if mktemp
        # collided. `--chain-id` is required; value is irrelevant.
        cmd = [binary, "init", "audit-probe",
               "--chain-id", "audit", "--home", tmp, "--overwrite"]
        result = subprocess.run(cmd, capture_output=True, timeout=30)
        if result.returncode != 0:
            err = (result.stderr or b"").decode(errors="replace")[:300]
            return None, f"sparkdreamd init failed: {err.strip() or 'no stderr'}"

        gen_path = Path(tmp) / "config" / "genesis.json"
        if not gen_path.exists():
            return None, f"sparkdreamd init didn't write {gen_path.name}"

        with gen_path.open() as f:
            doc = json.load(f)
        return doc.get("app_state", {}), f"fresh `sparkdreamd init` ({binary})"
    except (subprocess.TimeoutExpired, OSError) as e:
        return None, f"sparkdreamd init error: {e}"
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def find_module_snapshots():
    """Locate per-module post-setup snapshots used by test/run_parallel.sh.

    Each `test/<module>/snapshots/post-setup/sparkdream_data/` is the
    persisted chain home that the parallel runner restores into per-suite
    home directories. If config.yml has been edited more recently than the
    snapshot was saved, the snapshot's `setup_test_accounts.sh` ran against
    an older binary/config and the parallel suite will exercise stale
    parameters.

    Returns a list of (module_name, snapshot_genesis_path) for every module
    that has one. Empty if no snapshots exist yet.
    """
    out = []
    for mod_dir in sorted((REPO_ROOT / "test").iterdir()):
        if not mod_dir.is_dir():
            continue
        gen = mod_dir / "snapshots" / "post-setup" / "sparkdream_data" / "config" / "genesis.json"
        if gen.exists():
            out.append((mod_dir.name, gen))
    return out


# --- finding builders -----------------------------------------------------

class Findings:
    def __init__(self):
        self.error = []
        self.warn = []
        self.info = []
        self.schema_source = None  # human-readable: "fresh sparkdreamd init" or "fallback to deploy genesis"

    def add(self, level, msg):
        bucket = {"ERROR": self.error, "WARN": self.warn, "INFO": self.info}[level]
        bucket.append(msg)

    def to_dict(self):
        return {
            "error": self.error,
            "warn": self.warn,
            "info": self.info,
            "schema_source": self.schema_source,
        }


def audit(check_snapshots: bool) -> Findings:
    f = Findings()

    # --- 0. Schema reference: prefer freshly-generated, fall back to committed.

    schema_apps, source_label = generate_schema_reference()
    if schema_apps is None:
        # Couldn't run sparkdreamd init — fall back to the committed deploy
        # genesis. Add a WARN so the user knows the schema check might be
        # stale relative to uncommitted binary changes.
        try:
            gen_doc = load_json(DEPLOY_FALLBACK_GENESIS)
        except FileNotFoundError as e:
            f.add("ERROR", f"cannot run audit — no schema source: {source_label}; "
                           f"and fallback {e}")
            return f
        schema_apps = gen_doc.get("app_state", {})
        f.schema_source = f"deploy fallback ({DEPLOY_FALLBACK_GENESIS.relative_to(REPO_ROOT)}) — {source_label}"
        f.add("WARN",
              f"schema reference is the committed deploy genesis (reason: {source_label}). "
              f"This file lags behind the binary between releases; if you've added new params "
              f"since the last `regenerate-network-genesis.py` run, install/build sparkdreamd "
              f"so the audit can probe the current schema.")
    else:
        f.schema_source = source_label

    # --- 1. Stale-key check: every key in any config.yml must exist in the
    #       binary's current schema.

    configs_loaded = {}
    for name, path in CONFIGS.items():
        try:
            configs_loaded[name] = load_yaml(path)
        except FileNotFoundError as e:
            f.add("ERROR", f"missing config: {e}")
            return f

    # Walk every config and flag any param key the binary doesn't know.
    for cfg_name, doc in configs_loaded.items():
        apps = _app_state(doc)
        for mod, st in apps.items():
            cfg_p = _params({mod: st}, mod)
            if cfg_p is None:
                continue
            schema_p = _params(schema_apps, mod) or {}
            stale = sorted(set(cfg_p) - set(schema_p))
            for k in stale:
                f.add("ERROR",
                      f"{cfg_name}.{mod}.params.{k} — references a param the current binary doesn't expose "
                      f"(removed or renamed). Drop this key or re-add it to the proto schema.")

    # --- 2. testnet vs mainnet drift -----------------------------------------

    test_apps = _app_state(configs_loaded["testnet"])
    main_apps = _app_state(configs_loaded["mainnet"])
    all_modules = sorted(set(test_apps) | set(main_apps))
    for mod in all_modules:
        test_p = _params(test_apps, mod) or {}
        main_p = _params(main_apps, mod) or {}
        only_test = sorted(set(test_p) - set(main_p))
        only_main = sorted(set(main_p) - set(test_p))
        for k in only_test:
            f.add("WARN", f"testnet.{mod}.params.{k} — set in testnet but mainnet inherits binary default")
        for k in only_main:
            f.add("WARN", f"mainnet.{mod}.params.{k} — set in mainnet but testnet inherits binary default")

    # --- 3. deploy keys missing from root (unusual; root should be a superset)

    root_apps = _app_state(configs_loaded["root"])
    for net in ("devnet", "testnet", "mainnet"):
        net_apps = _app_state(configs_loaded[net])
        for mod in sorted(set(root_apps) | set(net_apps)):
            root_p = _params(root_apps, mod) or {}
            net_p  = _params(net_apps,  mod) or {}
            only_in_net = sorted(set(net_p) - set(root_p))
            for k in only_in_net:
                f.add("WARN",
                      f"{net}.{mod}.params.{k} — customized for prod but root config.yml leaves it at "
                      f"binary default. Consider whether E2E tests should also exercise this override.")

    # --- 4. root keys missing from deploy (INFO — almost always intentional)

    for mod in sorted(set(root_apps) | set(_app_state(configs_loaded["devnet"]))):
        root_p = _params(root_apps, mod) or {}
        for net in ("devnet", "testnet", "mainnet"):
            net_apps = _app_state(configs_loaded[net])
            net_p = _params(net_apps, mod) or {}
            only_in_root = sorted(set(root_p) - set(net_p))
            for k in only_in_root:
                v = root_p[k]
                f.add("INFO",
                      f"root.{mod}.params.{k} = {v!r} — only in root (deploy {net} inherits binary default; "
                      f"verify this is an intentional test-mode override).")

    # --- 5. Optional: per-module snapshot freshness ---------------------------

    if check_snapshots:
        cfg_path = CONFIGS["root"]
        try:
            cfg_mtime = cfg_path.stat().st_mtime
        except OSError as e:
            f.add("WARN", f"could not stat {cfg_path}: {e}")
            return f

        snapshots = find_module_snapshots()
        if not snapshots:
            f.add("INFO",
                  "no per-module snapshots found under test/<module>/snapshots/post-setup/ — "
                  "skipping freshness check (run `bash test/<module>/run_all_tests.sh --save-setup` "
                  "to create one, or `test/run_parallel.sh` which auto-snapshots).")
        else:
            stale = []
            fresh = []
            for module, gen_path in snapshots:
                try:
                    snap_mtime = gen_path.stat().st_mtime
                except OSError as e:
                    f.add("WARN", f"could not stat snapshot {gen_path}: {e}")
                    continue
                if cfg_mtime > snap_mtime:
                    delta_h = (cfg_mtime - snap_mtime) / 3600
                    stale.append((module, delta_h))
                else:
                    fresh.append(module)

            for module, delta_h in stale:
                f.add("INFO",
                      f"snapshot test/{module}/snapshots/post-setup/ is older than config.yml "
                      f"by {delta_h:.1f}h — re-run `bash test/{module}/run_all_tests.sh --save-setup --no-tests` "
                      f"to refresh, or it'll exercise stale params.")
            if fresh:
                f.add("INFO",
                      f"{len(fresh)} snapshot(s) up-to-date: {', '.join(sorted(fresh))}")

    return f


# --- output ---------------------------------------------------------------

def print_report(f: Findings, json_mode: bool):
    if json_mode:
        print(json.dumps(f.to_dict(), indent=2))
        return

    if f.schema_source:
        print(cyan(f"Schema source: {f.schema_source}"))

    def section(label, items, colorfn):
        if not items:
            return
        print(bold(colorfn(f"\n=== {label} ({len(items)}) ===")))
        for m in items:
            print(f"  {colorfn('•')} {m}")

    section("ERROR", f.error, red)
    section("WARN",  f.warn,  yellow)
    section("INFO",  f.info,  cyan)

    print()
    if not f.error and not f.warn:
        if f.info:
            print(green("[OK] No errors or warnings. ") + cyan(f"({len(f.info)} INFO line(s) above — review and move on.)"))
        else:
            print(green("[OK] All configs in sync, no drift detected."))
    else:
        bits = []
        if f.error: bits.append(red(f"{len(f.error)} ERROR"))
        if f.warn:  bits.append(yellow(f"{len(f.warn)} WARN"))
        if f.info:  bits.append(cyan(f"{len(f.info)} INFO"))
        print(bold(" / ".join(bits)) + " — see above.")


# --- main -----------------------------------------------------------------

def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--strict", action="store_true",
                   help="exit non-zero on any WARN (default: only ERROR)")
    p.add_argument("--check-snapshots", action="store_true",
                   help="flag per-module snapshots older than config.yml (test/<module>/snapshots/post-setup/)")
    p.add_argument("--json", action="store_true",
                   help="emit machine-readable JSON instead of human report")
    args = p.parse_args()

    f = audit(check_snapshots=args.check_snapshots)
    print_report(f, json_mode=args.json)

    if f.error:
        sys.exit(1)
    if args.strict and f.warn:
        sys.exit(1)
    sys.exit(0)


if __name__ == "__main__":
    main()
