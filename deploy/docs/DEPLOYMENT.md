# SparkDream Deployment Guide

Step-by-step guide to deploying a SparkDream validator with sentry architecture on Akash Network, private mesh networking via Headscale, and permanent block archival to Arweave.

## Prerequisites

- `sparkdreamd` binary built from source
- Docker installed for building container images
- Akash CLI or [Akash Console](https://console.akash.network) access with funded wallet
- An Arweave wallet with AR tokens (for block archival)
- A home machine for TMKMS and archive node (optional but recommended)

## Phase 1: Build and Push Docker Images

Set `VERSION` to the latest release tag (check the repo's tags or releases):

```bash
VERSION=v1.0.12  # replace with latest version
NETWORK=devnet  # devnet, testnet, or mainnet

# Build base image and SSH image
make docker-build-${NETWORK}-ssh VERSION=$VERSION

# Push to Docker Hub (or your registry)
docker push sparkdreamnft/sparkdreamd-${NETWORK}:$VERSION
docker push sparkdreamnft/sparkdreamd-${NETWORK}-ssh:$VERSION
```

## Phase 2: Deploy Headscale Coordination Server

Headscale manages the encrypted mesh network between your nodes.
Deploy it on a **different Akash provider** than your validator and sentry.

### Files

| File | Purpose |
|------|---------|
| `mesh/Dockerfile-headscale-alpine` | Multi-stage Dockerfile (Alpine + headscale + litestream + age + s5cmd) |
| `mesh/headscale-config.yaml` | Default Headscale config bundled into the image (sole-relay embedded DERP) |
| `mesh/entrypoint.sh` | Restores static keys from S3, copies default config on first run, then exec's `litestream replicate -exec "headscale serve"` |
| `mesh/headscale.sdl.yaml` | Akash SDL — Cloudflare-fronted, litestream env vars, age key overrides |
| `mesh/seed-replica.sh` | Host-side seed: hardened SQLite checks + uploads encrypted `state-keys.tar.age` |
| `mesh/migrate-schema.sh` | Rebuilds the DB with the canonical schema when atlas rejects an older one (used during headscale version bumps) |

### Architecture overview

- **Litestream** replicates the SQLite DB to an S3-compatible bucket on every WAL frame so a fresh PVC on a new provider can restore the full mesh state. RPO ≈ 10s.
- **State-key archive** (`state-keys.tar.age`) holds `noise_private.key` + `derp_server_private.key` — files litestream cannot replicate (it only ships SQLite). Without these, a fresh headscale generates new keys and every existing client rejects the new control plane.
- **Cloudflare** terminates TLS and proxies HTTP(S)/WebSocket to the Akash container on port 80. The container itself speaks plain HTTP; the SDL exposes `as: 80` with `accept: <your-hostname>`.

### Generate backup credentials (one-time, before first deploy)

These secrets live in the SDL env block — pick names you don't already use elsewhere.

```bash
# Age keypair for client-side encryption of the state archive.
# Stash AGE_IDENTITY offline (password manager + paper). Losing it makes
# every encrypted blob in the bucket unrecoverable.
age-keygen -o headscale-backup-age.key
cat headscale-backup-age.key
# => # public key: age1...
#    AGE-SECRET-KEY-1...

# S3 bucket: create on your provider of choice (Cloudflare R2, Backblaze B2,
# 4everland, Wasabi, MinIO, etc.). Mint a NEW bucket-scoped access key —
# never the root credential, since the Akash provider sees the env vars.
# Note: for R2 use LITESTREAM_S3_REGION=auto; for everyone else, us-east-1
# is the safe universal default.
```

### Build and push the Headscale image

```bash
docker build \
  -f deploy/mesh/Dockerfile-headscale-alpine \
  -t sparkdreamnft/headscale:v0.28.0 \
  deploy/mesh/

docker push sparkdreamnft/headscale:v0.28.0
```

The build context is `deploy/mesh/` so `headscale-config.yaml` and `entrypoint.sh` are picked up from that directory. To customize the default config before building, edit `mesh/headscale-config.yaml` — it is baked into the image at `/opt/headscale/default-config.yaml` and copied to `/etc/headscale/config.yaml` on first boot only.

To bump the Headscale version, update the `FROM headscale/headscale:<version>` line in the Dockerfile and the image tag. See [Updating Headscale](#updating-headscale) below for the schema-migration step that often follows.

### Deploy and configure

1. Edit `mesh/headscale.sdl.yaml`:
   - Set `LITESTREAM_S3_ENDPOINT` / `LITESTREAM_S3_BUCKET` / `LITESTREAM_S3_REGION` / `LITESTREAM_S3_ACCESS_KEY_ID` / `LITESTREAM_S3_SECRET_ACCESS_KEY` to your bucket
   - Set `AGE_RECIPIENT` to the `age1...` public key, `AGE_IDENTITY` to the `AGE-SECRET-KEY-1...` private key
   - Edit `accept:` under the port-8080 expose block to your DNS hostname (e.g. `headscale.example.io`)

2. Deploy via Akash Console on a provider that doesn't sit behind tight egress filtering (s5cmd needs outbound HTTPS to your bucket endpoint)

3. Configure Cloudflare:
   - DNS: A record `headscale.example.io` → provider IP, proxied (orange cloud)
   - SSL/TLS mode: **Flexible** (Cloudflare terminates TLS, origin speaks plain HTTP)
   - Network: enable **WebSockets** (headscale's control-plane stream uses them)

4. SSH into the Akash container and set `server_url` to the public Cloudflare URL:

```bash
sed -i 's|http://CHANGE_ME:8080|https://headscale.example.io|' \
  /etc/headscale/config.yaml
kill 1   # restart so headscale re-reads; persistent volumes survive
```

5. Create user and pre-auth keys:

```bash
headscale users create sparkdream
# Note the numeric user ID from:
headscale users list

# Validator key (replace <USER_ID> with the numeric ID from above)
headscale preauthkeys create --user <USER_ID> --reusable --expiration 8760h
# Save output as VALIDATOR_AUTHKEY

# Sentry key
headscale preauthkeys create --user <USER_ID> --reusable --expiration 8760h
# Save output as SENTRY_AUTHKEY

# Home LAN key
headscale preauthkeys create --user <USER_ID> --reusable --expiration 8760h
# Save output as HOME_AUTHKEY
```

6. **Seed the backup** (one-time, after first deploy is healthy). This uploads the
   first litestream snapshot AND `state-keys.tar.age` so a future disaster-recovery
   deploy on a new provider can fully restore the mesh. Run on your host (not in
   the container — `seed-replica.sh` is host-side):

```bash
# Install host-side deps if missing
sudo apt install -y age sqlite3 litestream
curl -fsSL https://github.com/peak/s5cmd/releases/download/v2.2.2/s5cmd_2.2.2_Linux-64bit.tar.gz \
  | sudo tar -xz -C /usr/local/bin s5cmd

# Copy the headscale DB + static keys from the Akash container to the host
ssh -p <ssh_port> -i ~/.ssh/your_key root@<provider> \
    'tar czf - /var/lib/headscale/db.sqlite /var/lib/headscale/db.sqlite-{shm,wal} \
       /var/lib/headscale/noise_private.key /var/lib/headscale/derp_server_private.key 2>/dev/null' \
  | tar xzf - -C /tmp/headscale-source/

# Run the seed script (validates the DB has user tables + nodes, then ships it)
export LITESTREAM_S3_ENDPOINT=...        # same values as in the SDL
export LITESTREAM_S3_BUCKET=...
export LITESTREAM_S3_PATH=archive
export LITESTREAM_S3_REGION=...
export LITESTREAM_S3_ACCESS_KEY_ID=...
export LITESTREAM_S3_SECRET_ACCESS_KEY=...
export AGE_RECIPIENT=age1...
export AGE_IDENTITY=AGE-SECRET-KEY-1...
./deploy/mesh/seed-replica.sh /tmp/headscale-source/var/lib/headscale/db.sqlite
```

   The script refuses to upload if the DB is empty (zero user tables) — this is the
   guard against a silently-corrupted source that would have shipped a schema-less
   replica. It also prints a summary of users/nodes/active preauth keys so you can
   eyeball that the mesh you're seeding is the one you expect.

## Phase 3: Prepare Chain Data

If starting a new chain:

```bash
# On your local machine
source deploy/config/network/$NETWORK/chain.env
sparkdreamd init validator --chain-id "$CHAIN_ID" --home ~/.sparkdream
# Configure genesis, add accounts, create gentx, etc.
```

If joining an existing chain:

```bash
# Get genesis from the repo or another operator
cp deploy/config/network/$NETWORK/genesis.json ~/.sparkdream/config/genesis.json
```

Package the chain data for upload:

```bash
tar czf validator-data.tgz -C ~/.sparkdream .
```

## Phase 4: Deploy Validator

1. Edit `config/network/<network>/validator.sdl.yaml` (e.g., `devnet/validator.sdl.yaml`):
   - Set your `SSH_PUBLIC_KEY`
   - Set `HEADSCALE_URL` to your Headscale address
   - Set `TS_AUTHKEY` to VALIDATOR_AUTHKEY
   - Set `WAIT_FOR_CONFIG=true` for initial deploy
   - Leave `PRIVVAL_KEEPALIVE_PORT=26659` set (default). This enables the keepalive
     proxy in `entrypoint_ssh.sh` section 5c; required for stable remote-signer
     (tmkms) operation over tailscale-userspace + DERP. The proxy is configured via
     the optional `PRIVVAL_BACKEND_PORT` / `PRIVVAL_KEEPIDLE` / `PRIVVAL_KEEPINTVL`
     / `PRIVVAL_KEEPCNT` env vars — defaults work for most setups.
   - Leave `STARTUP_DELAY=20` set. The entrypoint sleeps this many seconds after
     Tailscale comes up so tmkms has time to dial in before sparkdreamd's first
     consensus round. Without this delay a freshly-restarted validator can panic
     on block 1 because the privval socket isn't accepting yet, triggering an
     Akash crash-loop.

2. Deploy on Akash (choose a **different provider** than Headscale)

3. Note the forwarded SSH port, then upload chain data:

```bash
scp -O -P <ssh_port> -i ~/.ssh/your_key \
  validator-data.tgz root@<provider>:/root/.sparkdream/

# SSH in and extract
ssh -p <ssh_port> -i ~/.ssh/your_key root@<provider>
cd /root/.sparkdream
tar xzf validator-data.tgz
rm validator-data.tgz
```

4. Verify Tailscale is connected:

```bash
# Inside Akash containers, tailscaled uses a custom socket path
tailscale --socket=$TS_STATE_DIR/tailscaled.sock status
tailscale --socket=$TS_STATE_DIR/tailscaled.sock ip -4
# Note the validator's Tailscale IP (e.g., 100.64.0.10)
```

5. Update `config.toml` for sentry peering (will do after sentry deploys):

```bash
# For now, confirm the node starts
sparkdreamd start --home /root/.sparkdream
# If it works, stop it (Ctrl+C) and proceed
```

6. Redeploy with `WAIT_FOR_CONFIG` removed or set to `false`

## Phase 5: Deploy Sentry

1. Initialize sentry chain data on your local machine:

```bash
source deploy/config/network/$NETWORK/chain.env
sparkdreamd init sentry --chain-id "$CHAIN_ID" --home ~/.sparkdream-sentry
cp deploy/config/network/$NETWORK/genesis.json ~/.sparkdream-sentry/config/genesis.json
```

2. Apply the sentry config templates via `envsubst`:

```bash
source deploy/config/network/$NETWORK/chain.env
envsubst < deploy/config/template/config.toml.sentry  > ~/.sparkdream-sentry/config/config.toml
envsubst < deploy/config/template/app.toml.sentry     > ~/.sparkdream-sentry/config/app.toml
envsubst < deploy/config/template/client.toml.sentry  > ~/.sparkdream-sentry/config/client.toml
```

3. Edit `config/network/<network>/sentry.sdl.yaml`:
   - Set your `SSH_PUBLIC_KEY`
   - Set `HEADSCALE_URL`
   - Set `TS_AUTHKEY` to SENTRY_AUTHKEY
   - Set `TS_TUNNEL_1` to `16656:<validator_tailscale_ip>:26656`
   - Set `WAIT_FOR_CONFIG=true`

4. **Important**: Update `persistent_peers` in the sentry's `config.toml` to use the local
   tunnel instead of the Tailscale IP directly. Akash containers run Tailscale in userspace
   networking mode (no `NET_ADMIN` capability), so the Tailscale IP is not a real kernel
   interface. TCP connections between containers are tunneled via `socat` + `tailscale nc`:

```bash
# In the sentry's config.toml, use 127.0.0.1:16656 (the local socat tunnel)
# instead of <tailscale_ip>:26656
persistent_peers = "<validator_node_id>@127.0.0.1:16656"
```

5. Deploy on Akash (different provider than validator and Headscale)

6. Upload sentry data, SSH in, verify Tailscale and tunnel:

```bash
# Verify Tailscale is connected
tailscale --socket=$TS_STATE_DIR/tailscaled.sock status

# Verify the tunnel is listening
netstat -tlnp | grep 16656

# Test the tunnel reaches the validator
nc -zv 127.0.0.1 16656
```

## Phase 6: Configure Peering Over Tailscale

Now both nodes are on the mesh. Get their node IDs:

```bash
# On validator
sparkdreamd tendermint show-node-id --home /root/.sparkdream
# e.g., abc123...

# On sentry
sparkdreamd tendermint show-node-id --home /root/.sparkdream
# e.g., def456...
```

Update the peer variables in your `chain.env` (or export them directly), then regenerate configs with `envsubst`:

**On the validator**:

```bash
source deploy/config/network/$NETWORK/chain.env
export SENTRY_NODE_ID="def456..."
export SENTRY_HOST="100.64.0.2"
export SENTRY_PORT="26656"
envsubst < deploy/config/template/config.toml.validator > /root/.sparkdream/config/config.toml
envsubst < deploy/config/template/app.toml.validator    > /root/.sparkdream/config/app.toml
envsubst < deploy/config/template/client.toml.validator > /root/.sparkdream/config/client.toml
```

**On the sentry** — use the local socat tunnel (127.0.0.1:16656), not the Tailscale IP directly:

```bash
source deploy/config/network/$NETWORK/chain.env
export VALIDATOR_NODE_ID="abc123..."
export VALIDATOR_HOST="127.0.0.1"
export VALIDATOR_PORT="16656"
envsubst < deploy/config/template/config.toml.sentry > /root/.sparkdream/config/config.toml
envsubst < deploy/config/template/app.toml.sentry    > /root/.sparkdream/config/app.toml
envsubst < deploy/config/template/client.toml.sentry > /root/.sparkdream/config/client.toml
```

**On the validator** — two critical config changes for Tailscale userspace networking:

1. Bind the privval listener to the **backend port** (`127.0.0.1:26660`), not the
   tailnet-facing port. The entrypoint runs a `socat` keepalive proxy in front of it
   (see `PRIVVAL_KEEPALIVE_PORT` in the SDL — section 5c of `entrypoint_ssh.sh`).
   Without this proxy, the privval TCP connection drops between sign requests over
   tailscale-userspace + DERP and the validator misses prevotes/precommits. With it,
   the kernel sends SO_KEEPALIVE probes through the tunnel on a 10/5/3-second cadence
   so the connection never goes idle long enough to be torn down:

```bash
sed -i 's|^priv_validator_laddr.*|priv_validator_laddr = "tcp://127.0.0.1:26660"|' \
  /root/.sparkdream/config/config.toml
```

   tmkms continues to dial the validator's tailnet IP on `26659` (the keepalive proxy's
   public-facing port); only the validator's *internal* bind moves.

2. Allow duplicate IPs. Because sentries connect through socat tunnels, the validator sees
   all inbound sentry connections as coming from `127.0.0.1`. CometBFT deduplicates by
   remote IP by default, so only the first sentry can connect. This setting allows multiple
   peers from the same IP:

```bash
sed -i 's|^allow_duplicate_ip = .*|allow_duplicate_ip = true|' \
  /root/.sparkdream/config/config.toml
```

Redeploy both nodes with `WAIT_FOR_CONFIG=false`.

## Phase 7: Connect Home LAN Nodes

### TMKMS

Install Tailscale on your TMKMS machine:

```bash
# Linux
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up \
  --login-server=http://HEADSCALE_PROVIDER:PORT \
  --authkey=HOME_AUTHKEY \
  --hostname=tmkms
```

Update TMKMS config to connect to the validator via Tailscale. Use the **validator's**
tailnet IP (visible in `headscale nodes list` or `tailscale status` on the validator);
the placeholder `100.64.0.10` below is illustrative — substitute your own. Port stays
at `26659` (the keepalive proxy's public-facing port — the proxy forwards internally to
`127.0.0.1:26660` where sparkdreamd binds):

```toml
[[validator]]
addr = "tcp://100.64.0.10:26659"
chain_id = "sparkdream-1"  # match your CHAIN_ID from chain.env
reconnect = true
```

### Archive Node (optional)

On a machine with sufficient storage:

```bash
source deploy/config/network/$NETWORK/chain.env
sparkdreamd init archive --chain-id "$CHAIN_ID" --home ~/.sparkdream
cp deploy/config/network/$NETWORK/genesis.json ~/.sparkdream/config/genesis.json

# Set pruning to nothing for full history
sed -i 's/^pruning *=.*/pruning = "nothing"/' ~/.sparkdream/config/app.toml

# Peer with validator over Tailscale (home machines use kernel Tailscale with
# a real TUN interface, so they can connect directly to Tailscale IPs — no tunnel needed)
sed -i 's|^persistent_peers.*|persistent_peers = "abc123...@<validator_tailscale_ip>:26656"|' \
  ~/.sparkdream/config/config.toml

# Join the mesh
sudo tailscale up \
  --login-server=http://HEADSCALE_PROVIDER:PORT \
  --authkey=HOME_AUTHKEY \
  --hostname=archive

sparkdreamd start --home ~/.sparkdream
```

## Phase 8: Set Up Block Archival

Run the block archiver on the sentry (which has public RPC):

```bash
# SSH into the sentry

# Run the archiver
RPC_URL=http://localhost:26657 \
OUTPUT_DIR=/root/.sparkdream/archives \
  ./block-archiver.sh
```

Download archives to your local machine and upload to Arweave:

```bash
# Download
scp -O -P <sentry_ssh_port> -i ~/.ssh/your_key \
  root@<sentry_provider>:/root/.sparkdream/archives/*.jsonl.gz \
  ./archives/

# Upload to Arweave
./arweave-upload.sh -w ~/arweave-wallet.json ./archives/

# Or upload to Storacha
./storacha-upload.sh ./archives/
```

## Verification Checklist

After completing all phases, verify:

- [ ] Headscale shows all nodes connected: `headscale nodes list`
- [ ] Validator and sentry are peered: check logs for "peer connected"
- [ ] Sentry RPC is accessible: `curl http://<sentry_provider>:<rpc_port>/status`
- [ ] Sentry P2P is accessible: other nodes can peer with it
- [ ] TMKMS is signing blocks: check validator logs for signed precommits
- [ ] Validator has no public ports (only SSH, which can be removed later)
- [ ] Block archiver runs successfully
- [ ] Archives upload to Arweave/Storacha

## Ongoing Operations

### Restarting a node

The container restarts automatically on Akash. To force a restart,
update the SDL (even a comment change) and redeploy. Persistent
storage survives redeployments on the same provider.

### Updating the sparkdreamd binary

1. Build new Docker image with updated sparkdreamd
2. Push to registry
3. Update image tag in SDL
4. Redeploy

### Updating Headscale

1. Update the `FROM headscale/headscale:<version>` line in `mesh/Dockerfile-headscale-alpine`
2. Rebuild and push: `docker build -f deploy/mesh/Dockerfile-headscale-alpine -t sparkdreamnft/headscale:<version> deploy/mesh/ && docker push sparkdreamnft/headscale:<version>`
3. Update the `image:` field in `mesh/headscale.sdl.yaml`
4. Redeploy — persistent volumes retain the config and SQLite database

**Schema-validation gotcha.** Recent headscale versions run `atlas` schema
validation at startup and reject DBs whose table definitions don't match
byte-for-byte — column order, backtick-quoted identifiers, and obsolete
`migrations` table entries all cause failures even when the data is intact.
If the container logs show `SQLite schema failed to validate` followed by
a long `+ CREATE TABLE ...` diff, run `mesh/migrate-schema.sh` on a host-side
copy of the DB to rebuild it with the canonical schema, then re-seed via
`seed-replica.sh`:

```bash
# Pull the current DB out of the running container
scp -P <ssh_port> root@<provider>:/var/lib/headscale/db.sqlite ./db.sqlite

# Rebuild against the canonical schema (preserves data via named INSERTs)
./deploy/mesh/migrate-schema.sh ./db.sqlite

# Clear orphaned litestream generations and re-seed
AWS_ACCESS_KEY_ID=$LITESTREAM_S3_ACCESS_KEY_ID \
AWS_SECRET_ACCESS_KEY=$LITESTREAM_S3_SECRET_ACCESS_KEY \
  s5cmd --endpoint-url "$LITESTREAM_S3_ENDPOINT" \
        rm "s3://$LITESTREAM_S3_BUCKET/$LITESTREAM_S3_PATH/generations/*"

./deploy/mesh/seed-replica.sh ./db.migrated.sqlite

# In the container, wipe the broken DB and restart so litestream re-pulls
ssh root@<provider> "rm -f /var/lib/headscale/db.sqlite* && \
                     rm -rf /var/lib/headscale/.db.sqlite-litestream && \
                     kill 1"
```

The static keys on the PVC (`noise_private.key`, `derp_server_private.key`)
are preserved across this dance, so clients re-attach with their pinned
machine keys — no re-auth required.

### Regenerating genesis files

`deploy/scripts/regenerate-network-genesis.py` rebuilds the per-network
`genesis.json` files from `config.yml` overrides + the script's account
constants. Existing gentxs are carried forward and structurally validated
(chain_id, signer accounts).

The script also writes a sibling `genesis.json.gentx-hashes` file the first
time it sees a gentx, recording the SHA-256 of the canonical gentx JSON.
On every subsequent run it recomputes the hash and **refuses to ship a
modified gentx** — this catches the bug class where a migration silently
rewrites bytes inside a signed gentx (e.g. denom substitution) without
re-signing, which would otherwise surface only as a chain-start panic.

Workflow:

- **First run** for a network: review the printed notice, confirm the
  gentx denom matches the chain's `bond_denom`, then commit both
  `genesis.json` and `genesis.json.gentx-hashes`.
- **Operator regenerates a gentx legitimately** (new chain_id, new
  validator key, etc.): delete the `.gentx-hashes` file, swap in the
  new gentx, re-run the script to record a new baseline, commit both.

### Rotating Tailscale keys

Pre-auth keys are only used for initial join. After that, the node
uses its stored state. To rotate, generate a new key on Headscale,
update the SDL env var, and redeploy.

### Monitoring

- Check node status: `curl http://<sentry>:<rpc_port>/status | jq .result.sync_info`
- Check mesh health: `tailscale --socket=$TS_STATE_DIR/tailscaled.sock status` on Akash containers
- Check tunnels: `netstat -tlnp | grep socat` on Akash containers
- Check storage: `du -sh /root/.sparkdream/data/*/` via SSH

### Disaster Recovery

See [archival-strategy.md](archival-strategy.md) for full recovery
procedures using archived blocks from Arweave. The `replay-from-archive`
command can reconstruct the chain from any starting state plus
incremental block archives.