# Federation relayer: sparkdream-dev-1 ↔ sparkdream-test-1

Written for: whoever is bringing the live devnet/testnet federation link up.

Hermes config and bring-up script for the IBC channel that x/federation packets
travel over. The local two-chain equivalent lives in
[test/federation/multichain/](../../test/federation/multichain/); this is the
same shape pointed at deployed networks.

## Current state

Verified against both live chains:

| | sparkdream-dev-1 | sparkdream-test-1 |
|---|---|---|
| RPC | `https://rpc-dev.sparkdream.io` ✅ | `https://rpc-test.sparkdream.io` ✅ |
| WebSocket | `wss://.../websocket` ✅ | `wss://.../websocket` ✅ |
| gRPC | not exposed — use a local node ⚠️ | not exposed — use a local node ⚠️ |
| Min gas price (live) | `""` none set | `25000uspark.sparkdreamtest` — fix pending restart |
| IBC clients | 0 | 0 |
| IBC channels | 0 | 0 |
| Federation peers | 1 (`sparkdream-test-1`, PENDING, no channel) | 0 |
| Fee denom | `usparz.sparkdreamdev` | `uspark.sparkdreamtest` |
| Unbonding | 21 days | 21 days |

Nothing is connected yet, in either direction. The gRPC gap has a clean
workaround (local full nodes, verified). The gas price is fixed in chain.env
but the running nodes need the new app.toml and a restart — see below.

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

The version must also match what each network runs — `curl -s
https://rpc-dev.sparkdream.io/abci_info` (v1.0.38 at time of writing).

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
with full ibc-go/v10 support). The binary currently on this dev box is
**1.13.2** — above the functional floor but below the suite's stated bar, so it
may work and may hit handshake bugs. Worth upgrading before spending time
debugging a failed channel open:

```bash
HERMES_VERSION=v1.13.3
curl -fsSL "https://github.com/informalsystems/hermes/releases/download/${HERMES_VERSION}/hermes-${HERMES_VERSION}-x86_64-unknown-linux-gnu.tar.gz" \
  | tar -xz -C ~/.local/bin hermes
```

## Gas price — apply the node fix before bringing up

`deploy/config/network/*/chain.env` has been corrected: `MIN_GAS_PRICES` was
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
   they differ, and the wrong one is rejected by min-gas-prices). On the
   testnet the only substantially funded account seen is `kingofbitchain`
   (`sprkdrm1yhjdr8kxsrer3kcqpdrc2zd0kggvsj4c3vazkd`, 50,000 SPARK); alice and
   bob are empty there. See "Testnet gas price" for why 50,000 is not enough.
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

**Order matters, and it is not the obvious one.** `ibc_channel_id` is written
only at `MsgRegisterPeer` ([msg_server_register_peer.go](../../x/federation/keeper/msg_server_register_peer.go));
no other message sets it and no handshake callback fills it in. A peer
registered before the channel exists can never be pointed at one — it has to be
removed and re-registered.

The devnet's existing `sparkdream-test-1` peer was registered with an empty
channel, so it needs exactly that treatment:

1. **devnet, Commons Council:** `MsgRemovePeer` for `sparkdream-test-1`. Wait a
   block or two for the EndBlocker to drain `PeerRemovalQueue` — re-registration
   is refused while the entry is still there. (The peer has no bridges, content
   or links, so cleanup is immediate.)
2. **devnet, Commons Council:** `MsgRegisterPeer` with
   `ibc_channel_id = $CHANNEL_DEV`, then `MsgResumePeer` to take it PENDING →
   ACTIVE. Registration and activation are two separate proposals.
3. **testnet, Commons Council:** the mirror image — register `sparkdream-dev-1`
   with `ibc_channel_id = $CHANNEL_TEST`, then activate. Federation is bilateral;
   the testnet currently has no peers at all.
4. **both chains, Operations Committee:** `MsgUpdatePeerPolicy`. A registered
   peer carries the empty default policy, so every content list is empty and
   nothing federates in either direction even once ACTIVE. Reputation bridging
   (`allow_reputation_queries`, `accept_reputation_attestations`) is valid here
   because both sides are `PEER_TYPE_SPARK_DREAM`.

Steps 1–4 are all drivable from the UI's federation page (Propose peer / Edit
policy). Note step 4 is the **Operations Committee**, not the Council — the
Council policy's `allowed_messages` does not include `MsgUpdatePeerPolicy`.

## Checking it worked

```bash
hermes --config hermes_config.toml query channels --chain sparkdream-dev-1 --show-counterparty
```

The channel should be `OPEN`/`OPEN` on the `federation` port. After that, an
identity link created on one chain should reach VERIFIED rather than sitting
UNVERIFIED — which is the useful smoke test, because an unverified link is
pruned after `unverified_link_ttl` (1 hour on devnet) and simply disappears.
