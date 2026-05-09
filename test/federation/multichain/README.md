# Multi-Chain x/federation E2E Tests

End-to-end test suite for cross-chain federation flows (content sharing, identity verification, reputation attestation) between two Spark Dream chains connected via IBC. Single-chain tests in the parent directory only exercise message validation; the IBC `OnRecv` and `OnAck` handlers can only be reached with two real chains and a relayer.

## Architecture

```
   chain-a (fedtest-a)               chain-b (fedtest-b)
   RPC :26657 / gRPC :9090           RPC :36657 / gRPC :9190
                  |                          |
                  +-- hermes IBC relayer ----+
                          (port: federation, version: federation-1)
```

Both chains are bootstrapped from the existing post-setup snapshot ([../snapshots/post-setup/sparkdream_data/](../snapshots/post-setup/sparkdream_data/)) so they inherit the same founders, Commons Council members, x/rep population, and x/name registrations. Per-chain identity (chain-id, validator key, ports) is rewritten in [init_chains.sh](init_chains.sh).

## Prerequisites

### 1. sparkdreamd, built with `testparams`

```bash
cd /home/chill/cosmos/sparkdream/sparkdream
ignite chain build --build.tags testparams
```

The suite hard-depends on the short voting periods that the `testparams` build tag enables. Verify with `bash test/check_testparams.sh`.

### 2. Post-setup snapshot

**You don't have to do anything for this — `run_all_multichain_tests.sh` auto-creates the snapshot on first run** by delegating to `../run_all_tests.sh --save-setup`. The auto-creator also auto-bootstraps a chain via `ignite chain init` + `sparkdreamd start` if none is running.

To opt out (fail-fast instead of auto-create):
```bash
./run_all_multichain_tests.sh --no-auto-snapshot
```

To build the snapshot manually anyway:
```bash
cd test/federation
./run_all_tests.sh --save-setup
```

The snapshot lives at [test/federation/snapshots/post-setup/sparkdream_data/](../snapshots/post-setup/sparkdream_data/) and is a copy of `~/.sparkdream/data` taken right after [setup_test_accounts.sh](../setup_test_accounts.sh) completes. Both chains in the multichain suite are clones of it.

Regenerate after proto/keeper changes invalidate the saved app state. (CLAUDE.md forbids `rm -rf`; use `find -delete`.)
```bash
find test/federation/snapshots/post-setup -depth -delete
./run_all_tests.sh --save-setup
```

### 3. Hermes IBC relayer

Hermes is **not** installed by default. Pick one of the install paths below; verify with `hermes version`.

**Version requirement: 1.13.3 or newer.** This project uses [`ibc-go/v10`](../../../go.mod) (line 34: `github.com/cosmos/ibc-go/v10 v10.2.0`). Hermes 1.13.0 was the first release with full ibc-go/v10 support; older Hermes versions may fail during the channel handshake or panic on packet receipt. The latest stable at time of writing is **v1.13.3** (Sep 2025).

#### Option A: Pre-built release binary (recommended)

The fastest route; no Rust toolchain required.

```bash
# Pick the right asset for your OS/arch from
# https://github.com/informalsystems/hermes/releases
HERMES_VERSION=v1.13.3
ARCH="x86_64-unknown-linux-gnu"   # or aarch64-apple-darwin, x86_64-apple-darwin, etc.
URL="https://github.com/informalsystems/hermes/releases/download/${HERMES_VERSION}/hermes-${HERMES_VERSION}-${ARCH}.tar.gz"

mkdir -p ~/.local/bin
curl -fsSL "$URL" | tar -xz -C ~/.local/bin hermes
chmod +x ~/.local/bin/hermes

# Make sure ~/.local/bin is on PATH (most distros include it; if not, add to ~/.bashrc):
#   export PATH="$HOME/.local/bin:$PATH"

hermes version
```

If the asset name differs in newer releases, list the available files:
```bash
curl -fsSL https://api.github.com/repos/informalsystems/hermes/releases/latest \
  | jq -r '.assets[].browser_download_url'
```

To always grab whatever the current latest stable is (works as long as it's 1.13.x+):
```bash
LATEST=$(curl -fsSL https://api.github.com/repos/informalsystems/hermes/releases/latest | jq -r '.tag_name')
echo "Latest: $LATEST"
```

#### Option B: cargo (requires Rust >= 1.71)

```bash
# Install Rust first if needed:
#   curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh

cargo install --version 1.13.3 ibc-relayer-cli --bin hermes --locked
hermes version
```

This compiles from source and takes 5-15 minutes. The binary lands in `~/.cargo/bin/hermes`.

#### Option C: Homebrew (macOS / linuxbrew)

```bash
brew install informalsystems/tap/hermes   # tap may differ; check brew search hermes
hermes version
# Verify the installed version is >= 1.13.3; brew taps occasionally lag.
```

#### Where the suite looks for hermes

By default: `hermes` on `$PATH`. Override with the `HERMES` env var:
```bash
HERMES=$HOME/.local/bin/hermes ./run_all_multichain_tests.sh
```

[check_prereqs.sh](check_prereqs.sh) warns if the installed Hermes is older than 1.13. The configuration in [hermes_config.toml](hermes_config.toml) uses `event_source = push`, `clear packets`, and `--show-counterparty`, all of which are present in 1.13.x.

### 4. Other host tools

`jq`, `xxd`, `sha256sum`. All standard. [check_prereqs.sh](check_prereqs.sh) verifies presence.

### 5. Free ports

The suite needs `26656/26657/9090/1317` (chain-a) and `36656/36657/9190/1417` (chain-b). The prereq check warns if any are already in use.

## Running

### Full provision + tests (cold start)

```bash
cd test/federation/multichain
./run_all_multichain_tests.sh
```

Expected wall-clock: **8-12 minutes** cold. Phases:
1. Prereq checks
2. Init two chains from snapshot
3. Start both chains
4. IBC clients/connection/channel via hermes
5. Start hermes daemon
6. Register IBC peers via Commons Council proposals
7. Set peer policies
8. Create distinct chain-specific keys (`mc-linker-a`, `mc-owner-b`)
9. Cross-chain content tests (incl. silent-SendPacket regression)
10. Cross-chain identity tests (Phase 1 + Phase 2 round-trip)
11. Cross-chain reputation attestation tests

### Tests only (chains already running)

```bash
./run_all_multichain_tests.sh --no-provision
```

Useful for iterating on test logic without re-provisioning.

### Leave running for debugging

```bash
./run_all_multichain_tests.sh --no-cleanup
# investigate, then:
./stop_chains.sh
```

### Via the parent runner

```bash
cd test/federation
./run_all_tests.sh --multichain   # runs single-chain suite then the multichain suite
```

## Layout

| File | Role |
|------|------|
| [check_prereqs.sh](check_prereqs.sh) | Phase 0.0: hermes/jq/sparkdreamd/snapshot/port-binding sanity |
| [init_chains.sh](init_chains.sh) | Clone snapshot to chain-a/chain-b, regenerate chain-b validator |
| [start_chains.sh](start_chains.sh) / [stop_chains.sh](stop_chains.sh) | Background sparkdreamd processes |
| [hermes_config.toml](hermes_config.toml) | Relayer config (federation port, gas alignment with chain min-gas-prices) |
| [setup_ibc.sh](setup_ibc.sh) | BIP-39 mnemonics for hermes, fund relayer accounts, open clients/connection/channel |
| [start_relayer.sh](start_relayer.sh) / [stop_relayer.sh](stop_relayer.sh) | Background hermes daemon (separated so Task 2.5 can stop it mid-test) |
| [setup_peers.sh](setup_peers.sh) | Commons Council proposals registering each chain as an IBC peer of the other |
| [setup_policies.sh](setup_policies.sh) | Inbound/outbound content-type allowlists, `accept_reputation_attestations: true` |
| [setup_chain_keys.sh](setup_chain_keys.sh) | Chain-a-only `mc-linker-a`/`mc-linker-a2` and chain-b-only `mc-owner-b` |
| [lib_multichain.sh](lib_multichain.sh) | `cli_a`/`cli_b`/`qcli_a`/`qcli_b` wrappers, `wait_for_ibc_delivery`, governance helpers |
| [test_crosschain_content.sh](test_crosschain_content.sh) | A↔B round-trips, dedup, disallowed-type, **silent-SendPacket regression** |
| [test_crosschain_identity.sh](test_crosschain_identity.sh) | Full Phase 1 + Phase 2 link verification |
| [test_crosschain_reputation.sh](test_crosschain_reputation.sh) | Reputation attestation IBC round-trip with PROVISIONAL discount |
| [run_all_multichain_tests.sh](run_all_multichain_tests.sh) | Master orchestrator |

State written at runtime (gitignored):
- `data/chain-a/`, `data/chain-b/` — chain homes
- `data/hermes.log` — relayer log
- `.ibc_channels`, `.chain_keys`, `.chain_pids`, `.hermes_pid` — derived ids and pids
- `.hermes_mnemonics/` — generated relayer seeds (mode 600)
- `proposals/` — generated council proposal JSON

## Troubleshooting

### Hermes channel handshake hangs forever

Most often: the federation IBC port is not bound. Verify with:
```bash
grep AddRoute app/ibc.go | grep federation
```
Expect a line referencing `federationmoduletypes`. If missing, channel creation calls succeed but `OnChanOpenInit` is never reached, so the channel never transitions past INIT. [check_prereqs.sh](check_prereqs.sh) checks this.

### Hermes "insufficient fee" errors on relay

`gas_price` in [hermes_config.toml](hermes_config.toml) must be ≥ chain `minimum-gas-prices`. The suite pins both to mutually compatible values (chain: `0.001uspark`, hermes: `0.0025uspark`). If you change one, change the other.

### Tests pass but `OnRecv` never fires on the counterparty

Make sure the hermes daemon is actually running. `setup_ibc.sh` only opens the channel; `start_relayer.sh` is what scans for and submits packets. The orchestrator calls both, but a manual run can skip the daemon.

```bash
ps -ef | grep "hermes.*hermes_config" | grep -v grep
tail -50 data/hermes.log
```

### Chains not stopping between runs

Old `stop_chains.sh` matched on chain-id, which doesn't appear in `sparkdreamd start` argv. The current version matches on `data/chain-[ab]` and works correctly. If you have stragglers from a pre-fix run:
```bash
pkill -9 -f sparkdreamd
pkill -9 -f hermes
```

### Channel discovery fails after `setup_ibc.sh`

Hermes' `query channels` output format varies slightly between versions. The script tries `--show-counterparty` first, then falls back to a plain `grep -oE 'channel-[0-9]+'`. If both fail, run manually:
```bash
hermes --config test/federation/multichain/hermes_config.toml query channels --chain fedtest-a
```
and edit `.ibc_channels` directly to recover.

### "PROVISIONAL" discount cap on reputation attestations

By design. Federated reputation is bridged at heavy discount (capped at trust level 1, 30-day TTL). See [docs/x-federation-spec.md](../../../docs/x-federation-spec.md).

## Reference

- Plan / design rationale: [docs/plans/2026-04-25-federation-multichain-e2e.md](../../../docs/plans/2026-04-25-federation-multichain-e2e.md) (in temp/ until merged)
- Module spec: [docs/x-federation-spec.md](../../../docs/x-federation-spec.md)
- Single-chain tests: [..](..)
