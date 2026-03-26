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
VERSION=v1.0.1  # replace with latest version

# Build base image
docker build -f deploy/docker/Dockerfile-sparkdreamd-alpine -t sparkdreamnft/sparkdreamd:$VERSION .

# Build SSH + Tailscale image
docker build -f deploy/docker/Dockerfile-sparkdreamd-alpine-ssh -t sparkdreamnft/sparkdreamd-ssh:$VERSION .

# Push to Docker Hub (or your registry)
docker push sparkdreamnft/sparkdreamd:$VERSION
docker push sparkdreamnft/sparkdreamd-ssh:$VERSION
```

## Phase 2: Deploy Headscale Coordination Server

Headscale manages the encrypted mesh network between your nodes.
Deploy it on a **different Akash provider** than your validator and sentry.

1. Edit `akash/headscale.sdl.yaml` — no changes needed for initial deploy
2. Deploy via Akash Console
3. Note the provider address and forwarded port for 8080 (e.g., `provider.example.com:31234`)
4. Access the Akash shell and create the config:

```bash
# Update server_url with actual address
cat > /etc/headscale/config.yaml << 'EOF'
# paste contents of mesh/headscale-config.yaml
EOF
sed -i 's|CHANGE_ME:8080|provider.example.com:31234|' /etc/headscale/config.yaml
```

5. Restart the deployment for config to take effect
6. Create user and pre-auth keys:

```bash
headscale users create sparkdream

# Validator key
headscale preauthkeys create --user sparkdream --reusable --expiration 8760h
# Save output as VALIDATOR_AUTHKEY

# Sentry key
headscale preauthkeys create --user sparkdream --reusable --expiration 8760h
# Save output as SENTRY_AUTHKEY

# Home LAN key
headscale preauthkeys create --user sparkdream --reusable --expiration 8760h
# Save output as HOME_AUTHKEY
```

## Phase 3: Prepare Chain Data

If starting a new chain:

```bash
# On your local machine
source deploy/config/network/mainnet/chain.env
sparkdreamd init validator --chain-id "$CHAIN_ID" --home ~/.sparkdream
# Configure genesis, add accounts, create gentx, etc.
```

If joining an existing chain:

```bash
# Get genesis from the repo or another operator
cp deploy/config/network/mainnet/genesis.json ~/.sparkdream/config/genesis.json
```

Package the chain data for upload:

```bash
tar czf validator-data.tgz -C ~/.sparkdream .
```

## Phase 4: Deploy Validator

1. Edit `akash/validator.sdl.yaml`:
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
tailscale status
tailscale ip -4
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
source deploy/config/network/mainnet/chain.env
sparkdreamd init sentry --chain-id "$CHAIN_ID" --home ~/.sparkdream-sentry
cp deploy/config/network/mainnet/genesis.json ~/.sparkdream-sentry/config/genesis.json
```

2. Apply the sentry config templates via `envsubst`:

```bash
source deploy/config/network/mainnet/chain.env
envsubst < deploy/config/template/config.toml.sentry  > ~/.sparkdream-sentry/config/config.toml
envsubst < deploy/config/template/app.toml.sentry     > ~/.sparkdream-sentry/config/app.toml
envsubst < deploy/config/template/client.toml.sentry  > ~/.sparkdream-sentry/config/client.toml
```

3. Edit `akash/sentry.sdl.yaml`:
   - Set your `SSH_PUBLIC_KEY`
   - Set `HEADSCALE_URL`
   - Set `TS_AUTHKEY` to SENTRY_AUTHKEY
   - Set `WAIT_FOR_CONFIG=true`

4. Deploy on Akash (different provider than validator and Headscale)

5. Upload sentry data, SSH in, verify Tailscale:

```bash
tailscale status
tailscale ip -4
# Note sentry's Tailscale IP (e.g., 100.64.0.2)
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
source deploy/config/network/mainnet/chain.env
export SENTRY_NODE_ID="def456..."
export SENTRY_HOST="100.64.0.2"
export SENTRY_PORT="26656"
envsubst < deploy/config/template/config.toml.validator > /root/.sparkdream/config/config.toml
envsubst < deploy/config/template/app.toml.validator    > /root/.sparkdream/config/app.toml
envsubst < deploy/config/template/client.toml.validator > /root/.sparkdream/config/client.toml
```

**On the sentry**:

```bash
source deploy/config/network/mainnet/chain.env
export VALIDATOR_NODE_ID="abc123..."
export VALIDATOR_HOST="100.64.0.1"
export VALIDATOR_PORT="26656"
envsubst < deploy/config/template/config.toml.sentry > /root/.sparkdream/config/config.toml
envsubst < deploy/config/template/app.toml.sentry    > /root/.sparkdream/config/app.toml
envsubst < deploy/config/template/client.toml.sentry > /root/.sparkdream/config/client.toml
```

**On the validator** — bind TMKMS listener to Tailscale IP only:

```bash
sed -i 's|^priv_validator_laddr.*|priv_validator_laddr = "tcp://100.64.0.1:26659"|' \
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
source deploy/config/network/mainnet/chain.env
sparkdreamd init archive --chain-id "$CHAIN_ID" --home ~/.sparkdream
cp deploy/config/network/mainnet/genesis.json ~/.sparkdream/config/genesis.json

# Set pruning to nothing for full history
sed -i 's/^pruning *=.*/pruning = "nothing"/' ~/.sparkdream/config/app.toml

# Peer with validator over Tailscale
sed -i 's|^persistent_peers.*|persistent_peers = "abc123...@100.64.0.1:26656"|' \
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

### Updating the binary

1. Build new Docker image with updated sparkdreamd
2. Push to registry
3. Update image tag in SDL
4. Redeploy

### Rotating Tailscale keys

Pre-auth keys are only used for initial join. After that, the node
uses its stored state. To rotate, generate a new key on Headscale,
update the SDL env var, and redeploy.

### Monitoring

- Check node status: `curl http://<sentry>:<rpc_port>/status | jq .result.sync_info`
- Check mesh health: `tailscale status` from any node
- Check storage: `du -sh /root/.sparkdream/data/*/` via SSH

### Disaster Recovery

See [archival-strategy.md](archival-strategy.md) for full recovery
procedures using archived blocks from Arweave. The `replay-from-archive`
command can reconstruct the chain from any starting state plus
incremental block archives.