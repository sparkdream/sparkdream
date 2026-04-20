#!/usr/bin/env python3
"""
Regenerate deploy/config/network/{devnet,testnet,mainnet}/genesis.json from:
  * fresh `sparkdreamd init` outputs built per `-tags <network>`
    — source of truth for build-tag-specific Params values.
  * each network's config.yml `genesis.app_state` block
    — source of truth for per-network parameter overrides (mint inflation,
    gov timings, season cadence, forum archive windows, etc.).
  * FOUNDERS / TEST_ACCOUNTS / DEVNET_MEMBER_* constants below
    — source of truth for accounts, balances, members, and profiles.

All three network genesis files are fully generated artifacts: no template,
no manual editing. Update params in `params_vals_*.go` or per-network
overrides in `config.yml`, then run this script. Validate with
`make verify-genesis`.

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
# 4-tier structure documented in CLAUDE.md tokenomics.
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

# Devnet test accounts. Addresses derive from the mnemonics committed in
# deploy/config/network/devnet/config.yml; allocations match that file.
# Dave intentionally holds a tiny balance for non-member testing.
TEST_ACCOUNTS = [
    ("sprkdrm1mm04tct5hspk2qzjtf0xaqyjl46ajhcuc4wxcs", "20000000000000", "alice"),
    ("sprkdrm16ef99dd70nzl2lpvwcpz6k84tnhasw009uexc6", "10000000000000", "bob"),
    ("sprkdrm1a5wpjpcj0g7s38lqtlp54muytlal3j6jcmhjqw",  "5000000000000", "carol"),
    ("sprkdrm1v94rgfy8d3e345yva7p2p9uaaf6hle07lhkfs9",        "5000000", "dave"),
]

# Devnet rep member entries for alice/bob/carol. Trust levels and DREAM
# balances mirror deploy/config/network/devnet/config.yml — generous DEVNET
# allocations chosen for testing convenience, not tokenomic realism.
def _devnet_member(address, dream_balance, trust_level, invitation_credits):
    return {
        "address": address,
        "dream_balance": dream_balance,
        "staked_dream": "0",
        "lifetime_earned": dream_balance,
        "lifetime_burned": "0",
        "reputation_scores": {},
        "lifetime_reputation": {},
        "trust_level": trust_level,
        "trust_level_updated_at": 0,
        "joined_season": 0,
        "joined_at": 0,
        "invited_by": "",
        "invitation_chain": [],
        "invitation_credits": invitation_credits,
        "status": "MEMBER_STATUS_ACTIVE",
        "zeroed_at": 0,
        "zeroed_count": 0,
        "last_decay_epoch": 0,
        "tips_given_this_epoch": 0,
        "last_tip_epoch": 0,
        "completed_interims_count": 0,
        "completed_initiatives_count": 0,
    }


DEVNET_MEMBER_MAP = [
    _devnet_member("sprkdrm1mm04tct5hspk2qzjtf0xaqyjl46ajhcuc4wxcs", "50000000000", "TRUST_LEVEL_CORE",        10),  # alice
    _devnet_member("sprkdrm16ef99dd70nzl2lpvwcpz6k84tnhasw009uexc6", "25000000000", "TRUST_LEVEL_ESTABLISHED",  5),  # bob
    _devnet_member("sprkdrm1a5wpjpcj0g7s38lqtlp54muytlal3j6jcmhjqw", "10000000000", "TRUST_LEVEL_PROVISIONAL",  3),  # carol
]

# Devnet season profiles for alice/bob/carol — high/mid/low XP tiers so
# devnet exercises different title/level states out of the box. Mirrors
# deploy/config/network/devnet/config.yml.
DEVNET_MEMBER_PROFILES = [
    {
        "address": "sprkdrm1mm04tct5hspk2qzjtf0xaqyjl46ajhcuc4wxcs",
        "display_name": "Alice", "username": "alice", "display_title": "veteran",
        "season_xp": 5000, "lifetime_xp": 5000, "season_level": 8,
        "unlocked_titles": ["newcomer", "veteran", "rising_star"],
        "achievements": ["first_step", "voice_heard", "contributor"],
        "invitations_successful": 5, "challenges_won": 2,
        "jury_duties_completed": 3, "votes_cast": 15, "forum_helpful_count": 50,
    },
    {
        "address": "sprkdrm16ef99dd70nzl2lpvwcpz6k84tnhasw009uexc6",
        "display_name": "Bob", "username": "bob", "display_title": "newcomer",
        "season_xp": 1500, "lifetime_xp": 1500, "season_level": 6,
        "unlocked_titles": ["newcomer"],
        "achievements": ["first_step", "voice_heard"],
        "invitations_successful": 2, "challenges_won": 0,
        "jury_duties_completed": 1, "votes_cast": 5, "forum_helpful_count": 15,
    },
    {
        "address": "sprkdrm1a5wpjpcj0g7s38lqtlp54muytlal3j6jcmhjqw",
        "display_name": "Carol", "username": "carol", "display_title": "",
        "season_xp": 300, "lifetime_xp": 300, "season_level": 2,
        "unlocked_titles": ["newcomer"],
        "achievements": ["voice_heard"],
        "invitations_successful": 0, "challenges_won": 0,
        "jury_duties_completed": 0, "votes_cast": 2, "forum_helpful_count": 5,
    },
]

NETWORKS = {
    "devnet": {
        "chain_id": "sparkdream-dev-1",
        "binary": "/tmp/sparkdreamd-devnet",
        "init_home": "/tmp/devnet-init",
        "config": os.path.join(REPO_ROOT, "deploy/config/network/devnet/config.yml"),
        "out": os.path.join(REPO_ROOT, "deploy/config/network/devnet/genesis.json"),
    },
    "testnet": {
        "chain_id": "sparkdream-test-1",
        "binary": "/tmp/sparkdreamd-testnet",
        "init_home": "/tmp/testnet-init",
        "config": os.path.join(REPO_ROOT, "deploy/config/network/testnet/config.yml"),
        "out": os.path.join(REPO_ROOT, "deploy/config/network/testnet/genesis.json"),
    },
    "mainnet": {
        "chain_id": "sparkdream-1",
        "binary": "/tmp/sparkdreamd-mainnet",
        "init_home": "/tmp/mainnet-init",
        "config": os.path.join(REPO_ROOT, "deploy/config/network/mainnet/config.yml"),
        "out": os.path.join(REPO_ROOT, "deploy/config/network/mainnet/genesis.json"),
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


def _parse_coin_amount(coin_str, denom="uspark"):
    """Parse a coins string like '20000000000000uspark' → '20000000000000'."""
    if not isinstance(coin_str, str) or not coin_str.endswith(denom):
        return None
    return coin_str[: -len(denom)]


def _check_founders(cfg, errors):
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
        amt = _parse_coin_amount(coins[0]) if coins else None
        if amt != f["spark"]:
            errors.append(f"accounts.{name}.coins[0]: config={(coins[0] if coins else None)!r} expected {f['spark']}uspark")
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


def _check_devnet(cfg, errors):
    """Validate devnet config.yml against TEST_ACCOUNTS, DEVNET_MEMBER_MAP,
    DEVNET_MEMBER_PROFILES. Test accounts use mnemonic-derived addresses, so
    the accounts: block is matched by name+amount only (we trust the
    mnemonic→address mapping was verified once at setup)."""
    by_name = {name: (addr, amt) for addr, amt, name in TEST_ACCOUNTS}

    # accounts: top-level
    cfg_accounts = cfg.get("accounts") or []
    cfg_names = set()
    for acct in cfg_accounts:
        name = acct.get("name")
        cfg_names.add(name)
        if name not in by_name:
            errors.append(f"accounts: unknown test account name {name!r}")
            continue
        _, expected_amt = by_name[name]
        coins = acct.get("coins") or []
        amt = _parse_coin_amount(coins[0]) if coins else None
        if amt != expected_amt:
            errors.append(f"accounts.{name}.coins[0]: config={(coins[0] if coins else None)!r} expected {expected_amt}uspark")
    for _, _, name in TEST_ACCOUNTS:
        if name not in cfg_names:
            errors.append(f"accounts: test account {name!r} present in script but missing from config.yml")

    app_state = (cfg.get("genesis") or {}).get("app_state") or {}

    # rep.member_map
    rep_mm = (app_state.get("rep") or {}).get("member_map") or []
    by_addr = {m["address"]: m for m in DEVNET_MEMBER_MAP}
    cfg_addrs = set()
    for m in rep_mm:
        addr = m.get("address")
        cfg_addrs.add(addr)
        if addr not in by_addr:
            errors.append(f"rep.member_map: unknown devnet member address {addr}")
            continue
        ours = by_addr[addr]
        for key in ("dream_balance", "trust_level", "invitation_credits"):
            if m.get(key) != ours.get(key):
                errors.append(f"rep.member_map[{addr}].{key}: config={m.get(key)!r} script={ours.get(key)!r}")
    for ours_m in DEVNET_MEMBER_MAP:
        if ours_m["address"] not in cfg_addrs:
            errors.append(f"rep.member_map: address {ours_m['address']} present in script but missing from config.yml")

    # season.member_profile_map
    profile_mm = (app_state.get("season") or {}).get("member_profile_map") or []
    by_addr = {p["address"]: p for p in DEVNET_MEMBER_PROFILES}
    cfg_addrs = set()
    for p in profile_mm:
        addr = p.get("address")
        cfg_addrs.add(addr)
        if addr not in by_addr:
            errors.append(f"season.member_profile_map: unknown devnet address {addr}")
            continue
        ours = by_addr[addr]
        for key in ("display_name", "username", "season_xp", "season_level"):
            if p.get(key) != ours.get(key):
                errors.append(f"season.member_profile_map[{addr}].{key}: config={p.get(key)!r} script={ours.get(key)!r}")
    for ours_p in DEVNET_MEMBER_PROFILES:
        if ours_p["address"] not in cfg_addrs:
            errors.append(f"season.member_profile_map: address {ours_p['address']} present in script but missing from config.yml")


def _check_community_pool(cfg, errors):
    """Validate config.yml's community_pool seed matches COMMUNITY_POOL_AMOUNT."""
    app_state = (cfg.get("genesis") or {}).get("app_state") or {}
    pool = ((app_state.get("distribution") or {}).get("fee_pool") or {}).get("community_pool") or []
    if not pool:
        errors.append("distribution.fee_pool.community_pool: missing or empty")
        return
    coin = pool[0]
    if coin.get("denom") != "uspark":
        errors.append(f"distribution.fee_pool.community_pool[0].denom: config={coin.get('denom')!r} expected 'uspark'")
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
    errors = []
    if network == "devnet":
        _check_devnet(cfg, errors)
    else:
        _check_founders(cfg, errors)
    _check_community_pool(cfg, errors)
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


def preserve_gen_txs(network, account_addrs):
    """Load gen_txs from the existing output genesis (if present), validate
    each against the current chain_id and account list, and return them.

    Gentxs are signed artifacts we cannot reproduce without the validator's
    private key, so regeneration must carry them forward. But a stale gentx
    (wrong chain_id, or signed by an address the operator has since removed
    from FOUNDERS/TEST_ACCOUNTS) will silently fail InitGenesis at chain
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
                f"correspond to any account in FOUNDERS/TEST_ACCOUNTS. Either the "
                f"gentx is stale (recollect it) or the account was dropped from "
                f"the script (restore it). Remove "
                f"{os.path.relpath(out_path, REPO_ROOT)} to regenerate without "
                f"preserving the gentx."
            )
        delegator = msg.get("delegator_address") or ""
        if delegator and _bech32_data(delegator) not in known_data:
            raise ValueError(
                f"{network}: gen_tx[{i}] delegator_address {delegator} does not "
                f"correspond to any account in FOUNDERS/TEST_ACCOUNTS."
            )
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

# Per-founder extras. `first_spark` goes to Valya (Tier 1 Lead Vocal) who
# gathered the initial founding members — exactly one holder, ever.
EXTRA_FOUNDER_ACHIEVEMENTS = {
    "sprkdrm19wsctgkpk93wkquu7t8g07gnvwzwdupshys9mu": ["first_spark"],  # Valya
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


def _set_user_state(g, principal_accounts, members, profiles):
    """Set auth.accounts, bank.balances, bank.supply, rep.member_map and
    season.member_profile_map from a flat list of (address, spark_amount)
    plus the rep + season member entries.

    Adds the distribution ModuleAccount and the 95M SPARK community pool
    seed at the end (same on every network so x/split has uniform state)."""
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
        {"address": addr, "coins": [{"denom": "uspark", "amount": amt}]}
        for addr, amt in principal_accounts
    ] + [{
        "address": COMMUNITY_POOL_ADDR,
        "coins": [{"denom": "uspark", "amount": COMMUNITY_POOL_AMOUNT}],
    }]
    total = sum(int(amt) for _, amt in principal_accounts) + int(COMMUNITY_POOL_AMOUNT)
    g["app_state"]["bank"]["supply"] = [{"denom": "uspark", "amount": str(total)}]

    g["app_state"]["rep"]["member_map"] = members
    g["app_state"]["season"]["member_profile_map"] = profiles


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
    )
    return g


def build_devnet(fresh):
    g = copy.deepcopy(fresh)
    apply_config_overrides(g, NETWORKS["devnet"]["config"])
    g["chain_id"] = NETWORKS["devnet"]["chain_id"]
    g["genesis_time"] = preserve_or_now_genesis_time("devnet")
    g["app_version"] = ""
    g["app_state"]["genutil"]["gen_txs"] = preserve_gen_txs(
        "devnet", [addr for addr, _, _ in TEST_ACCOUNTS],
    )
    _set_user_state(
        g,
        [(addr, amt) for addr, amt, _ in TEST_ACCOUNTS],
        copy.deepcopy(DEVNET_MEMBER_MAP),
        copy.deepcopy(DEVNET_MEMBER_PROFILES),
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
            f"TEST_ACCOUNTS / DEVNET_MEMBER_MAP / DEVNET_MEMBER_PROFILES /\n"
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
