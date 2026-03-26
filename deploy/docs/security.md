# Security Model and Key Management

This document describes the security architecture for SparkDream node
deployments, covering the threat model, key management practices,
network security, and operational guidelines.

## Threat Model

### What we protect against

- **Validator compromise**: An attacker gaining control of the
  validator's signing key could double-sign, causing slashing
- **Network exposure**: The validator's IP being discoverable
  enables targeted DDoS attacks
- **Key theft**: Private keys stored on remote infrastructure
  (Akash) are at risk from malicious or compromised providers
- **Man-in-the-middle**: Unencrypted communication between nodes
  could be intercepted or tampered with
- **Infrastructure dependency**: Reliance on a single provider,
  service, or hosting platform creates single points of failure

### What we accept as residual risk

- **Akash provider access**: Providers have theoretical access to
  container memory and filesystem. We mitigate this by keeping
  signing keys off Akash entirely
- **Headscale availability**: If the Headscale coordination server
  goes down, new mesh connections cannot be established (existing
  connections persist). Mitigated by deploying Headscale on a
  separate provider
- **Tailscale userspace networking**: Slightly lower performance
  than kernel WireGuard, but necessary for Akash compatibility

## Key Inventory

A SparkDream deployment involves several distinct keys, each with
different security requirements:

### Consensus Key (highest sensitivity)

- **What**: Ed25519 key used to sign blocks and precommits
- **Where**: TMKMS on your home LAN — never on Akash
- **Managed by**: TMKMS (supports PKCS11, YubiHSM, or softsign)
- **Backup**: Encrypted offline backup in a secure physical location
- **Rotation**: Requires a validator update transaction
- **Compromise impact**: Double-signing → slashing and jailing

### Operator Account Key (high sensitivity)

- **What**: Secp256k1 key for governance votes, commission changes,
  rewards withdrawal, and other validator operations
- **Where**: Your local machine, managed by the OS keychain
  (`--keyring-backend os`) or `pass` (`--keyring-backend pass`)
- **Never on Akash**: Sign transactions locally, broadcast via
  the sentry's public RPC
- **Backup**: Mnemonic phrase stored offline in a secure location
- **Compromise impact**: Unauthorized transactions from your
  validator operator account

### Node Keys (moderate sensitivity)

- **What**: Ed25519 keys for P2P authentication between nodes
- **Where**: `config/node_key.json` on each node
- **On Akash**: Yes, these must be on the running nodes
- **Risk**: A compromised node key allows impersonation of that
  specific node in the P2P network. Limited blast radius since
  consensus signing is separate
- **Rotation**: Generate a new key, update persistent peer configs

### SSH Keys (moderate sensitivity)

- **What**: Ed25519 keys for SSH access to Akash containers
- **Where**: Public key in SDL env var, private key on your machine
- **Rotation**: Update the SDL with a new public key and redeploy

### Tailscale Pre-Auth Keys (low sensitivity, time-limited)

- **What**: One-time or reusable keys for joining the Headscale mesh
- **Where**: SDL env vars (for initial join only)
- **Lifecycle**: Only needed for the first connection. After the node
  joins, Tailscale state is persisted on disk and the key is no
  longer required
- **Best practice**: Use short-lived keys where possible. For Akash
  deployments where you need reusable keys (container restarts),
  set a reasonable expiration (e.g., 1 year) and rotate before expiry

### Arweave Wallet Key (moderate sensitivity)

- **What**: RSA key for signing Arweave upload transactions
- **Where**: Your local machine only — never on Akash
- **Compromise impact**: Unauthorized uploads charged to your AR balance

## Network Security Architecture

```
Public Internet
      │
      │  Only these ports are exposed:
      │  ┌──────────────────┐
      │  │ Sentry           │
      │  │  26656 (P2P)     │◄── Other validators, full nodes
      │  │  26657 (RPC)     │◄── Users, light clients
      │  │  2222  (SSH)     │◄── Operator (can be removed)
      │  └────────┬─────────┘
      │           │
      │    Tailscale mesh (WireGuard encrypted)
      │           │
      │  ┌────────▼──────────┐
      │  │ Validator         │
      │  │  No public ports  │
      │  │  26656 via TS     │◄── Sentry only
      │  │  26659 via TS     │◄── TMKMS only
      │  └───────────────────┘
      │
      │  ┌───────────────────┐
      │  │ Headscale         │
      │  │  8080  (API)      │◄── Tailscale clients
      │  │  3478  (STUN)     │◄── NAT traversal
      │  └───────────────────┘
      │
  Home LAN (NAT, no port forwarding)
      │
      │  ┌───────────────────┐
      │  │ TMKMS             │── Connects to validator:26659 via TS
      │  │ Archive Node      │── Connects to validator:26656 via TS
      │  └───────────────────┘
```

### Key security properties

- **Validator is invisible**: No public ports, no discoverable IP.
  All communication over Tailscale mesh
- **Sentry absorbs attacks**: DDoS targets the sentry, not the
  validator. Sentry is replaceable — deploy a new one on a
  different provider
- **TMKMS is air-gapped from the internet**: Runs on home LAN
  behind NAT. Connects to validator through Tailscale tunnel.
  No port forwarding required
- **End-to-end encryption**: All inter-node traffic is encrypted
  by WireGuard (via Tailscale). Even if Akash providers inspect
  network traffic, they see only encrypted packets
- **Provider isolation**: Validator, sentry, and Headscale run on
  different Akash providers. Compromise of one provider doesn't
  affect the others

## Operational Security Guidelines

### Do

- Keep consensus keys on TMKMS hardware you physically control
- Sign operational transactions locally, broadcast via sentry RPC
- Use `--keyring-backend os` or `--keyring-backend pass` on your
  local machine
- Deploy validator, sentry, and Headscale on different providers
- Monitor Headscale node list for unauthorized devices
- Rotate Tailscale pre-auth keys before expiration
- Back up Headscale's SQLite database periodically
- Use the block archiver to maintain off-chain backups on Arweave

### Don't

- Never store consensus keys or operator account keys on Akash
- Never put private keys, mnemonics, or passwords in `.env` files
- Never commit real keys, auth tokens, or provider addresses to Git
- Never use `--keyring-backend test` for mainnet accounts
- Never expose the validator's `priv_validator_laddr` (26659) to
  the public internet
- Never run validator and sentry on the same Akash provider
- Never leave `WAIT_FOR_CONFIG=true` in production

### Secrets management

For operational scripts that need secrets (Arweave wallet, pre-auth
keys), use `pass` (the standard Unix password manager) to store and
retrieve them:

```bash
# Store a secret
pass insert sparkdream/headscale/validator-authkey

# Retrieve in a script
AUTHKEY=$(pass sparkdream/headscale/validator-authkey)
tailscale up --login-server=... --authkey="$AUTHKEY"
```

This keeps secrets encrypted at rest with your GPG key and out of
plaintext files, environment variables, and shell history.

## Incident Response

### Suspected key compromise

1. **Consensus key**: Immediately unjail and rotate the key via
   TMKMS. If double-signing occurred, assess slashing damage.
   Generate a new consensus key on TMKMS, submit a validator
   update transaction
2. **Operator account**: Transfer funds to a new account immediately.
   Create a new operator key and update the validator's operator
   address if possible
3. **Node key**: Generate a new `node_key.json`, update all peer
   configs, redeploy
4. **SSH key**: Update the SDL with a new public key, redeploy
5. **Tailscale pre-auth key**: Revoke on Headscale
   (`headscale preauthkeys expire`), generate new keys, redeploy
   affected nodes

### Suspected provider compromise

1. Deploy replacement nodes on different providers immediately
2. Rotate all node keys and SSH keys
3. Verify TMKMS was not affected (it runs on your hardware)
4. Check Headscale node list for unauthorized devices
5. Review block signing history for any anomalies

### Headscale goes down

1. Existing mesh connections persist — no immediate impact
2. No new nodes can join the mesh until Headscale is restored
3. Redeploy Headscale on a new provider if the current one is
   unrecoverable
4. Nodes will need to be reconfigured with the new Headscale URL
   if the address changes

## Audit Checklist

Periodically verify:

- [ ] TMKMS is signing blocks correctly (check validator uptime)
- [ ] No unauthorized nodes in Headscale: `headscale nodes list`
- [ ] Tailscale pre-auth keys haven't expired
- [ ] Validator has no public ports exposed (check SDL and lease)
- [ ] Sentry's `private_peer_ids` includes the validator's node ID
- [ ] Validator's `pex = false`
- [ ] Block archives are being uploaded to Arweave on schedule
- [ ] Headscale database is backed up
- [ ] All container images use pinned versions (no `latest` tags)
