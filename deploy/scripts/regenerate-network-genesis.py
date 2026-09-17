#!/usr/bin/env python3
"""
Regenerate deploy/config/network/{devnet,testnet,mainnet}/genesis.json from:
  * fresh `sparkdreamd init` outputs built per `-tags <network>`
    — source of truth for build-tag-specific Params values.
  * each network's config.yml `genesis.app_state` block
    — source of truth for per-network parameter overrides (mint inflation,
    gov timings, season cadence, forum archive windows, etc.).
  * FOUNDERS / DEVNET_ACCOUNTS constants below
    — source of truth for accounts, balances, members, and profiles.

All three network genesis files are fully generated artifacts: no template,
no manual editing. Update params in `params_vals_*.go` or per-network
overrides in `config.yml`, then run this script. Validate with
`make verify-genesis`.

The output has two consumers that use different halves of it: this repo's
manual deployment path uses all of it, and the chain launcher keeps only the
module params and non-address bootstrap state, rebuilding everything keyed to
an address from its own launch spec. Anything address-keyed added here has to
be strippable on that side. See the genesis ownership section of
deploy/README.md before adding one.

Two exceptions are preserved from the existing output file across
regenerations:
  * `app_state.genutil.gen_txs` — the validator gentx is manually signed and
    cannot be reproduced without the validator's key. Gentxs are validated
    against the current account list and chain_id — if either has drifted,
    the script fails and asks you to remove the stale output file.
  * `genesis_time` — kept stable across reruns so an already-deployed chain's
    genesis hash doesn't change. When no existing file is present we default
    to the current UTC time (never the Go time.Time zero value, which breaks
    modules that compute `block.time - genesis.time`).

Usage:
    deploy/scripts/regenerate-network-genesis.py
    deploy/scripts/regenerate-network-genesis.py --networks devnet
    deploy/scripts/regenerate-network-genesis.py --skip-build   # reuse /tmp binaries
"""

import argparse
import copy
import datetime
import hashlib
import json
import os
import re
import subprocess
import sys

import yaml

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))

# Distribution module account address (deterministic from the chain bech32
# prefix and the module name "distribution"). Same on every network.
COMMUNITY_POOL_ADDR = "sprkdrm1jv65s3grqf6v6jl3dp4t6c9t9rk99cd868re8z"

# 95M SPARK seeded into the community pool at genesis (95% of total supply).
# x/split routes from this pool to councils on the first block.
COMMUNITY_POOL_AMOUNT = "95000000000000"

# Founder accounts shared by testnet and mainnet. Addresses match
# x/commons/keeper/genesis_vals.go. SPARK and DREAM allocations follow the
# 4-tier structure documented in docs/tokenomics.md.
#
# The per-trust-level `dream` and `invitation_credits` values are read back by
# the chain launcher, which treats rep.member_map in the generated genesis as a
# template library: it finds the row whose trust_level matches what a launch
# spec asked for and copies those two fields onto the spec's own account.
# Changing a value here changes what every launched chain grants members of
# that level, and dropping a tier leaves the launcher with no template for it
# (it falls back to the CORE row and zeroes both fields). See the genesis
# ownership section of deploy/README.md.
FOUNDERS = [
    # Tier 1 (Lead Vocal)
    {"address": "sprkdrm19wsctgkpk93wkquu7t8g07gnvwzwdupshys9mu", "name": "valya",
     "spark": "1250000000000", "dream": "5000000000",
     "trust_level": "TRUST_LEVEL_CORE",        "invitation_credits": 10,
     "display_name": "Valya",            "username": "valya"},
    # Tier 2 (Vocal)
    {"address": "sprkdrm1emtnqs9qw9vrg5lsa58dyt8llq5fyenylmqy3p", "name": "cozmonika",
     "spark":  "750000000000", "dream": "3500000000",
     "trust_level": "TRUST_LEVEL_TRUSTED",     "invitation_credits":  7,
     "display_name": "Cozmonika",        "username": "cozmonika"},
    # Tier 3 (Public)
    {"address": "sprkdrm1yhjdr8kxsrer3kcqpdrc2zd0kggvsj4c3vazkd", "name": "kingofbitchain",
     "spark":  "450000000000", "dream": "2500000000",
     "trust_level": "TRUST_LEVEL_ESTABLISHED", "invitation_credits":  5,
     "display_name": "King of Bitchain", "username": "kingofbitchain"},
    {"address": "sprkdrm1psq079p8erng2pf37nvvvmpqpetkknpmwxx4r8", "name": "viorika",
     "spark":  "450000000000", "dream": "2500000000",
     "trust_level": "TRUST_LEVEL_ESTABLISHED", "invitation_credits":  5,
     "display_name": "Viorika",          "username": "viorika"},
    {"address": "sprkdrm1wk6eh9zrw7n6xqmyw2yqja58ekpwy3h5u4gkge", "name": "uyen",
     "spark":  "450000000000", "dream": "2500000000",
     "trust_level": "TRUST_LEVEL_ESTABLISHED", "invitation_credits":  5,
     "display_name": "Uyen",             "username": "uyen"},
    {"address": "sprkdrm1crwfn2z2230jhtlaxwphyz0xrmuwc5ntc47vak", "name": "houri",
     "spark":  "450000000000", "dream": "2500000000",
     "trust_level": "TRUST_LEVEL_ESTABLISHED", "invitation_credits":  5,
     "display_name": "Houri",            "username": "houri"},
    {"address": "sprkdrm1x39wrr0l8x5lvxzuwff65t7zkw23fyyeres2mu", "name": "gilda",
     "spark":  "450000000000", "dream": "2500000000",
     "trust_level": "TRUST_LEVEL_ESTABLISHED", "invitation_credits":  5,
     "display_name": "Gilda",            "username": "gilda"},
    # Tier 4 (Anon)
    {"address": "sprkdrm1dqpr060l2pxy08j7q4gaahnmchs7qlhmf2w4y9", "name": "anon1",
     "spark":  "375000000000", "dream": "2000000000",
     "trust_level": "TRUST_LEVEL_PROVISIONAL", "invitation_credits":  3,
     "display_name": "",                 "username": ""},
    {"address": "sprkdrm1jqyzam9sewlmf704c84ysmkvhaqy8l0tpwysfs", "name": "anon2",
     "spark":  "375000000000", "dream": "2000000000",
     "trust_level": "TRUST_LEVEL_PROVISIONAL", "invitation_credits":  3,
     "display_name": "",                 "username": ""},
]

# Devnet accounts. One roster is the source of truth for the auth/bank
# accounts, the rep member entries, the season profiles AND the commons
# founding_members block — keeping them in three parallel lists keyed by
# address is what let the devnet relaunch of 2026-09-15 drift away from
# this script without anything noticing.
#
# Mnemonics are NOT in this repo: they live in the operator's
# `accounts.txt` alongside the other devnet keys. Addresses below were
# verified against the running chain.
#
# Per-trust-level `dream` and `invitation_credits` are read back by the chain
# launcher as templates for launched chains -- see the note on FOUNDERS above,
# which applies here identically.
#
# The roster mirrors the mainnet/testnet founder tiers so devnet exercises
# the same trust ladder, plus two deliberate test fixtures:
#   * gwen — the only PROVISIONAL member, for trust-gate rejection paths.
#   * hal  — funded but NOT a member (no rep entry, no season profile, not
#            a founding member), for non-member rejection paths. Its tiny
#            balance also makes it the insufficient-funds fixture.
# Devnet therefore totals 99,175,005 SPARK rather than the 100,000,000 the
# tokenomics doc specifies for mainnet; devnet has no float to preserve.
DEVNET_ACCOUNTS = [
    # Tier 1 analog — the founder. Marked founder=True in founding_members;
    # exactly one account may carry it (x/commons GenesisState.Validate).
    {"address": "sprkdrm1jct5shlz26j2qgdeyzj2nwe5e79djprhcgpwrc", "name": "alice",
     "spark": "1250000000000", "dream": "5000000000",
     "trust_level": "TRUST_LEVEL_CORE",        "invitation_credits": 10,
     "display_name": "Alice", "username": "alice", "handles": ["alice"],
     "member": True, "founder": True},
    # Tier 2 analog
    {"address": "sprkdrm1ctk6qnsg5rf7ddupvkxth8q6h7d49h80vcjgg0", "name": "bob",
     "spark":  "750000000000", "dream": "3500000000",
     "trust_level": "TRUST_LEVEL_TRUSTED",     "invitation_credits":  7,
     "display_name": "Bob", "username": "bob", "handles": ["bob"],
     "member": True, "founder": False},
    # Tier 3 analogs
    {"address": "sprkdrm17tce9p442054fkwl9zrntt7k7h3fs92fcwjxke", "name": "carol",
     "spark":  "450000000000", "dream": "2500000000",
     "trust_level": "TRUST_LEVEL_ESTABLISHED", "invitation_credits":  5,
     "display_name": "Carol", "username": "carol", "handles": ["carol"],
     "member": True, "founder": False},
    {"address": "sprkdrm1m6xjdt97j9a67smsy6n6h4vqgdkjy7jm8hvq8x", "name": "dave",
     "spark":  "450000000000", "dream": "2500000000",
     "trust_level": "TRUST_LEVEL_ESTABLISHED", "invitation_credits":  5,
     "display_name": "Dave", "username": "dave", "handles": ["dave"],
     "member": True, "founder": False},
    {"address": "sprkdrm1jzxgmcv3xgqlagkfrppaf8jvpvf3lyqpkhljn9", "name": "erin",
     "spark":  "450000000000", "dream": "2500000000",
     "trust_level": "TRUST_LEVEL_ESTABLISHED", "invitation_credits":  5,
     "display_name": "Erin", "username": "erin", "handles": ["erin"],
     "member": True, "founder": False},
    {"address": "sprkdrm1tfl0ys2wws5shrfre259drs5llh4ug83ne7q9e", "name": "felix",
     "spark":  "450000000000", "dream": "2500000000",
     "trust_level": "TRUST_LEVEL_ESTABLISHED", "invitation_credits":  5,
     "display_name": "Felix", "username": "felix", "handles": ["felix"],
     "member": True, "founder": False},
    # Tier 4 analog — devnet's only PROVISIONAL member (trust-gate fixture).
    {"address": "sprkdrm1kllgzkzvjyq09r8unw4l69wl993u0tex5gd3v0", "name": "gwen",
     "spark":  "375000000000", "dream": "2000000000",
     "trust_level": "TRUST_LEVEL_PROVISIONAL", "invitation_credits":  3,
     "display_name": "Gwen", "username": "gwen", "handles": ["gwen"],
     "member": True, "founder": False},
    # Non-member fixture. 5 SPARK is enough for a handful of fee-bearing
    # txs and nothing else, so it doubles as the insufficient-funds case.
    {"address": "sprkdrm1enva2et8wh6mn3j04lz0lyxezz0mjsyw0kz49s", "name": "hal",
     "spark":       "5000000",
     "member": False, "founder": False},
]


def devnet_members():
    """The subset of DEVNET_ACCOUNTS that gets a rep Member, a season
    profile and a commons founding_members entry. Excludes `hal`."""
    return [a for a in DEVNET_ACCOUNTS if a["member"]]


# Testnet-only welcome blog post (id=1; blog IDs start at 1), authored by kingofbitchain. Lives
# in genesis so a fresh testnet always boots with the same landing post and
# we don't have to re-post it after every chain reset. The body references
# `sparkdream-test-1` and the testnet reset cadence explicitly, so devnet
# and mainnet keep an empty blog state. The post is permanent (expires_at=0)
# but not pinned: pinning in this chain semantically means "rescued from
# ephemeral expiry", which is a meaningless flag on an already-permanent
# post. created_at is tied to the network's genesis_time at write time.
TESTNET_WELCOME_POST_TITLE = "Welcome to Spark Dream"
TESTNET_WELCOME_POST_BODY = (
    "Spark Dream is a Cosmos SDK chain for shared creative work, "
    "coordination, and self-publishing.\n"
    "\n"
    "From this site you can post dreams in Imaginarium, start discussions "
    "in Swarm, curate collections in Wonders, earn achievements and join "
    "guilds in Season, trade conditional shares in Futarchy, and follow "
    "governance and federation activity. Identity is anchored by onchain "
    "names, reputation is earned across initiatives, and councils route the "
    "more sensitive actions through governance.\n"
    "\n"
    "This is a live demo. Everything you do here is signed through Keplr "
    "and broadcast against the real test chain (sparkdream-test-1). Posts, "
    "stakes, proposals, and reactions are written onchain, not mocked. The "
    "RSS feed at /feed.xml syndicates the activity stream for anyone who "
    "wants to follow along outside the UI.\n"
    "\n"
    "A few caveats while you explore:\n"
    "\n"
    "This is a work in progress. Several modules already have full UI "
    "coverage: Imaginarium, Swarm, Wonders, Reveal, Governance, Sessions, "
    "Names. Two are still partial:\n"
    "\n"
    "• Futarchy markets. The LMSR market list and conditional-share "
    "trading work, but creator residuals, resolution flows, and some admin "
    "paths are still being built out.\n"
    "\n"
    "• Federation. Peers, identity links, and the verification queue "
    "render, but cross-chain attestation and bridge operator actions are "
    "scaffolded rather than fully wired.\n"
    "\n"
    "Beyond that, expect rough edges. Things will move, copy will change, "
    "and a few buttons may sit there looking pretty without doing much yet.\n"
    "\n"
    "Finally: the test chain is reset periodically to improve and add "
    "functionality to the chain. When that happens, every post, balance, "
    "market, contribution, and proposal you see disappears. That is on "
    "purpose. Resets let us land breaking changes without dragging legacy "
    "state along. Please don't get too attached to anything you publish "
    "here yet.\n"
    "\n"
    "Have fun. Break things. File issues."
)


NETWORKS = {
    "devnet": {
        "chain_id": "sparkdream-dev-1",
        "binary": "/tmp/sparkdreamd-devnet",
        "init_home": "/tmp/devnet-init",
        "config": os.path.join(REPO_ROOT, "deploy/config/network/devnet/config.yml"),
        "out": os.path.join(REPO_ROOT, "deploy/config/network/devnet/genesis.json"),
        # bond_denom is per-chain since the x/identity migration; the binary's
        # build-tagged DefaultChainIdentity sets it per network. Mirrored here
        # so the config.yml consistency check accepts the matching coin strings.
        "bond_denom": "usparz.sparkdreamdev",
    },
    "testnet": {
        "chain_id": "sparkdream-test-1",
        "binary": "/tmp/sparkdreamd-testnet",
        "init_home": "/tmp/testnet-init",
        "config": os.path.join(REPO_ROOT, "deploy/config/network/testnet/config.yml"),
        "out": os.path.join(REPO_ROOT, "deploy/config/network/testnet/genesis.json"),
        "bond_denom": "uspark.sparkdreamtest",
    },
    "mainnet": {
        "chain_id": "sparkdream-1",
        "binary": "/tmp/sparkdreamd-mainnet",
        "init_home": "/tmp/mainnet-init",
        "config": os.path.join(REPO_ROOT, "deploy/config/network/mainnet/config.yml"),
        "out": os.path.join(REPO_ROOT, "deploy/config/network/mainnet/genesis.json"),
        "bond_denom": "uspark.sparkdream",
    },
}


def run(cmd, **kwargs):
    print("$", " ".join(cmd))
    subprocess.run(cmd, check=True, **kwargs)


def build_binary(network):
    cfg = NETWORKS[network]
    # Mirror Makefile ldflags so `sparkdreamd init` stamps the correct
    # version.Name/AppName into genesis.json instead of the SDK default "<appd>".
    ldflags = (
        "-X github.com/cosmos/cosmos-sdk/version.Name=sparkdream "
        "-X github.com/cosmos/cosmos-sdk/version.AppName=sparkdreamd "
        f"-X github.com/cosmos/cosmos-sdk/version.BuildTags={network}"
    )
    run(
        ["go", "build", "-tags", network, "-ldflags", ldflags,
         "-o", cfg["binary"], "./cmd/sparkdreamd/main.go"],
        cwd=REPO_ROOT,
    )


def init_fresh_genesis(network):
    cfg = NETWORKS[network]
    if os.path.exists(cfg["init_home"]):
        run(["rm", "-rf", cfg["init_home"]])
    run(
        [cfg["binary"], "init", "genesis-template",
         "--chain-id", cfg["chain_id"], "--home", cfg["init_home"]],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )


def load_fresh(network):
    path = os.path.join(NETWORKS[network]["init_home"], "config", "genesis.json")
    with open(path) as f:
        return json.load(f)


def deep_merge(base, overrides):
    """Recursively overlay `overrides` onto `base`. Dicts merge key-by-key;
    leaf values and lists are replaced outright."""
    if not isinstance(base, dict) or not isinstance(overrides, dict):
        return overrides
    out = dict(base)
    for k, v in overrides.items():
        if k in out and isinstance(out[k], dict) and isinstance(v, dict):
            out[k] = deep_merge(out[k], v)
        else:
            out[k] = v
    return out


def apply_config_overrides(genesis, config_path):
    """Overlay the network's config.yml `genesis.app_state` onto the fresh
    genesis. Captures per-network overrides that fresh `sparkdreamd init`
    output misses — mint inflation, gov timings, season cadence, forum
    archive windows, denom_metadata, commons.category_map, and more.
    Mirrors what `ignite chain init` would do."""
    with open(config_path) as f:
        cfg = yaml.safe_load(f) or {}
    overrides = (cfg.get("genesis") or {}).get("app_state") or {}
    if overrides:
        genesis["app_state"] = deep_merge(genesis["app_state"], overrides)


# -------------------------- consistency check --------------------------


def _parse_coin_amount(coin_str, denom):
    """Parse a coins string like '20000000000000uspark.sparkdream' → '20000000000000'.

    `denom` is the per-network bond denom (e.g. 'uspark.sparkdream',
    'uspark.sparkdreamtest', 'usparz.sparkdreamdev') — passed in from the
    NETWORKS table rather than hardcoded since the x/identity migration
    made bond denoms per-chain."""
    if not isinstance(coin_str, str) or not coin_str.endswith(denom):
        return None
    return coin_str[: -len(denom)]


def _check_founders(cfg, errors, bond_denom):
    """Validate config.yml founder data against the FOUNDERS constant."""
    by_name = {f["name"]: f for f in FOUNDERS}
    by_addr = {f["address"]: f for f in FOUNDERS}

    # accounts: top-level
    cfg_accounts = cfg.get("accounts") or []
    cfg_names = set()
    for acct in cfg_accounts:
        name = acct.get("name")
        cfg_names.add(name)
        if name not in by_name:
            errors.append(f"accounts: unknown founder name {name!r}")
            continue
        f = by_name[name]
        if acct.get("address") != f["address"]:
            errors.append(f"accounts.{name}.address: config={acct.get('address')!r} script={f['address']!r}")
        coins = acct.get("coins") or []
        amt = _parse_coin_amount(coins[0], bond_denom) if coins else None
        if amt != f["spark"]:
            errors.append(f"accounts.{name}.coins[0]: config={(coins[0] if coins else None)!r} expected {f['spark']}{bond_denom}")
    for f in FOUNDERS:
        if f["name"] not in cfg_names:
            errors.append(f"accounts: founder {f['name']!r} present in script but missing from config.yml")

    app_state = (cfg.get("genesis") or {}).get("app_state") or {}

    # rep.member_map
    rep_mm = (app_state.get("rep") or {}).get("member_map") or []
    cfg_addrs = set()
    for m in rep_mm:
        addr = m.get("address")
        cfg_addrs.add(addr)
        if addr not in by_addr:
            errors.append(f"rep.member_map: unknown founder address {addr}")
            continue
        f = by_addr[addr]
        if m.get("dream_balance") != f["dream"]:
            errors.append(f"rep.member_map[{f['name']}].dream_balance: config={m.get('dream_balance')!r} script={f['dream']!r}")
        if m.get("trust_level") != f["trust_level"]:
            errors.append(f"rep.member_map[{f['name']}].trust_level: config={m.get('trust_level')!r} script={f['trust_level']!r}")
        if m.get("invitation_credits") != f["invitation_credits"]:
            errors.append(f"rep.member_map[{f['name']}].invitation_credits: config={m.get('invitation_credits')!r} script={f['invitation_credits']!r}")
    for f in FOUNDERS:
        if f["address"] not in cfg_addrs:
            errors.append(f"rep.member_map: founder {f['name']!r} present in script but missing from config.yml")

    # season.member_profile_map
    profile_mm = (app_state.get("season") or {}).get("member_profile_map") or []
    cfg_addrs = set()
    for p in profile_mm:
        addr = p.get("address")
        cfg_addrs.add(addr)
        if addr not in by_addr:
            errors.append(f"season.member_profile_map: unknown founder address {addr}")
            continue
        f = by_addr[addr]
        if p.get("display_name") != f["display_name"]:
            errors.append(f"season.member_profile_map[{f['name']}].display_name: config={p.get('display_name')!r} script={f['display_name']!r}")
        if p.get("username") != f["username"]:
            errors.append(f"season.member_profile_map[{f['name']}].username: config={p.get('username')!r} script={f['username']!r}")
        expected_ach = _founder_achievements(addr)
        if (p.get("achievements") or []) != expected_ach:
            errors.append(f"season.member_profile_map[{f['name']}].achievements: config={p.get('achievements')!r} script={expected_ach!r}")
    for f in FOUNDERS:
        if f["address"] not in cfg_addrs:
            errors.append(f"season.member_profile_map: founder {f['name']!r} present in script but missing from config.yml")


def _check_devnet(cfg, errors, bond_denom):
    """Validate devnet config.yml against DEVNET_ACCOUNTS.

    Unlike the founder networks, devnet's accounts: block is matched by
    address as well as amount — the roster now carries the addresses, so
    there is no reason to trust a name-only match. Covers the four blocks
    the regenerator writes from the roster: accounts, rep.member_map,
    season.member_profile_map and commons.founding_members."""
    by_name = {a["name"]: a for a in DEVNET_ACCOUNTS}

    # accounts: top-level
    cfg_accounts = cfg.get("accounts") or []
    cfg_names = set()
    for acct in cfg_accounts:
        name = acct.get("name")
        cfg_names.add(name)
        if name not in by_name:
            errors.append(f"accounts: unknown devnet account name {name!r}")
            continue
        a = by_name[name]
        if acct.get("address") != a["address"]:
            errors.append(f"accounts.{name}.address: config={acct.get('address')!r} script={a['address']!r}")
        coins = acct.get("coins") or []
        amt = _parse_coin_amount(coins[0], bond_denom) if coins else None
        if amt != a["spark"]:
            errors.append(f"accounts.{name}.coins[0]: config={(coins[0] if coins else None)!r} expected {a['spark']}{bond_denom}")
    for a in DEVNET_ACCOUNTS:
        if a["name"] not in cfg_names:
            errors.append(f"accounts: devnet account {a['name']!r} present in script but missing from config.yml")

    app_state = (cfg.get("genesis") or {}).get("app_state") or {}
    by_addr = {a["address"]: a for a in devnet_members()}

    # rep.member_map — members only; hal is deliberately absent.
    rep_mm = (app_state.get("rep") or {}).get("member_map") or []
    cfg_addrs = set()
    for m in rep_mm:
        addr = m.get("address")
        cfg_addrs.add(addr)
        if addr not in by_addr:
            errors.append(f"rep.member_map: {addr} is not a devnet member in the script roster")
            continue
        ours = _devnet_member(by_addr[addr])
        for key in ("dream_balance", "trust_level", "invitation_credits"):
            if m.get(key) != ours[key]:
                errors.append(f"rep.member_map[{addr}].{key}: config={m.get(key)!r} script={ours[key]!r}")
    for a in devnet_members():
        if a["address"] not in cfg_addrs:
            errors.append(f"rep.member_map: address {a['address']} ({a['name']}) present in script but missing from config.yml")

    # season.member_profile_map
    profile_mm = (app_state.get("season") or {}).get("member_profile_map") or []
    cfg_addrs = set()
    for p in profile_mm:
        addr = p.get("address")
        cfg_addrs.add(addr)
        if addr not in by_addr:
            errors.append(f"season.member_profile_map: {addr} is not a devnet member in the script roster")
            continue
        ours = _devnet_profile(by_addr[addr])
        for key in ("display_name", "username", "achievements", "season_xp", "season_level"):
            if p.get(key) != ours[key]:
                errors.append(f"season.member_profile_map[{addr}].{key}: config={p.get(key)!r} script={ours[key]!r}")
    for a in devnet_members():
        if a["address"] not in cfg_addrs:
            errors.append(f"season.member_profile_map: address {a['address']} ({a['name']}) present in script but missing from config.yml")

    # commons.founding_members — overrides the compiled-in GenesisNames /
    # GenesisHandles / FounderName in x/commons/keeper/genesis_vals_devnet.go,
    # so a mismatch here silently bootstraps governance around the wrong set.
    cfg_fm = (app_state.get("commons") or {}).get("founding_members") or []
    ours_fm = {f["address"]: f for f in _devnet_founding_members()}
    cfg_addrs = set()
    for f in cfg_fm:
        addr = f.get("address")
        cfg_addrs.add(addr)
        if addr not in ours_fm:
            errors.append(f"commons.founding_members: {addr} is not a devnet member in the script roster")
            continue
        ours = ours_fm[addr]
        for key in ("display_name", "founder", "handles"):
            if f.get(key) != ours[key]:
                errors.append(f"commons.founding_members[{addr}].{key}: config={f.get(key)!r} script={ours[key]!r}")
    for addr, f in ours_fm.items():
        if addr not in cfg_addrs:
            errors.append(f"commons.founding_members: address {addr} ({f['display_name']}) present in script but missing from config.yml")


def _check_community_pool(cfg, errors, bond_denom):
    """Validate config.yml's community_pool seed matches COMMUNITY_POOL_AMOUNT."""
    app_state = (cfg.get("genesis") or {}).get("app_state") or {}
    pool = ((app_state.get("distribution") or {}).get("fee_pool") or {}).get("community_pool") or []
    if not pool:
        errors.append("distribution.fee_pool.community_pool: missing or empty")
        return
    coin = pool[0]
    if coin.get("denom") != bond_denom:
        errors.append(f"distribution.fee_pool.community_pool[0].denom: config={coin.get('denom')!r} expected {bond_denom!r}")
    if coin.get("amount") != COMMUNITY_POOL_AMOUNT:
        errors.append(f"distribution.fee_pool.community_pool[0].amount: config={coin.get('amount')!r} script={COMMUNITY_POOL_AMOUNT!r}")


def validate_config_consistency(network):
    """Return a list of consistency errors for the given network's config.yml
    relative to the regenerator's constants. Empty list = clean. Both files
    carry overlapping content (so `ignite chain init` can still produce a
    usable genesis from config.yml alone) and this check is the only thing
    keeping them in sync."""
    with open(NETWORKS[network]["config"]) as f:
        cfg = yaml.safe_load(f) or {}
    bond_denom = NETWORKS[network]["bond_denom"]
    errors = []
    if network == "devnet":
        _check_devnet(cfg, errors, bond_denom)
    else:
        _check_founders(cfg, errors, bond_denom)
    _check_community_pool(cfg, errors, bond_denom)
    return errors


# -------------------------- preservation --------------------------


# Match RFC3339 with arbitrary fractional-second precision. Go's time.Time
# marshals genesis_time at nanosecond precision (9 digits), which Python
# 3.10's datetime.fromisoformat rejects (it only accepts 3 or 6) — so we
# validate shape with a regex instead of the stdlib parser.
_RFC3339_RE = re.compile(
    r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})$"
)


def preserve_or_now_genesis_time(network):
    """Return the existing output file's genesis_time if it looks like a
    non-zero RFC3339 timestamp; otherwise return the current UTC time
    formatted for Cosmos SDK genesis.json.

    We cannot use '0001-01-01T00:00:00Z' as a placeholder — CometBFT accepts
    it, but modules that compute `block.time - genesis.time` (notably
    x/slashing's downtime window) underflow or produce absurd values, causing
    nodes to fail shortly after start. A real timestamp is always safer, and
    preserving across regenerations keeps the genesis hash stable for already-
    deployed chains."""
    out_path = NETWORKS[network]["out"]
    zero = "0001-01-01T00:00:00Z"
    if os.path.exists(out_path):
        with open(out_path) as f:
            existing = json.load(f)
        gt = existing.get("genesis_time")
        if isinstance(gt, str) and gt and gt != zero and _RFC3339_RE.match(gt):
            return gt
    return datetime.datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%S.%fZ")


def _rfc3339_to_unix(rfc3339_str):
    """Convert a genesis_time RFC3339 string to an int64 unix timestamp.

    Drops fractional seconds entirely — Go emits up to nanosecond precision
    (9 digits) which Python's `datetime.fromisoformat` rejects in 3.10, and
    blog `created_at` is a whole-second int64 anyway. The 'Z' suffix encodes
    UTC; Cosmos always uses UTC for genesis_time."""
    s = rfc3339_str.rstrip("Z").split(".", 1)[0]
    dt = datetime.datetime.fromisoformat(s).replace(tzinfo=datetime.timezone.utc)
    return int(dt.timestamp())


def _bech32_data(addr):
    """Return the data portion of a bech32 address (chars between the final
    '1' separator and the 6-char checksum). The same underlying account bytes
    yield the same data portion regardless of HRP — so `sprkdrm1…` and
    `sprkdrmvaloper1…` for one validator share this substring even though
    their checksums differ."""
    if not isinstance(addr, str):
        return None
    idx = addr.rfind("1")
    if idx == -1 or len(addr) - idx < 7:
        return None
    return addr[idx + 1 : -6]


# -------------------- gentx tamper detection (hash pinning) --------------------
#
# Catches the bug class introduced by commit efcf392: a migration script
# bulk-rewrote denoms inside gentxs (uspark → uspark.sparkdreamtest etc.)
# but had no access to the operator's secp256k1 key, so the signature was
# left pointing at the pre-migration bytes. The chain panics at InitChain
# with "signature verification failed" — which we now want to surface at
# regenerate time, not at deploy time.
#
# Why hash-pinning instead of full signature verification:
# cosmos-sdk's SIGN_MODE_LEGACY_AMINO_JSON sign-doc reconstruction depends on
# the aminojson encoder's exact rules (uint64-as-string, dont_omitempty
# semantics, the `legacy_coins` Coin encoding, and the new `unordered`/
# `timeout_timestamp` fields in SDK 0.53). Reproducing that byte-for-byte
# in Python is high-effort and fragile. A trust-on-first-use hash baseline
# catches the same bug class — any tampering, signature-related or not —
# without needing to track SDK signing internals.
#
# On the FIRST run after a fresh gentx is committed, the hash file is
# created automatically and the operator is asked to commit it. On every
# subsequent run, the recorded hash is compared to a freshly-computed one
# over the carried-forward gentx; any drift fails the regenerator loudly.
#
# If the gentx has to legitimately change (e.g. a new chain_id and the
# operator regenerates), delete the .gentx-hashes file and re-run.


def _gentx_canonical_bytes(gentx):
    """Return a deterministic byte representation of the gentx for hashing.
    Sorted keys, no whitespace, UTF-8 encoded — same recipe cosmos-sdk uses
    for canonical JSON, so two semantically-equal gentxs hash identically
    regardless of insertion order in the source genesis."""
    return json.dumps(gentx, sort_keys=True, separators=(",", ":")).encode()


def _gentx_hash_path(network):
    """Sibling file to the network's genesis.json. One hash per line, in
    the same order as app_state.genutil.gen_txs."""
    return NETWORKS[network]["out"] + ".gentx-hashes"


def check_gentx_tampering(network, gen_txs):
    """Compare each carried-forward gentx to its recorded hash. Raise on
    any drift, with diagnostics that name the specific mismatch."""
    hash_path = _gentx_hash_path(network)
    rel = os.path.relpath(hash_path, REPO_ROOT)
    current_hashes = [
        hashlib.sha256(_gentx_canonical_bytes(tx)).hexdigest() for tx in gen_txs
    ]

    if not os.path.exists(hash_path):
        # Bootstrap: record current hashes and ask the operator to commit.
        with open(hash_path, "w") as f:
            f.write("\n".join(current_hashes) + "\n")
        print(
            f"  [{network}] {rel} did not exist — recorded {len(current_hashes)} "
            f"gentx hash(es) from the current genesis. Review the gentx for\n"
            f"  correctness (in particular: the denom in body.messages[i].value\n"
            f"  and auth_info.fee.amount[*] matches NETWORKS[{network!r}]\n"
            f"  ['bond_denom']), then commit this file. Future regenerations\n"
            f"  will fail if anyone silently modifies a gentx after this point."
        )
        return

    with open(hash_path) as f:
        recorded_hashes = [line.strip() for line in f if line.strip()]

    if len(recorded_hashes) != len(current_hashes):
        raise ValueError(
            f"{network}: gentx count drift — {rel} has {len(recorded_hashes)} "
            f"hash(es) but the genesis has {len(current_hashes)} gen_tx(s). "
            f"If gentxs were intentionally added/removed, delete {rel} and "
            f"re-run to record a new baseline."
        )

    for i, (got, want) in enumerate(zip(current_hashes, recorded_hashes)):
        if got != want:
            bond_denom = NETWORKS[network]["bond_denom"]
            tx = gen_txs[i]
            body_denoms = sorted({
                c["denom"]
                for msg in tx.get("body", {}).get("messages", [])
                if isinstance(msg.get("value"), dict) and "denom" in msg["value"]
                for c in [msg["value"]]
            })
            fee_denoms = sorted({
                c["denom"]
                for c in tx.get("auth_info", {}).get("fee", {}).get("amount", [])
                if "denom" in c
            })
            raise ValueError(
                f"\n{network}: gen_tx[{i}] HAS BEEN MODIFIED since its hash was "
                f"recorded.\n"
                f"  expected sha256       : {want}\n"
                f"  current sha256        : {got}\n"
                f"  gentx body denoms     : {body_denoms}\n"
                f"  gentx fee denoms      : {fee_denoms}\n"
                f"  expected bond_denom   : {bond_denom}\n\n"
                f"This is the bug class introduced by commit efcf392 (the\n"
                f"x/identity denom rewrite that silently retagged gentx bytes\n"
                f"without re-signing). The chain will panic at InitChain with\n"
                f"'signature verification failed'.\n\n"
                f"If this change is INTENTIONAL (operator regenerated the\n"
                f"gentx and you've verified the new signature), delete\n"
                f"  {rel}\n"
                f"and re-run to record the new baseline.\n\n"
                f"If this change is UNEXPECTED, restore the gentx from a known-\n"
                f"good source (the operator, a deployed chain's export, or a\n"
                f"git revision before the modification). Carrying the broken\n"
                f"gentx forward will deploy a chain that can't start.\n"
            )

    print(
        f"  [{network}] {len(current_hashes)} gen_tx(s) match recorded "
        f"hashes in {os.path.basename(rel)} ✓"
    )


def preserve_gen_txs(network, account_addrs):
    """Load gen_txs from the existing output genesis (if present), validate
    each against the current chain_id and account list, and return them.

    Gentxs are signed artifacts we cannot reproduce without the validator's
    private key, so regeneration must carry them forward. But a stale gentx
    (wrong chain_id, or signed by an address the operator has since removed
    from FOUNDERS/DEVNET_ACCOUNTS) will silently fail InitGenesis at chain
    start — so we refuse to preserve in those cases and ask the operator to
    delete the output file and collect a fresh gentx."""
    out_path = NETWORKS[network]["out"]
    if not os.path.exists(out_path):
        return []
    with open(out_path) as f:
        existing = json.load(f)
    gen_txs = ((existing.get("app_state") or {}).get("genutil") or {}).get("gen_txs") or []
    if not gen_txs:
        return []

    expected_chain_id = NETWORKS[network]["chain_id"]
    existing_chain_id = existing.get("chain_id")
    if existing_chain_id != expected_chain_id:
        raise ValueError(
            f"{network}: existing {os.path.relpath(out_path, REPO_ROOT)} "
            f"has chain_id={existing_chain_id!r} but regenerator is writing "
            f"chain_id={expected_chain_id!r}. Gentxs were signed against the "
            f"old chain_id and will not verify. Delete the output file and "
            f"recollect a gentx against the new chain_id."
        )

    known_data = {d for d in (_bech32_data(a) for a in account_addrs) if d}
    for i, tx in enumerate(gen_txs):
        messages = ((tx.get("body") or {}).get("messages")) or []
        if not messages:
            raise ValueError(f"{network}: gen_tx[{i}] has no messages; cannot validate")
        msg = messages[0]
        val_addr = msg.get("validator_address", "")
        val_data = _bech32_data(val_addr)
        if val_data is None:
            raise ValueError(
                f"{network}: gen_tx[{i}] validator_address {val_addr!r} is not a valid bech32 address"
            )
        if val_data not in known_data:
            raise ValueError(
                f"{network}: gen_tx[{i}] validator_address {val_addr} does not "
                f"correspond to any account in FOUNDERS/DEVNET_ACCOUNTS. Either the "
                f"gentx is stale (recollect it) or the account was dropped from "
                f"the script (restore it). Remove "
                f"{os.path.relpath(out_path, REPO_ROOT)} to regenerate without "
                f"preserving the gentx."
            )
        delegator = msg.get("delegator_address") or ""
        if delegator and _bech32_data(delegator) not in known_data:
            raise ValueError(
                f"{network}: gen_tx[{i}] delegator_address {delegator} does not "
                f"correspond to any account in FOUNDERS/DEVNET_ACCOUNTS."
            )

    # Hash-pinning tamper check (see comment block above check_gentx_tampering).
    # Runs after the structural checks so the diagnostics that fire here can
    # assume chain_id and known signers are already correct.
    check_gentx_tampering(network, gen_txs)

    print(
        f"preserved {len(gen_txs)} gen_tx(s) from existing "
        f"{os.path.relpath(out_path, REPO_ROOT)}"
    )
    return gen_txs


# -------------------------- per-network composers --------------------------


def _founder_member(f):
    return {
        "address": f["address"],
        "dream_balance": f["dream"],
        "staked_dream": "0",
        "lifetime_earned": f["dream"],
        "lifetime_burned": "0",
        "reputation_scores": {},
        "lifetime_reputation": {},
        "trust_level": f["trust_level"],
        "trust_level_updated_at": 0,
        "joined_season": 0,
        "joined_at": 0,
        "invited_by": "",
        "invitation_chain": [],
        "invitation_credits": f["invitation_credits"],
        "status": "MEMBER_STATUS_ACTIVE",
        "zeroed_at": 0,
        "zeroed_count": 0,
        "last_decay_epoch": 0,
        "tips_given_this_epoch": 0,
        "last_tip_epoch": 0,
        "completed_interims_count": 0,
        "completed_initiatives_count": 0,
    }


# Every founder starts with the Genesis Founder achievement — irreplaceable
# because REQUIREMENT_TYPE_GENESIS has no runtime awarder in x/season.
FOUNDER_GENESIS_ACHIEVEMENTS = ["genesis_founder"]

# Per-founder extras. `first_spark` goes to whoever gathered that chain's
# initial founding members — exactly one holder per chain, ever. Keyed by
# address, so the devnet and testnet/mainnet holders coexist here.
EXTRA_FOUNDER_ACHIEVEMENTS = {
    "sprkdrm19wsctgkpk93wkquu7t8g07gnvwzwdupshys9mu": ["first_spark"],  # Valya (testnet/mainnet)
    "sprkdrm1jct5shlz26j2qgdeyzj2nwe5e79djprhcgpwrc": ["first_spark"],  # Alice (devnet)
}


def _founder_achievements(address):
    # Extras come first: query_member_achievements returns [0] as the member's
    # headline, so the more distinctive achievement should surface there.
    return EXTRA_FOUNDER_ACHIEVEMENTS.get(address, []) + FOUNDER_GENESIS_ACHIEVEMENTS


def _founder_profile(f):
    return {
        "address": f["address"],
        "display_name": f["display_name"],
        "username": f["username"],
        "display_title": "",
        "season_xp": 0,
        "lifetime_xp": 0,
        "season_level": 0,
        "unlocked_titles": [],
        "achievements": _founder_achievements(f["address"]),
        "invitations_successful": 0,
        "challenges_won": 0,
        "jury_duties_completed": 0,
        "votes_cast": 0,
        "forum_helpful_count": 0,
    }


def _devnet_member(a):
    """rep.member_map entry for one devnet member. Same shape as
    _founder_member — devnet just draws its field values from
    DEVNET_ACCOUNTS instead of FOUNDERS."""
    return _founder_member(a)


def _devnet_profile(a):
    """season.member_profile_map entry for one devnet member. Profiles are
    zeroed at genesis (no seeded XP, levels or titles): a fresh devnet
    should exercise the earning paths, and pre-seeded progress made the
    season-transition tests start from a state no real chain can reach."""
    return _founder_profile(a)


def _devnet_founding_members():
    """commons.founding_members for devnet.

    Non-empty, which makes x/commons bootstrap governance around THIS
    roster and ignore the compiled-in GenesisNames / GenesisHandles /
    FounderName in x/commons/keeper/genesis_vals_devnet.go. Handles are
    carried through so each member's canonical name is claimed at genesis
    rather than left open to a squatter, matching what the compiled-in
    map does on testnet and mainnet.

    Exactly one entry may set founder=True; GenesisState.Validate rejects
    anything else and BootstrapGovernance panics on a founderless set."""
    return [
        {
            "address": a["address"],
            "display_name": a["display_name"],
            "founder": a["founder"],
            "handles": list(a["handles"]),
        }
        for a in devnet_members()
    ]


def _set_user_state(g, principal_accounts, members, profiles, bond_denom,
                    founding_members=None):
    """Set auth.accounts, bank.balances, bank.supply, rep.member_map and
    season.member_profile_map from a flat list of (address, spark_amount)
    plus the rep + season member entries.

    `principal_accounts` may be a superset of `members`: an account that is
    funded but has no rep entry (devnet's `hal`) simply never appears in
    members/profiles/founding_members.

    `founding_members` overrides x/commons's compiled-in founder maps when
    non-empty. Networks that want the build-tag defaults pass None, which
    leaves the fresh-init empty list alone.

    Adds the distribution ModuleAccount and the 95M SPARK community pool
    seed at the end (same on every network so x/split has uniform state).

    `bond_denom` is the per-network native denom (uspark.sparkdream,
    uspark.sparkdreamtest, usparz.sparkdreamdev) — passed in from the
    NETWORKS table since the x/identity migration made it per-chain."""
    accounts = [
        {
            "@type": "/cosmos.auth.v1beta1.BaseAccount",
            "address": addr, "pub_key": None,
            "account_number": str(i), "sequence": "0",
        }
        for i, (addr, _) in enumerate(principal_accounts)
    ]
    accounts.append({
        "@type": "/cosmos.auth.v1beta1.ModuleAccount",
        "base_account": {
            "address": COMMUNITY_POOL_ADDR, "pub_key": None,
            "account_number": str(len(accounts)), "sequence": "0",
        },
        "name": "distribution",
        "permissions": [],
    })
    g["app_state"]["auth"]["accounts"] = accounts

    g["app_state"]["bank"]["balances"] = [
        {"address": addr, "coins": [{"denom": bond_denom, "amount": amt}]}
        for addr, amt in principal_accounts
    ] + [{
        "address": COMMUNITY_POOL_ADDR,
        "coins": [{"denom": bond_denom, "amount": COMMUNITY_POOL_AMOUNT}],
    }]
    total = sum(int(amt) for _, amt in principal_accounts) + int(COMMUNITY_POOL_AMOUNT)
    g["app_state"]["bank"]["supply"] = [{"denom": bond_denom, "amount": str(total)}]

    g["app_state"]["rep"]["member_map"] = members
    g["app_state"]["season"]["member_profile_map"] = profiles
    if founding_members is not None:
        g["app_state"]["commons"]["founding_members"] = founding_members


def _apply_testnet_welcome_blog_post(g):
    """Inject the kingofbitchain testnet welcome post at blog post id=1.

    Blog post IDs start at 1 (ID 0 is reserved: reply_id=0 means a
    post-level reaction target and parent_reply_id=0 means top-level).
    Replaces the empty `posts` list and bumps `post_count` to 2. The
    `created_at` is set to the network's genesis_time so the post's
    timestamp moves with the chain start rather than drifting on each
    regenerate. Testnet-only — the body references `sparkdream-test-1`
    and the testnet reset cadence explicitly."""
    genesis_unix = _rfc3339_to_unix(g["genesis_time"])
    creator = next(f["address"] for f in FOUNDERS if f["name"] == "kingofbitchain")

    welcome_post = {
        "id": 1,
        "title": TESTNET_WELCOME_POST_TITLE,
        "body": TESTNET_WELCOME_POST_BODY,
        "creator": creator,
        "content_type": "CONTENT_TYPE_MARKDOWN",
        "replies_enabled": True,
        "reply_count": 0,
        "min_reply_trust_level": 0,
        "created_at": genesis_unix,
        "updated_at": 0,
        "status": "POST_STATUS_ACTIVE",
        "hidden_by": "",
        "hidden_at": 0,
        "expires_at": 0,
        "pinned_by": "",
        "pinned_at": 0,
        "fee_bytes_high_water": 0,
        "edited": False,
        "edited_at": 0,
        "initiative_id": 0,
        "conviction_sustained": False,
        "tags": [],
    }
    g["app_state"]["blog"]["posts"] = [welcome_post]
    g["app_state"]["blog"]["post_count"] = "2"


def _build_with_founders(network, fresh):
    """Compose testnet/mainnet from the founder constants. Identical flow
    for both — only chain_id (from NETWORKS) differs."""
    g = copy.deepcopy(fresh)
    apply_config_overrides(g, NETWORKS[network]["config"])
    g["chain_id"] = NETWORKS[network]["chain_id"]
    g["genesis_time"] = preserve_or_now_genesis_time(network)
    g["app_version"] = ""
    g["app_state"]["genutil"]["gen_txs"] = preserve_gen_txs(
        network, [f["address"] for f in FOUNDERS],
    )
    _set_user_state(
        g,
        [(f["address"], f["spark"]) for f in FOUNDERS],
        [_founder_member(f) for f in FOUNDERS],
        [_founder_profile(f) for f in FOUNDERS],
        NETWORKS[network]["bond_denom"],
    )
    if network == "testnet":
        _apply_testnet_welcome_blog_post(g)
    return g


def build_devnet(fresh):
    g = copy.deepcopy(fresh)
    apply_config_overrides(g, NETWORKS["devnet"]["config"])
    g["chain_id"] = NETWORKS["devnet"]["chain_id"]
    g["genesis_time"] = preserve_or_now_genesis_time("devnet")
    g["app_version"] = ""
    g["app_state"]["genutil"]["gen_txs"] = preserve_gen_txs(
        "devnet", [a["address"] for a in DEVNET_ACCOUNTS],
    )
    _set_user_state(
        g,
        [(a["address"], a["spark"]) for a in DEVNET_ACCOUNTS],
        [_devnet_member(a) for a in devnet_members()],
        [_devnet_profile(a) for a in devnet_members()],
        NETWORKS["devnet"]["bond_denom"],
        founding_members=_devnet_founding_members(),
    )
    return g


def build_testnet(fresh):
    return _build_with_founders("testnet", fresh)


def build_mainnet(fresh):
    return _build_with_founders("mainnet", fresh)


BUILDERS = {
    "devnet":  build_devnet,
    "testnet": build_testnet,
    "mainnet": build_mainnet,
}


def write(path, doc):
    # sort_keys=True alphabetizes object keys recursively (array element
    # order is preserved). Stable ordering across all three networks makes
    # cross-network diffs readable in side-by-side diff viewers.
    with open(path, "w") as f:
        json.dump(doc, f, indent=2, sort_keys=True, ensure_ascii=False)
        f.write("\n")
    print(f"wrote {os.path.relpath(path, REPO_ROOT)}")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument(
        "--networks", nargs="+",
        default=["devnet", "testnet", "mainnet"],
        choices=["devnet", "testnet", "mainnet"],
        help="which networks to regenerate (default: all three)",
    )
    ap.add_argument(
        "--skip-build", action="store_true",
        help="reuse existing /tmp binaries (faster iteration when only the merger logic changed)",
    )
    args = ap.parse_args()

    # Fail fast on config↔script drift before doing any build/init work.
    # Collect errors across all networks so the user sees everything at once
    # rather than fixing one, re-running, fixing the next, etc.
    all_errors = {network: validate_config_consistency(network) for network in args.networks}
    if any(all_errors.values()):
        for network, errors in all_errors.items():
            if not errors:
                continue
            rel = os.path.relpath(NETWORKS[network]["config"], REPO_ROOT)
            print(f"\nERROR: {rel} is out of sync with {os.path.basename(__file__)} constants:", file=sys.stderr)
            for e in errors:
                print(f"  • {e}", file=sys.stderr)
        print(
            f"\nUpdate either the offending config.yml file(s) or the FOUNDERS /\n"
            f"DEVNET_ACCOUNTS /\n"
            f"COMMUNITY_POOL_AMOUNT constants in the script.",
            file=sys.stderr,
        )
        sys.exit(1)

    for network in args.networks:
        if not args.skip_build:
            build_binary(network)
        init_fresh_genesis(network)
        fresh = load_fresh(network)
        doc = BUILDERS[network](fresh)
        write(NETWORKS[network]["out"], doc)


if __name__ == "__main__":
    main()
