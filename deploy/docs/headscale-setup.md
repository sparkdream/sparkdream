# SparkDream Headscale Setup Guide
#
# This guide walks through deploying Headscale on Akash and
# connecting your validator, sentry, and local nodes to the mesh.

## Step 1: Deploy Headscale on Akash

Deploy using headscale-akash-sdl.yaml. After deployment, note:
- Provider address (e.g., provider.example.com)
- Forwarded port for 8080 (e.g., 31234)
- Forwarded port for 3478/UDP (for STUN)

## Step 2: Upload the config

Access the Akash shell and upload/create the config file:

```
cat > /etc/headscale/config.yaml << 'EOF'
<paste contents of headscale-config.yaml here>
EOF
```

Update `server_url` to match your actual deployment:
```
sed -i 's|CHANGE_ME:8080|provider.example.com:31234|' /etc/headscale/config.yaml
```

Then restart the deployment (close and redeploy, or kill the process).

## Step 3: Create a user and pre-auth keys

From the Akash shell:

```
# Create the sparkdream user
headscale users create sparkdream

# Create pre-auth keys for each node (reusable, long-lived)
# Validator
headscale preauthkeys create --user sparkdream --reusable --expiration 8760h
# (save this key as VALIDATOR_AUTH_KEY)

# Sentry
headscale preauthkeys create --user sparkdream --reusable --expiration 8760h
# (save this key as SENTRY_AUTH_KEY)

# TMKMS / Home LAN nodes
headscale preauthkeys create --user sparkdream --reusable --expiration 8760h
# (save this key as HOME_AUTH_KEY)
```

## Step 4: Connect the validator

SSH into the validator and install Tailscale:
```
apk add --no-cache tailscale
tailscaled --tun=userspace-networking --state=/var/lib/tailscale/tailscaled.state &
tailscale up --login-server=http://provider.example.com:31234 --authkey=VALIDATOR_AUTH_KEY --hostname=validator
```

Verify it joined:
```
tailscale status
```

## Step 5: Connect the sentry

Same process on the sentry:
```
apk add --no-cache tailscale
tailscaled --tun=userspace-networking --state=/var/lib/tailscale/tailscaled.state &
tailscale up --login-server=http://provider.example.com:31234 --authkey=SENTRY_AUTH_KEY --hostname=sentry
```

## Step 6: Connect home LAN nodes (TMKMS, archive node)

Install Tailscale on your home machine:
- Linux: https://tailscale.com/download/linux
- macOS: https://tailscale.com/download/mac

Then connect to your Headscale server:
```
sudo tailscale up --login-server=http://provider.example.com:31234 --authkey=HOME_AUTH_KEY --hostname=tmkms
```

No port forwarding needed on your router.

## Step 7: Verify the mesh

From any connected node:
```
tailscale status
```

You should see all nodes with their 100.64.x.x addresses.
Verify connectivity:
```
tailscale ping validator
tailscale ping sentry
```

## Step 8: Reconfigure services to use Tailscale IPs

On the validator, get its Tailscale IP:
```
tailscale ip -4
# e.g., 100.64.0.1
```

Update config.toml on the validator:
```
# Bind TMKMS listener to Tailscale IP only
priv_validator_laddr = "tcp://100.64.0.1:26659"

# Peer with sentry over Tailscale
persistent_peers = "<sentry_node_id>@<sentry_tailscale_ip>:26656"
```

Update config.toml on the sentry:
```
# Peer with validator over Tailscale
persistent_peers = "<validator_node_id>@<validator_tailscale_ip>:26656"
private_peer_ids = "<validator_node_id>"
```

Update TMKMS config to connect to validator's Tailscale IP:
```
[[validator]]
addr = "tcp://100.64.0.1:26659"
```

## Step 9: Remove public ports from validator SDL

Once everything communicates over Tailscale, update the
validator SDL to expose only SSH (if needed):
```yaml
expose:
  - port: 2222
    as: 2222
    proto: tcp
    to:
      - global: true
```

Or remove all public ports if SSH also goes through Tailscale.

## Step 10: Verify everything works

1. Validator should connect to sentry via Tailscale P2P
2. TMKMS should sign blocks via Tailscale tunnel
3. Sentry should serve public RPC/P2P on its Akash ports
4. Archive node at home should sync over Tailscale

## Notes

- Pre-auth keys expire. Generate long-lived ones (8760h = 1 year)
  or create new ones before expiry.
- If Headscale goes down temporarily, existing mesh connections
  persist. Only new node joins/re-keying are affected.
- The Headscale SQLite database is on persistent storage. Back it
  up periodically: cp /var/lib/headscale/db.sqlite <backup_path>
- Use `--tun=userspace-networking` on Akash containers since
  kernel TUN device is not available.
