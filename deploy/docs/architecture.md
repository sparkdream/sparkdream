# SparkDream Network Architecture

This document describes the design rationale and architecture of a
SparkDream node deployment, covering the node topology, networking
model, data flow, and infrastructure choices.

## Design Principles

**Sovereignty**: No dependency on corporate cloud providers. All
infrastructure runs on decentralized platforms (Akash) or hardware
you physically control (home LAN). The coordination server
(Headscale) is self-hosted. Block history is permanently archived
on Arweave.

**Defense in depth**: Multiple layers protect the validator —
sentry architecture hides its IP, Tailscale mesh encrypts all
private communication, TMKMS keeps signing keys on local hardware,
and no sensitive ports are exposed publicly.

**Zero duplication archival**: Blocks are incremental state diffs.
Each archive file contains only new blocks. The full chain can be
reconstructed by replaying genesis + all archives in order. No
redundant snapshots.

**Graceful degradation**: If any single component fails, the rest
continue operating. Headscale downtime doesn't break existing mesh
connections. Sentry loss doesn't affect the validator (deploy a
replacement). Archive node loss doesn't affect consensus. Arweave
ensures permanent data availability regardless of node status.

## Node Topology

```
┌─────────────────────────────────────────────────────────────────┐
│                     Tailscale Mesh (WireGuard)                  │
│                                                                 │
│  ┌──────────────┐  ┌──────────────┐  ┌────────────────────────┐ │
│  │  Validator   │  │  Sentry      │  │  Home LAN              │ │
│  │              │  │              │  │                        │ │
│  │  Consensus   │  │  Public      │  │  ┌───────────┐         │ │
│  │  engine      │◄─┤  P2P + RPC   │  │  │  TMKMS    │         │ │
│  │              │  │  gateway     │  │  │  (signer) ├────►    │ │
│  │  No public   │  │              │  │  └───────────┘ │       │ │
│  │  ports       │  │  Serves      │  │                │       │ │
│  │              │  │  external    │  │  ┌──────────┐  │       │ │
│  │  Akash       │  │  validators  │  │  │ Archive  │◄─┘       │ │
│  │  Provider A  │  │  and users   │  │  │ node     │          │ │
│  │              │  │              │  │  │ (full    │          │ │
│  └──────────────┘  │  Akash       │  │  │ history) │          │ │
│                    │  Provider B  │  │  └──────────┘          │ │
│  ┌──────────────┐  │              │  │                        │ │
│  │  Headscale   │  └──────────────┘  │  No port forwarding    │ │
│  │              │                    │  NAT traversal via     │ │
│  │  Coordination│                    │  Tailscale             │ │
│  │  server      │                    │                        │ │
│  │              │                    └────────────────────────┘ │
│  │  Akash       │                                               │
│  │  Provider C  │                                               │
│  └──────────────┘                                               │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### Node Roles

**Validator** (Akash, Provider A)
- Participates in consensus, proposes and signs blocks
- No public ports — communicates exclusively over Tailscale
- Peers only with the sentry via Tailscale IP
- Receives signing requests from TMKMS via Tailscale
- `pex = false` — does not participate in peer exchange
- `pruning = "default"` — keeps recent state for fast queries
- Serves state sync snapshots for bootstrapping new nodes

**Sentry** (Akash, Provider B)
- Public-facing gateway to the SparkDream network
- Exposes P2P (26656) for other validators and full nodes
- Exposes RPC (26657) for users and light clients
- Peers with validator over Tailscale (private)
- Peers with the public network via P2P (public)
- `pex = true` — participates in peer exchange
- `private_peer_ids` contains the validator's node ID to prevent
  gossiping the validator's address
- `pruning = "everything"` — minimal state, keeps all blocks
- Runs the block archiver for incremental backups

**Headscale** (Akash, Provider C)
- Self-hosted Tailscale coordination server
- Manages key exchange and node registration for the mesh
- Lightweight — 0.5 CPU, 512MB RAM, 1GB persistent storage
- Deployed on a separate provider for redundancy
- If it goes down, existing connections persist; only new
  node joins are affected

**TMKMS** (Home LAN)
- Tendermint Key Management System
- Holds the validator consensus key on hardware you control
- Connects to the validator's `priv_validator_laddr` over Tailscale
- Supports PKCS11, YubiHSM or encrypted softsign
- No port forwarding — Tailscale handles NAT traversal

**Archive Node** (Home LAN)
- Full history node with `pruning = "nothing"`
- Stores complete block and state history
- Accessible to the sentry via Tailscale for proxying
  historical queries
- Runs on your own hardware — much cheaper than Akash for
  storage-heavy workloads
- Syncs from the validator over Tailscale P2P

## Network Flows

### Consensus Flow

```
1. Validator receives proposed block from sentry (Tailscale P2P)
2. Validator sends signing request to TMKMS (Tailscale, port 26659)
3. TMKMS signs the precommit and returns it
4. Validator broadcasts signed precommit to sentry (Tailscale P2P)
5. Sentry relays to the public network (public P2P)
```

### User Query Flow

```
1. User sends RPC query to sentry (public, port 26657)
2. Sentry serves from local state if available
3. For historical queries beyond sentry's pruned state:
   sentry proxies to archive node (Tailscale)
4. Response returned to user
```

### Block Archival Flow

```
1. Block archiver runs on sentry, fetches new blocks via localhost RPC
2. Archives saved as gzipped JSONL on sentry's persistent storage
3. Operator downloads archives via SSH/SCP
4. Operator uploads to Arweave (permanent) and/or Storacha (IPFS)
5. Manifest tracks block ranges → Arweave TX IDs
```

### State Sync Flow

```
1. New node starts with statesync enabled in config.toml
2. Connects to sentry's RPC (or validator's RPC over Tailscale)
3. Discovers available snapshots
4. Downloads and restores snapshot
5. Continues with normal block sync from snapshot height
```

## Provider Isolation

Each Akash deployment runs on a different provider to avoid
correlated failures:

| Component | Provider | Rationale |
|-----------|----------|-----------|
| Validator | Provider A | Core consensus — isolated from other components |
| Sentry | Provider B | Public-facing — can be DDoS'd without affecting validator |
| Headscale | Provider C | Coordination — independent of both chain nodes |

If Provider B goes down (sentry lost), deploy a replacement sentry
on Provider D. The validator continues operating on Provider A.

If Provider A goes down (validator lost), deploy a replacement on
any available provider, state sync from the sentry, reconnect TMKMS.

If Provider C goes down (Headscale lost), existing mesh connections
persist. Deploy Headscale on a new provider and update node configs
when convenient.

## Data Persistence

### On Akash (persistent storage)

Each Akash deployment uses persistent volumes that survive container
restarts and redeployments on the same provider:

- **Validator**: `/root/.sparkdream` — chain data, config, Tailscale state
- **Sentry**: `/root/.sparkdream` — chain data, config, archives, Tailscale state
- **Headscale**: `/var/lib/headscale` — SQLite database, encryption keys;
  `/etc/headscale` — configuration

Closing a lease and redeploying on a different provider loses the
persistent volume. Always back up critical data before migrating.

### On Home LAN

- **TMKMS**: Consensus key, TMKMS config
- **Archive node**: Full chain data (`pruning = "nothing"`)

### On Arweave (permanent)

- Genesis file
- Incremental block archives (JSONL, gzipped)
- Archive manifest (block ranges → TX IDs)

## Scaling Considerations

### Adding more sentries

Deploy additional sentry nodes on different providers. Each sentry
peers with the validator over Tailscale and serves public P2P and
RPC. This provides geographic distribution and DDoS resilience.

Update the validator's `persistent_peers` to include all sentry
Tailscale IPs, and add the validator's node ID to each sentry's
`private_peer_ids`.

### Adding seed nodes

For public network bootstrapping, deploy seed nodes with
`seed_mode = true` in `config.toml`. Seeds help new nodes discover
peers but don't participate in consensus.

### Chain upgrades

1. Build new Docker image with updated `sparkdreamd` binary
2. Push to registry
3. Update image tag in all SDLs
4. Coordinate upgrade height with other validators
5. Set `halt-height` in `app.toml` to the upgrade height
6. When the chain halts, redeploy with the new image
7. Nodes resume from the halt height with new binary

## Technology Choices

| Component | Technology | Why |
|-----------|-----------|-----|
| Hosting | Akash Network | Decentralized, sovereignty-aligned, cost-effective |
| Container OS | Alpine Linux | Minimal attack surface, small image size |
| Mesh VPN | Tailscale (client) + Headscale (server) | Userspace WireGuard works on Akash without TUN/NET_ADMIN; Headscale is self-hosted |
| Signing | TMKMS | Industry standard for Cosmos validator key management |
| Block archival | Arweave | Permanent, pay-once, immutable storage |
| Redundant storage | Storacha/IPFS | Fast retrieval via IPFS gateways, free tier |
| Block replay | Custom `replay-from-archive` | Enables full chain reconstruction from incremental archives |
