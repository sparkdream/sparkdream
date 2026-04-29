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
VERSION=v1.0.4  # replace with latest version
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
| `mesh/Dockerfile-headscale-alpine` | Multi-stage Dockerfile (Alpine + official Headscale binary) |
| `mesh/headscale-config.yaml` | Default Headscale config bundled into the image |
| `mesh/entrypoint.sh` | Copies default config on first run, then exec's `headscale serve` |
| `mesh/headscale.sdl.yaml` | Akash SDL for deploying the image |

### Build and Push the Headscale Image

```bash
docker build \
  -f deploy/mesh/Dockerfile-headscale-alpine \
  -t sparkdreamnft/headscale:v0.28.0 \
  deploy/mesh/

docker push sparkdreamnft/headscale:v0.28.0
```

The build context is `deploy/mesh/` so both `headscale-config.yaml` and `entrypoint.sh` are picked up from that directory. To customize the default config before building, edit `mesh/headscale-config.yaml` — it is baked into the image at `/opt/headscale/default-config.yaml` and copied to `/etc/headscale/config.yaml` on first boot only.

To bump the Headscale version, update the `FROM headscale/headscale:<version>` line in the Dockerfile and the image tag.

### Deploy and Configure

1. Deploy `mesh/headscale.sdl.yaml` via Akash Console (no changes needed for initial deploy)
2. Note the provider address and forwarded port for 8080 (e.g., `provider.example.com:31234`)
3. Access the Akash shell and update the config:

```bash
sed -i 's|http://CHANGE_ME:8080|http://provider.example.com:31234|' \
  /etc/headscale/config.yaml
```

4. Restart the deployment for config to take effect (on subsequent restarts the existing config is preserved since the entrypoint only copies the default when no config exists)
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
# Note the validator's Tailscale IP (e.g., 100.64.0.1)
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

1. Bind TMKMS listener to all interfaces (Tailscale userspace networking doesn't create a
   kernel interface, so binding to a specific Tailscale IP will fail with "cannot assign
   requested address". Port 26659 is not in the SDL `expose` block, so it is only reachable
   via Tailscale):

```bash
sed -i 's|^priv_validator_laddr.*|priv_validator_laddr = "tcp://0.0.0.0:26659"|' \
  /root/.sparkdream/config/config.toml
```

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

Update TMKMS config to connect to validator via Tailscale:

```toml
[[validator]]
addr = "tcp://100.64.0.1:26659"
chain_id = "sparkdream-1"  # match your CHAIN_ID from chain.env
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