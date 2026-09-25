# Federation relayer: sparkdream-dev-1 ↔ sparkdream-test-1

Written for: whoever is bringing the live devnet/testnet federation link up.

Hermes config and bring-up script for the IBC channel that x/federation packets
travel over. The local two-chain equivalent lives in
[test/federation/multichain/](../../test/federation/multichain/); this is the
same shape pointed at deployed networks.

The same bring-up is also packaged as a container,
[`deploy/docker/Dockerfile-hermes`](../docker/Dockerfile-hermes), which the chain
launcher deploys as its relayer component. Its
[`relayer-bringup`](../docker/hermes/relayer-bringup.sh) generalises
`bringup.sh` to any number of paths (ICS-20 `transfer` and `federation`
channels, to any counterparty) and keys connections per chain pair rather than
"any connection exists". The scripts here stay the hand-run path for the
canonical dev/test link.

## After a chain reset — start here

Resetting either chain destroys everything this link is built on: IBC clients,
connections, channels, peer records, relayer balances, and the local full nodes
hermes talks to. Rebuilding it is one command:

```bash
./relink.sh
```

That rebuilds the network-tagged binaries, re-initialises and re-syncs the local
full nodes, funds the hermes relayers, opens the client/connection/channel, and
registers + activates the devnet peer. It is idempotent — re-run it after
fixing any failure and it resumes rather than duplicating.

**Stop the local nodes before you reset the chains**, or do the reset and let
`relink.sh` stop them for you. A node still running against the pre-reset chain
will peer with the sentry and wedge it: the CometBFT p2p handshake authenticates
**chain-id only, never the genesis hash**, so a stale node is accepted as a peer
and then serves garbage. This is the single most common way to break the devnet
while trying to fix it.

The one thing `relink.sh` cannot do is the **testnet half of the peer setup** —
that needs `kingofbitchain`, whose key is deliberately not on this machine. The
script prints the exact UI steps when it finishes. See
[The testnet half](#the-testnet-half) below.

### Checking the current state

Rather than a table here that goes stale on every reset, ask the chains:

```bash
./local_nodes.sh status                     # both nodes synced?
./fund_check.sh                             # relayer keys funded + auth account?
./setup_peers.sh                            # re-run: reports both peers, non-zero if not ACTIVE
hermes --config hermes_config.toml query channels --chain sparkdream-dev-1 --show-counterparty
```

`setup_peers.sh` is the useful one: it is idempotent and ends with a RESULT
block showing each chain's peer status and bound channel. Content moves only
when **both** sides are ACTIVE — a peer that is ACTIVE on one chain and PENDING
on the other silently drops packets rather than erroring.

Standing facts that do not change per reset:

| | sparkdream-dev-1 | sparkdream-test-1 |
|---|---|---|
| RPC | `https://rpc-dev.sparkdream.io` | `https://rpc-test.sparkdream.io` |
| gRPC | not exposed — use a local node | not exposed — use a local node |
| Fee denom | `usparz.sparkdreamdev` | `uspark.sparkdreamtest` |
| Local node RPC | `127.0.0.1:26657` | `127.0.0.1:36657` |
| Local node gRPC | `127.0.0.1:9090` | `127.0.0.1:9091` |
| Ops Committee member | `alice` (key is local) | `kingofbitchain` (key is NOT local) |

## Reaching gRPC

This is the one real blocker. Hermes needs gRPC for account queries and tx
simulation, and [app.toml.sentry](../config/template/app.toml.sentry) binds it
to `localhost:9090`. The sentry SDLs expose only 2222, 26656 and 26657, so
there is no gRPC to reach from outside.

**Run local full nodes** ([local_nodes.sh](local_nodes.sh)) — the recommended
path, and the one verified working here. A local node serves gRPC on localhost
natively, so nothing about the deployment has to change and you do not need the
sentry's SSH key. Both chains' P2P ports are publicly dialable and their genesis
is downloadable over RPC, so this is entirely self-service:

```bash
./local_nodes.sh setup && ./local_nodes.sh start
./local_nodes.sh status      # until catching_up=False on both
```

**Each chain needs its own binary, built with that network's build tag.**
`x/federation`, `x/shield` and `x/identity` carry `//go:build devnet|testnet|
mainnet` genesis defaults, so an untagged build computes a different app hash
and the node dies at block 1:

```
Error in validation  err="wrong Block.Header.AppHash. Expected 03ECF673..., got 028E2CF9..."
```

That is not a corrupt genesis and no amount of re-downloading fixes it. Build
both first:

```bash
cd <chain repo>
CGO_ENABLED=0 go build -tags devnet  -o ~/.local/bin/sparkdreamd-devnet  ./cmd/sparkdreamd/main.go
CGO_ENABLED=0 go build -tags testnet -o ~/.local/bin/sparkdreamd-testnet ./cmd/sparkdreamd/main.go
```

The version must also match what each network runs — check it with `curl -s
https://rpc-dev.sparkdream.io/abci_info`.

Two alternatives, if you would rather not run nodes:

**SSH tunnel via the sentry's port 2222.** The sentry image
(`sparkdreamnft/sparkdreamd-devnet-ssh`) runs sshd, keyed by `SSH_PUBLIC_KEY`
in the SDL. Needs the matching private key and the Akash-mapped external port
(`akash provider lease-status`). Tunnelling to `localhost:9090` on the sentry
works with the existing bind, no redeploy:

```bash
ssh -p <mapped> -N -L 9090:localhost:9090 root@<provider host>
```

**Tailscale mesh.** The nodes are on a headscale mesh ([deploy/mesh/](../mesh/)).
Join the relayer host, set `address = "0.0.0.0:9090"` in each sentry's app.toml,
redeploy, and point `grpc_addr` at the mesh IPs.

## Hermes version

The multichain suite asks for **1.13.3 or newer** (1.13.0 was the first release
with full ibc-go/v10 support). **`hermes version` cannot tell you which you
have:** the v1.13.3 release binary reports itself as `v1.13.2+bab3b80`
(upstream did not bump the version string). Check the file hash instead; the
v1.13.3 x86_64 binary is
`ed8d57781f57a93e74259c12c8feee661b9633c882307f3a6236e41dbf5ffe35`, and the
one on this dev box matches it. To install it:

```bash
HERMES_VERSION=v1.13.3
curl -fsSL "https://github.com/informalsystems/hermes/releases/download/${HERMES_VERSION}/hermes-${HERMES_VERSION}-x86_64-unknown-linux-gnu.tar.gz" \
  | tar -xz -C ~/.local/bin hermes
```

## Gas price — why it is 0.025

Applied and deployed; kept here because the failure it caused was baffling and
someone will be tempted to "fix" the value back.

`deploy/config/network/*/chain.env` was corrected: `MIN_GAS_PRICES` was
`25000<denom>`, a flat `--fees` amount copied into a field that wants a price
**per gas unit**. At 300k gas that charged 7,500 SPARK per transaction. It is
now `0.025<denom>` on all three networks, and the devnet's `DENOM` was also
corrected to `usparz.sparkdreamdev` to match the running chain.

`minimum-gas-prices` is node-local config, not consensus — no chain change, no
upgrade, no governance vote. But the running nodes still carry the old value
until their `app.toml` is regenerated and they restart:

```bash
set -a; . deploy/config/network/testnet/chain.env; set +a
envsubst < deploy/config/template/app.toml.sentry > $NODE_HOME/config/app.toml
# restart the node
```

Do the same for the validator with `app.toml.validator`. Verify from outside:

```bash
curl -s https://api-test.sparkdream.io/cosmos/base/node/v1beta1/config
curl -s https://api-dev.sparkdream.io/cosmos/base/node/v1beta1/config
```

Both should report `0.025...`. **Check this before running `bringup.sh`** — the
config here now assumes 0.025, so if the testnet node still enforces 25000
every relayer tx is rejected with "insufficient fees" partway through the
handshake.

The devnet currently reports `""` (no minimum), which is why it works today at
any price; after the fix it will enforce 0.025 like the others.

## Relayer keys

The script deliberately does not create or fund keys; on a live network these
are real accounts. For each chain:

1. `hermes keys add --chain <id> --mnemonic-file <file>`, using key names
   `relayer-dev` and `relayer-test` to match the config.
2. Fund each address in its chain's own fee denom (see the table above —
   they differ, and the wrong one is rejected by min-gas-prices). `relink.sh`
   does this automatically, from `alice` on devnet and `bob` on testnet, and
   skips the top-up when the balance is already above `FUND_FLOOR`. The keys
   survive a chain reset — same mnemonic, same address — but the **balances do
   not**, because the new genesis has never heard of them.
3. **Send one self-transfer from each relayer key before running the script.**
   `bank.SendCoins` does not create an `auth.BaseAccount` for the recipient, so
   a freshly funded address exists in bank state but returns NotFound from
   `query auth account` — and Hermes aborts with `account <addr> not found`. A
   self-send signed by the key itself materializes the account from the tx's
   pubkey. The local suite hit this too; see the `register_account` comment in
   [setup_ibc.sh](../../test/federation/multichain/setup_ibc.sh).

## Bring-up

```bash
./bringup.sh                               # health check, clients, connection, channel
hermes --config hermes_config.toml start   # the daemon; channels alone relay nothing
```

**If a hermes command prints its banner and then just sits there, gRPC is
unreachable.** It dials forever with no error and no timeout — verified here
against a dead port, where `query channels` hung until killed and emitted
nothing but the startup line. `bringup.sh` wraps its health check in a hard
timeout for this reason, so it reports the problem instead of freezing. To
check by hand:

```bash
ss -ltn | grep -E '9090|9091'   # your tunnels
```

RPC is fine over Cloudflare, including the `abci_query` path Hermes uses — that
was confirmed separately, so a hang is gRPC and not the CDN.

`bringup.sh` is idempotent — `hermes create ...` is not, so each step is guarded
by a query and skipped when the object exists. It writes the resulting channel
ids to `.ibc_channels`.

## Then: the chain side

`./setup_peers.sh` does this, and `relink.sh` calls it. Run it directly when
only the peer half needs redoing:

```bash
SIDES=dev ./setup_peers.sh        # devnet only (the default)
DRY_RUN=1 SIDES=dev ./setup_peers.sh   # print the txs, broadcast nothing
```

**Order matters, and it is not the obvious one.** `ibc_channel_id` is written
only at `MsgRegisterPeer` ([msg_server_register_peer.go](../../x/federation/keeper/msg_server_register_peer.go));
no other message sets it and no handshake callback fills it in. A peer
registered before the channel exists can never be pointed at one — it has to be
removed and re-registered. So the channel comes first, always, which is why
`bringup.sh` runs before `setup_peers.sh`.

Each direction is three steps, and the third one changed:

1. **`MsgRegisterPeer`** with `ibc_channel_id` — a single Operations Committee
   member's signature. Lands PENDING.
2. **`MsgUpdatePeerPolicy`** — also a single member. A registered peer carries
   the **empty** default policy, so every content list is empty and nothing
   federates in either direction even once ACTIVE. Reputation bridging
   (`allow_reputation_queries`, `accept_reputation_attestations`) is valid here
   because both sides are `PEER_TYPE_SPARK_DREAM`, and `setup_peers.sh` enables
   both by default.
3. **`MsgResumePeer`** — **a committee VOTE, not a signature.** This is the
   trust decision, so it takes the Operations Committee policy address: submit
   a proposal, vote yes, then execute once `min_execution_period` has elapsed
   (5 min devnet, 10 min testnet). A single member signing directly is rejected
   with `ErrNotAuthorized` (2318). `setup_peers.sh` drives all three
   transactions when it holds the key.

At one member and a `percentage` 0.5 decision policy, one yes vote crosses the
threshold and triggers early acceptance, so the 5-day voting period never
applies — the real cost is three transactions plus the min-execution wait.

### The testnet half

`kingofbitchain` is the testnet's only Operations Committee member and that key
is deliberately not on this machine, so drive it from the UI's federation page:

| Step | How |
|---|---|
| Register `sparkdream-dev-1` | Direct signing. Type SPARK_DREAM, `ibc_channel_id` from `.ibc_channels` |
| Edit policy | Direct signing. Same content types both directions; reputation flags on |
| **Activate** | **Proposal path only.** Submit to the Operations Committee policy, vote yes, execute after 10 min |

The activation row is the one that trips people up: the UI still offers direct
council signing for peer messages, and it works for the first two rows. It does
**not** work for activation any more, and the failure is a `2318` at the point
of broadcast rather than anything the form warns about.

Two alternatives if you would rather not use the UI: run `setup_peers.sh` with
`SIDES=test` on the machine that holds the key (it automates all three steps
including the vote), or set `SIGNER_TEST=<address>` here to emit unsigned
transactions into `.unsigned/` for signing elsewhere — though that path can only
emit the proposal submission, leaving the vote and execute to you.

## Checking it worked

```bash
hermes --config hermes_config.toml query channels --chain sparkdream-dev-1 --show-counterparty
```

The channel should be `OPEN`/`OPEN` on the `federation` port. After that, an
identity link created on one chain should reach VERIFIED rather than sitting
UNVERIFIED — which is the useful smoke test, because an unverified link is
pruned after `unverified_link_ttl` (1 hour on devnet) and simply disappears.
