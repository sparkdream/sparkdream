#!/bin/bash
# ============================================================================
# PREPARE-RELEASE — orchestrate the prerelease checklist
# ============================================================================
# Runs the four mechanical steps that go into every version bump:
#
#   1. BUMP DEPLOY VERSIONS — rewrite v<old> -> v<new> in the seven deploy
#      files (3× sentry.sdl.yaml, 3× validator.sdl.yaml, deploy/docs/DEPLOYMENT.md).
#   2. AUDIT CONFIGS        — run deploy/scripts/audit-configs.py to catch any
#      stale/drifted module parameters. Aborts on ERROR; INFO is shown but
#      doesn't block.
#   3. REGEN GENESIS        — run deploy/scripts/regenerate-network-genesis.py
#      to refresh devnet/testnet/mainnet genesis.json files from the current
#      params_vals_<network>.go and config.yml files.
#   4. VERIFY GENESIS       — run `make verify-genesis` to run the per-network
#      and cross-network audit tests.
#   5. BUILD ZK PROVER      — build the browser shield prover for this release
#      into build/zk/<version>/ (tools/zk/wasm/build.sh, stamped with the
#      version, plus the ceremony keys from build/ceremony and a manifest.json
#      of sha256 digests), after checking that the ceremony's verifying key is
#      the one in each network's genesis. Web clients serve this directory at
#      <zk assets>/<version>/ and refuse to prove when the wasm's version
#      differs from the node's, so publish it alongside every chain release.
#      The build is also copied to <zk publish dir>/<version>/, a web client's
#      zk asset directory: ../sparkdream-ui/public/zk (beside this repo) when
#      that checkout exists, or the directory given with --zk-publish.
#   6. E2E SNAPSHOTS        — nothing to do; post-setup snapshots are
#      fingerprinted on config.yml and the binary and refresh on their own.
#
# This script does NOT commit. It prints a diff summary and a suggested commit
# message at the end so you can stage and commit yourself.
#
# Usage:
#   deploy/scripts/prepare-release.sh <new-version>             # full pipeline
#   deploy/scripts/prepare-release.sh <new-version> --no-bump   # skip step 1 (re-run from step 2)
#   deploy/scripts/prepare-release.sh <new-version> --no-regen  # skip step 3+4
#   deploy/scripts/prepare-release.sh <new-version> --strict    # treat audit WARN as failure
#   deploy/scripts/prepare-release.sh <new-version> --no-zk     # skip step 5
#   deploy/scripts/prepare-release.sh <new-version> --zk-publish <dir>  # copy the prover to <dir>/<version>/
#   deploy/scripts/prepare-release.sh <new-version> --no-zk-publish     # build the prover but don't copy it
#   deploy/scripts/prepare-release.sh --help
#
# Examples:
#   deploy/scripts/prepare-release.sh v1.0.7
#   deploy/scripts/prepare-release.sh v1.0.7 --no-bump          # versions already bumped manually
#   deploy/scripts/prepare-release.sh v1.0.7 --zk-publish /srv/ui/public/zk
#
# Environment:
#   SKIP_DIRTY_CHECK=1   bypass the "working tree dirty" pre-flight check.
# ============================================================================

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
REPO_ROOT="$( cd "$SCRIPT_DIR/../.." && pwd )"
cd "$REPO_ROOT"

# --- color helpers ---------------------------------------------------------
if [ -t 1 ]; then
    BOLD='\033[1m'; RED='\033[31m'; GRN='\033[32m'; YLW='\033[33m'; CYN='\033[36m'; RST='\033[0m'
else
    BOLD=''; RED=''; GRN=''; YLW=''; CYN=''; RST=''
fi
section() { printf "\n${BOLD}${CYN}=== %s ===${RST}\n" "$*"; }
ok()      { printf "${GRN}[OK]${RST} %s\n" "$*"; }
warn()    { printf "${YLW}[WARN]${RST} %s\n" "$*"; }
fail()    { printf "${RED}[FAIL]${RST} %s\n" "$*" >&2; }
note()    { printf "${CYN}[NOTE]${RST} %s\n" "$*"; }

# --- arg parsing -----------------------------------------------------------
NEW_VERSION=""
DO_BUMP=true
DO_REGEN=true
DO_ZK=true
ZK_PUBLISH=""
ZK_PUBLISH_OFF=false
# The sparkdream-ui checkout beside this repo: the default --zk-publish target.
ZK_PUBLISH_DEFAULT="$REPO_ROOT/../sparkdream-ui/public/zk"
STRICT=false

usage() {
    # Print every leading `# ...` comment line after the shebang, stripped
    # of the leading `# `. Stops at the first non-comment line.
    awk '/^#!/ {next} /^[^#]/ {exit} /^#/ {print}' "$0" | sed 's/^#\s\?//'
    exit 0
}

while [ $# -gt 0 ]; do
    case "$1" in
        -h|--help) usage ;;
        --no-bump) DO_BUMP=false ;;
        --no-regen) DO_REGEN=false ;;
        --no-zk) DO_ZK=false ;;
        --zk-publish)
            if [ $# -lt 2 ]; then
                fail "--zk-publish needs a directory"
                exit 2
            fi
            ZK_PUBLISH="$2"
            shift
            ;;
        --zk-publish=*) ZK_PUBLISH="${1#--zk-publish=}" ;;
        --no-zk-publish) ZK_PUBLISH_OFF=true ;;
        --strict) STRICT=true ;;
        v[0-9]*.[0-9]*.[0-9]*)
            NEW_VERSION="$1"
            ;;
        *)
            fail "unknown argument: $1"
            echo "  run with --help for usage" >&2
            exit 2
            ;;
    esac
    shift
done

if [ -z "$NEW_VERSION" ] && [ "$DO_BUMP" = true ]; then
    fail "no version supplied (e.g. v1.0.7)"
    echo "  if you've already bumped versions manually, add --no-bump" >&2
    exit 2
fi

# --- pre-flight ------------------------------------------------------------
section "PRE-FLIGHT"

# Working tree dirty?
if [ -z "$SKIP_DIRTY_CHECK" ]; then
    if ! git diff --quiet --exit-code 2>/dev/null; then
        warn "working tree has unstaged changes — they'll be mixed in with this release's diff"
        echo "    (set SKIP_DIRTY_CHECK=1 to silence)"
    fi
    if ! git diff --cached --quiet --exit-code 2>/dev/null; then
        warn "you have STAGED changes — they'll be mixed in with this release's diff"
        echo "    (set SKIP_DIRTY_CHECK=1 to silence)"
    fi
else
    note "SKIP_DIRTY_CHECK=1 — skipping working-tree cleanliness check"
fi

# Any sparkdreamd running? Genesis regen builds binaries to /tmp; running chains
# don't conflict, but a parallel run could fight us if it tries to use ~/.sparkdream.
if pgrep -af 'sparkdreamd start --home /home' >/dev/null 2>&1 || \
   pgrep -af 'sparkdreamd start$' >/dev/null 2>&1; then
    if pgrep -af 'sparkdreamd start --home '"$HOME"'/.sparkdream' >/dev/null 2>&1; then
        warn "a chain is running with --home ~/.sparkdream — regen will fight it"
        echo "    stop it first: pkill -f 'sparkdreamd start --home $HOME/.sparkdream'"
    else
        note "sparkdreamd processes detected but not on ~/.sparkdream — fine"
    fi
fi

# --zk-publish target must exist (checked now, not after the regen). Without
# the flag, the sparkdream-ui checkout beside this repo is used if present.
if [ -n "$ZK_PUBLISH" ] && [ "$ZK_PUBLISH_OFF" = true ]; then
    fail "--zk-publish and --no-zk-publish conflict"
    exit 2
fi
if [ -z "$ZK_PUBLISH" ] && [ "$ZK_PUBLISH_OFF" = false ] && [ "$DO_ZK" = true ]; then
    if [ -d "$ZK_PUBLISH_DEFAULT" ]; then
        ZK_PUBLISH="$(cd "$ZK_PUBLISH_DEFAULT" && pwd)"
    else
        note "no sparkdream-ui checkout at $ZK_PUBLISH_DEFAULT — the prover will be built but not published"
    fi
fi
if [ -n "$ZK_PUBLISH" ]; then
    if [ "$DO_ZK" = false ]; then
        fail "--zk-publish and --no-zk conflict"
        exit 2
    fi
    if [ ! -d "$ZK_PUBLISH" ]; then
        fail "--zk-publish: $ZK_PUBLISH is not a directory"
        exit 2
    fi
    ok "prover will be published to $ZK_PUBLISH"
fi

# Required tools
for cmd in python3 jq go make; do
    command -v "$cmd" >/dev/null 2>&1 || { fail "missing required tool: $cmd"; exit 2; }
done
python3 -c 'import yaml' 2>/dev/null || { fail "missing required Python module: yaml (pip install pyyaml)"; exit 2; }
ok "tools available"

# --- 1. bump deploy versions ------------------------------------------------
if [ "$DO_BUMP" = true ]; then
    section "1. BUMP DEPLOY VERSIONS"

    # The seven files that the prior version-bump commits (ea6ff66, 328e357,
    # 81c2741, 8ef40d6) all touched in lockstep.
    DEPLOY_FILES=(
        deploy/config/network/devnet/sentry.sdl.yaml
        deploy/config/network/devnet/validator.sdl.yaml
        deploy/config/network/mainnet/sentry.sdl.yaml
        deploy/config/network/mainnet/validator.sdl.yaml
        deploy/config/network/testnet/sentry.sdl.yaml
        deploy/config/network/testnet/validator.sdl.yaml
        deploy/docs/DEPLOYMENT.md
    )

    # Detect the current version from DEPLOYMENT.md so we don't blindly run a
    # global v*.* substitution (which would corrupt unrelated version strings).
    OLD_VERSION=$(grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' deploy/docs/DEPLOYMENT.md | head -1)
    if [ -z "$OLD_VERSION" ]; then
        fail "could not detect current version from deploy/docs/DEPLOYMENT.md"
        exit 1
    fi

    if [ "$OLD_VERSION" = "$NEW_VERSION" ]; then
        warn "current version is already $NEW_VERSION — skipping bump"
    else
        echo "  $OLD_VERSION -> $NEW_VERSION"
        for f in "${DEPLOY_FILES[@]}"; do
            if ! grep -qF "$OLD_VERSION" "$f"; then
                warn "  $f doesn't contain $OLD_VERSION — possibly already bumped, skipping"
                continue
            fi
            sed -i "s|${OLD_VERSION}|${NEW_VERSION}|g" "$f"
            echo "    [OK] $f"
        done

        # Sanity sweep: any stragglers? Tracked files only: deploy/ also holds
        # ignored checkouts (deploy/explorer/ping-pub) whose compressed git
        # packs can contain the version's bytes by chance.
        STRAGGLERS=$(git grep -lF "$OLD_VERSION" -- deploy/ 2>/dev/null || true)
        if [ -n "$STRAGGLERS" ]; then
            fail "still found $OLD_VERSION in:"
            echo "$STRAGGLERS" | sed 's/^/      /' >&2
            echo "    fix manually or extend DEPLOY_FILES in this script" >&2
            exit 1
        fi
        ok "all deploy files bumped to $NEW_VERSION"
    fi
fi

# --- 2. audit configs -------------------------------------------------------
section "2. AUDIT CONFIGS"

AUDIT_ARGS=(--check-snapshots)
[ "$STRICT" = true ] && AUDIT_ARGS+=(--strict)

if deploy/scripts/audit-configs.py "${AUDIT_ARGS[@]}"; then
    ok "audit clean"
else
    rc=$?
    fail "audit reported issues (exit $rc) — fix before proceeding"
    echo "    re-run after fixing:  deploy/scripts/prepare-release.sh $NEW_VERSION --no-bump" >&2
    exit 1
fi

# --- 3. regenerate genesis --------------------------------------------------
if [ "$DO_REGEN" = true ]; then
    section "3. REGENERATE GENESIS"

    # The regenerate script builds 3 network-tagged binaries to /tmp and runs
    # `sparkdreamd init` per network. Takes 1-2 min total.
    if deploy/scripts/regenerate-network-genesis.py; then
        ok "genesis files regenerated"
    else
        fail "genesis regeneration failed"
        exit 1
    fi

    # --- 4. verify genesis --------------------------------------------------
    section "4. VERIFY GENESIS"

    if make verify-genesis; then
        ok "all per-network and cross-network audit tests pass"
    else
        fail "make verify-genesis failed — inspect output above"
        exit 1
    fi
fi

# --- 5. build zk prover -----------------------------------------------------
# The browser prover must come from the same release as the chain: a wasm from
# another release can derive the proof's public inputs differently, and the
# chain then rejects every anonymous exec with "ZK proof verification failed".
ZK_OUT=""
if [ "$DO_ZK" = true ]; then
    section "5. BUILD ZK PROVER"

    ZK_VERSION="${NEW_VERSION:-$(grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' deploy/docs/DEPLOYMENT.md | head -1)}"
    if [ -z "$ZK_VERSION" ]; then
        fail "could not determine the release version for the zk prover"
        exit 1
    fi
    ZK_OUT="build/zk/$ZK_VERSION"
    CEREMONY=build/ceremony

    ZK_KEYS=""
    if [ -f "$CEREMONY/verifying_key.bin" ] && [ -f "$CEREMONY/proving_key.raw.bin" ] && [ -f "$CEREMONY/circuit.r1cs" ]; then
        ZK_KEYS="$CEREMONY"
        CEREMONY_VK=$(base64 -w0 "$CEREMONY/verifying_key.bin")
        for net in devnet testnet mainnet; do
            GENESIS_VK=$(jq -r '.app_state.shield.verification_keys[]? | select(.circuit_id == "shield_v1") | .vk_bytes' \
                "deploy/config/network/$net/genesis.json")
            if [ -z "$GENESIS_VK" ]; then
                note "$net genesis has no shield verifying key — anonymous execs are off there"
            elif [ "$GENESIS_VK" = "$CEREMONY_VK" ]; then
                ok "$CEREMONY pairs with the $net genesis verifying key"
            else
                warn "$net genesis verifying key is not $CEREMONY's — its web client needs the proving key from its own ceremony, not $ZK_OUT"
            fi
        done
    else
        warn "no ceremony keys in $CEREMONY — building the wasm only; add proving_key.raw.bin and circuit.r1cs from the network's ceremony before publishing"
    fi

    if VERSION="$ZK_VERSION" tools/zk/wasm/build.sh "$ZK_OUT" $ZK_KEYS >/dev/null; then
        ok "prover for $ZK_VERSION built in $ZK_OUT"
        sed 's/^/    /' "$ZK_OUT/manifest.json"
    else
        fail "zk prover build failed"
        exit 1
    fi

    if [ -n "$ZK_PUBLISH" ]; then
        if [ -z "$ZK_KEYS" ]; then
            warn "not publishing to $ZK_PUBLISH: the build has no ceremony keys, and a client can't prove without them"
        else
            ZK_DEST="$ZK_PUBLISH/$ZK_VERSION"
            mkdir -p "$ZK_DEST"
            cp "$ZK_OUT"/* "$ZK_DEST/"
            ok "published to $ZK_DEST"
        fi
    fi
fi

# --- 6. e2e snapshots ------------------------------------------------------
# Nothing to do: local E2E `post-setup` snapshots are fingerprinted on the
# setup script, the sparkdreamd binary and config.yml
# (test/_snapshot_fingerprint.sh), so a config or code change in this release
# refreshes them on their next run.
section "6. E2E SNAPSHOTS"
note "snapshots refresh on their own when config.yml or the binary changes"

# --- summary ----------------------------------------------------------------
section "SUMMARY"

CHANGED=$(git status --short -- deploy/ 2>/dev/null | wc -l)
echo "  files changed in deploy/: $CHANGED"
git status --short -- deploy/ 2>/dev/null | sed 's/^/    /'

echo ""
ok "release $NEW_VERSION prepared. Suggested commit messages (split or combine):"
echo ""
if [ "$DO_BUMP" = true ]; then
    echo -e "  ${BOLD}Bump deploy images to $NEW_VERSION across all networks.${RST}"
fi
if [ "$DO_REGEN" = true ]; then
    echo -e "  ${BOLD}Regenerate genesis files to match $NEW_VERSION.${RST}"
fi
if [ -n "$ZK_OUT" ] && [ -z "${ZK_DEST:-}" ]; then
    echo ""
    note "publish $ZK_OUT/ at each web client's <zk assets>/$(basename "$ZK_OUT")/ and set its"
    note "ZK_DIGESTS to the manifest's \"files\" (build/zk/ is not tracked)"
fi
echo ""
note "NOT committing — stage and commit yourself."
