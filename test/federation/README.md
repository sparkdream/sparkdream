# x/federation E2E Tests

End-to-end test suite for [x/federation](../../x/federation/) — the cross-chain content, identity, and reputation bridge module.

Two complementary suites live here:
- **Single-chain tests** (this directory) — exercise message validation, governance flows, bridge-operator slashing, and local state transitions on a single sparkdreamd node. Fast and self-contained.
- **Multi-chain tests** ([multichain/](multichain/)) — exercise the IBC `OnRecv` and `OnAck` handlers across two real chains connected via Hermes. The only place where cross-chain packet round-trips can actually be observed.

If you only want the multichain quick-start, jump to [multichain/README.md](multichain/README.md).

## What's tested

| Suite | File | Coverage |
|-------|------|----------|
| Single-chain | [params_test.sh](params_test.sh) | Module params shape + `MsgUpdateOperationalParams` (OpsComm subset) |
| Single-chain | [peer_lifecycle_test.sh](peer_lifecycle_test.sh) | `RegisterPeer`, `ResumePeer`, `SuspendPeer`, `RemovePeer` state machine |
| Single-chain | [peer_policy_test.sh](peer_policy_test.sh) | `UpdatePeerPolicy` content-type allowlists, attestation flag, blocked identities |
| Single-chain | [bridge_operator_test.sh](bridge_operator_test.sh) | `MsgRegisterBridge` (operator-signed), `MsgUpdateBridge`, plus the x/service top-up/unbond/double-unbond flows that replace the deleted federation messages |
| Single-chain | [content_federation_test.sh](content_federation_test.sh) | `FederateContent` (IBC outbound — sender-side validation) and inbound moderation |
| Single-chain | [identity_link_test.sh](identity_link_test.sh) | `LinkIdentity` (Phase 1 outbound), `UnlinkIdentity`, claimed-address rejections |
| Single-chain | [verifier_test.sh](verifier_test.sh) | Cross-chain verifier role: `VerifyContent`, `ChallengeVerification`, `SubmitArbiterHash`, `EscalateChallenge` (verifier bonding flows through `tx rep bond-role federation-verifier`) |
| Single-chain | [query_test.sh](query_test.sh) | All read-side queries (peers, policies, content, links, attestations, bindings, verifier activity) |
| Single-chain | [query_pagination_test.sh](query_pagination_test.sh) | `--limit` / `--page-key` / `--reverse` / `--count-total` against every `List*` query + the per-claimed-address filter on `ListPendingIdentityChallenges` |
| Single-chain | [service_hooks_test.sh](service_hooks_test.sh) | All four x/service hooks end-to-end: `AfterOperatorUnderfunded` → `BridgeBinding.suspended=true` (Group A via T1_SLASH that drops bond below `min_bond`); `AfterOperatorReFunded` → suspended cleared (Group A via `tx service top-up-bond`); `AfterOperatorRetired` → binding pruned (Group B via voluntary unbond + wait + claim); `AfterOperatorDissolved` → binding pruned (Group C via gov-tightened cap + 100% T1_SLASH that drives bond to zero per spec §3.4.9); plus Decision 1a shared-bond binding and SLASHED re-registration block |
| Single-chain | [recovery_test.sh](recovery_test.sh) | `MsgUpdatePeerController` (gov-only happy + rejected paths), `MsgResyncBridgeCount` and `MsgPruneOrphanBindings` (dual-authority: OpsComm OR gov), no-op idempotency |
| Single-chain | [endblocker_test.sh](endblocker_test.sh) | Content TTL prune (Phase 1) with accelerated `content_ttl` via OpsComm, bridge inactivity threshold application (Phase 12); restores params after each test |
| Single-chain | [governance_params_test.sh](governance_params_test.sh) | `MsgUpdateParams` (gov, expedited) full-blob round-trip with LegacyDec fixup; rejects OpsComm-signed attempt |
| Multi-chain | [multichain/test_crosschain_content.sh](multichain/test_crosschain_content.sh) | A↔B content round-trips, dedup, disallowed-type, **silent-SendPacket regression** |
| Multi-chain | [multichain/test_crosschain_identity.sh](multichain/test_crosschain_identity.sh) | Full Phase 1 + Phase 2 link verification IBC round-trip |
| Multi-chain | [multichain/test_crosschain_reputation.sh](multichain/test_crosschain_reputation.sh) | Reputation attestation round-trip with PROVISIONAL discount |

The single-chain tests can validate that a `FederateContent` message is *constructed* correctly and that `SendPacket` is *called*, but they cannot prove the packet is *delivered* — that's why the multichain suite exists.

## Prerequisites

### Single-chain suite
1. **sparkdreamd built with `testparams`:**
   ```bash
   cd /home/chill/cosmos/sparkdream/sparkdream
   ignite chain build --build.tags testparams
   ```
2. **A running chain** (the suite expects it on `localhost:26657`). Start manually with `sparkdreamd start --home ~/.sparkdream` after `ignite chain init`, or use `--restore-setup` (below) which handles startup itself.
3. `jq`, standard CLI tools.

### Multi-chain suite
Everything above, plus the **Hermes IBC relayer** and a saved post-setup snapshot. See [multichain/README.md](multichain/README.md#prerequisites) for the full Hermes install procedure (release binary, cargo, or Homebrew).

## Running

Just run the suite. Both runners auto-create the required snapshot if it's missing — you don't have to remember to run `--save-setup` manually.

```bash
cd test/federation
./run_all_tests.sh                  # bootstraps chain + saves snapshot if missing, then tests
./run_all_tests.sh --multichain     # same, plus the multichain suite afterwards
```

What "auto-create" actually does:
- `--restore-setup` or `--multichain` need the snapshot at [snapshots/post-setup/](snapshots/post-setup/). If it doesn't exist, the runner internally calls itself with `--save-setup`.
- `--save-setup` requires a running chain. If none is up, it auto-runs `ignite chain init` + `sparkdreamd start` first.
- After the snapshot exists, the original mode resumes (chain is restored from the snapshot, tests run against it).

To opt out:
- `--no-auto-snapshot` — fail instead of auto-creating a missing snapshot.
- `--no-auto-bootstrap` — fail instead of auto-init+starting a fresh chain when `--save-setup` is invoked with no running chain.

### Manual snapshot control (for fast iteration loops)

```bash
# One-time setup (or after proto/keeper changes invalidate the snapshot):
./run_all_tests.sh --save-setup

# Iterate fast (~1-2 min per run):
./run_all_tests.sh --restore-setup
```

The snapshot under [snapshots/post-setup/](snapshots/post-setup/) contains a fully-bootstrapped sparkdreamd home (founders, Commons Council, x/rep members, x/name registrations, federation peers) and is what the multichain suite clones for both of its chains in [multichain/init_chains.sh](multichain/init_chains.sh).

### Run a subset of tests

`run_all_tests.sh` supports per-suite skip flags. Combine freely:

| Flag | Skips |
|------|-------|
| `--no-setup` | Account setup (assumes already done; pair with `--restore-setup`) |
| `--no-params` | Params tests |
| `--no-peer` | Peer lifecycle tests |
| `--no-policy` | Peer policy tests |
| `--no-bridge` | Bridge operator tests |
| `--no-content` | Content federation tests |
| `--no-identity` | Identity link tests |
| `--no-verifier` | Verifier tests |
| `--no-query` | Query tests |
| `--no-pagination` | Query pagination tests |
| `--no-hooks` | x/service ServiceHooks integration tests |
| `--no-recovery` | Recovery message tests (UpdatePeerController / Resync / Prune) |
| `--no-endblocker` | EndBlocker sweep tests (uses accelerated TTLs via OpsComm) |
| `--no-gov-params` | `MsgUpdateParams` gov-authority tests |
| `--no-tests` | All tests (use with `--restore-setup` for a clean shell) |
| `--multichain` | (Adds, doesn't skip) Run the multichain suite after single-chain tests |

Examples:
```bash
# Iterate on a single test
./run_all_tests.sh --restore-setup --no-params --no-peer --no-policy --no-bridge --no-identity --no-verifier --no-query

# Run a single script directly (chain must be running, accounts set up)
bash content_federation_test.sh

# Full surface: single-chain + multichain
./run_all_tests.sh --multichain
```

### Run only the multichain suite

```bash
cd test/federation/multichain
./run_all_multichain_tests.sh                  # auto-creates snapshot if missing, ~8-12 min cold
./run_all_multichain_tests.sh --no-provision   # tests only, chains already running
./run_all_multichain_tests.sh --no-cleanup     # leave chains/relayer up after run
./run_all_multichain_tests.sh --no-auto-snapshot # fail instead of auto-building snapshot
```

See [multichain/README.md](multichain/README.md) for the full multichain workflow.

## Layout

```
test/federation/
├── README.md                           # this file
├── run_all_tests.sh                    # orchestrator (single-chain + optional --multichain)
├── setup_test_accounts.sh              # creates linker1/linker2/operator1/operator2/verifier1/verifier2/challenger1
├── peer_fixtures.sh                    # shared helpers: register_test_peer, set_peer_policy, register_test_bridge
├── params_test.sh                      # see table above
├── peer_lifecycle_test.sh
├── peer_policy_test.sh
├── bridge_operator_test.sh
├── content_federation_test.sh
├── identity_link_test.sh
├── verifier_test.sh
├── query_test.sh
├── query_pagination_test.sh            # --limit / --page-key / --reverse / filter args
├── service_hooks_test.sh               # x/service ServiceHooks integration
├── recovery_test.sh                    # UpdatePeerController / Resync / Prune
├── endblocker_test.sh                  # accelerated-TTL sweep coverage
├── governance_params_test.sh           # MsgUpdateParams (gov, expedited)
├── proposals/                          # generated council proposal JSON (gitignored)
├── snapshots/
│   └── post-setup/                     # saved chain state, used by --restore-setup and multichain
│       ├── restore.sh
│       ├── metadata.json
│       └── sparkdream_data/            # full ~/.sparkdream snapshot (config + genesis + data)
└── multichain/                         # cross-chain IBC suite — see multichain/README.md
    ├── README.md
    ├── check_prereqs.sh
    ├── init_chains.sh ... test_crosschain_*.sh
    └── ...
```

## Common pitfalls

### "Chain is not running" at test start
The single-chain suite expects a node on `localhost:26657`. If you stopped it manually, restart with:
```bash
sparkdreamd start --home ~/.sparkdream
```
Or use `--restore-setup` which boots the snapshot for you.

### Stale snapshots after proto changes
Proto schema changes invalidate the snapshot's app state. Regenerate after any proto/keeper schema change. (CLAUDE.md forbids `rm -rf`; use `find -delete` instead.)
```bash
find snapshots/post-setup -depth -delete
./run_all_tests.sh --save-setup
```

### Stale binaries
CLAUDE.md guidance: only one `sparkdreamd` should exist, at `$(go env GOPATH)/bin/sparkdreamd`. Check for stragglers:
```bash
ls /tmp/sparkdreamd ./sparkdreamd build/sparkdreamd 2>/dev/null  # all should be missing
```

### Test results scattered across stdout
Each test script prints its own pass/fail summary. The orchestrator's final block lists which suites failed. For a per-test breakdown, scroll up to each `*** TEST x ***` heading or run scripts individually.

### Multichain tests time out without errors
Almost always Hermes-related. Quick triage:
```bash
ps -ef | grep "hermes.*hermes_config" | grep -v grep   # is the daemon running?
tail -100 multichain/data/hermes.log                   # what does it think happened?
```
See [multichain/README.md](multichain/README.md#troubleshooting) for the full list.

## Reference

- Module: [x/federation/](../../x/federation/)
- Spec: [docs/x-federation-spec.md](../../docs/x-federation-spec.md)
- Proto: [proto/sparkdream/federation/v1/](../../proto/sparkdream/federation/v1/)
- Multichain plan / design: [docs/plans/2026-04-25-federation-multichain-e2e.md](../../docs/plans/2026-04-25-federation-multichain-e2e.md) (currently in `temp/sparkdream/`)
- Naming conventions: [docs/naming-conventions.md](../../docs/naming-conventions.md)
