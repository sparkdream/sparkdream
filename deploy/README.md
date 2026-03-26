# SparkDream Deploy

Deployment toolkit for running SparkDream blockchain nodes on decentralized infrastructure.

This directory contains everything needed to deploy and operate validator nodes, sentry nodes, and supporting infrastructure on [Akash Network](https://akash.network), with optional private mesh networking via [Headscale](https://headscale.net) + [Tailscale](https://tailscale.com), and permanent block archival to [Arweave](https://arweave.org).

## Architecture

```
                    ┌─────────────────┐
                    │   Headscale     │
                    │  (Akash, own    │
                    │   provider)     │
                    └────────┬────────┘
                             │ Tailscale mesh
              ┌──────────────┼──────────────┐
              │              │              │
    ┌─────────▼───┐  ┌───────▼────┐  ┌──────▼───────┐
    │ Validator   │  │  Sentry    │  │  Home LAN    │
    │ (Akash)     │◄─┤  (Akash)   │  │              │
    │             │  │            │  │  • TMKMS     │
    │ No public   │  │ Public:    │  │  • Archive   │
    │ ports       │  │  P2P 26656 │  │    node      │
    │             │  │  RPC 26657 │  │              │
    └─────────────┘  └──────┬─────┘  └──────────────┘
                            │
                     Public Internet
                     (other validators,
                      full nodes, users)
```

The validator is fully hidden behind the Tailscale mesh. The sentry is the only public-facing node. TMKMS and the archive node run on your home LAN with no port forwarding — Tailscale handles NAT traversal.

## Quick Start

```bash
# 1. Build the Docker images
make docker-build
make docker-build-ssh

# 2. Deploy Headscale on Akash
#    See docs/headscale-setup.md

# 3. Deploy the validator
#    Edit akash/validator.sdl.yaml with your keys and Headscale URL
#    Deploy via Akash console or CLI

# 4. Deploy the sentry
#    Edit akash/sentry.sdl.yaml
#    Deploy on a DIFFERENT Akash provider than the validator

# 5. Connect home LAN nodes
#    Install Tailscale, join the Headscale network
#    See docs/headscale-setup.md
```

## Directory Structure

```
deploy/
├── docker/                                  Docker images
│   ├── Dockerfile-sparkdreamd-alpine        Base sparkdreamd Alpine image
│   ├── Dockerfile-sparkdreamd-alpine-ssh    SSH + Tailscale enabled image
│   └── entrypoint_ssh.sh                    SSH + Tailscale container entrypoint script
│
├── akash/                      Akash SDL deployment files
│   ├── validator.sdl.yaml      Validator (no public ports)
│   ├── sentry.sdl.yaml         Sentry (public P2P + RPC)
│   └── headscale.sdl.yaml      Headscale coordination server
│
├── config/
│   ├── template/               Role-based config templates (use envsubst)
│   │   ├── config.toml.validator
│   │   ├── config.toml.sentry
│   │   ├── app.toml.validator
│   │   ├── app.toml.sentry
│   │   ├── client.toml.validator
│   │   └── client.toml.sentry
│   └── network/                Per-network chain parameters
│       ├── mainnet/chain.env
│       ├── testnet/chain.env
│       └── devnet/chain.env
│
├── mesh/                       Private networking
│   └── headscale-config.yaml   Headscale server configuration
│
├── scripts/                    Operational scripts
│   ├── block-archiver.sh       Incremental block archival via RPC
│   ├── storacha-upload.sh      Upload archives to Storacha/IPFS
│   └── arweave-upload.sh       Upload archives to Arweave
│
└── docs/                       Guides and documentation
    ├── DEPLOYMENT.md            Full deployment walkthrough
    ├── architecture.md          Network architecture overview
    ├── headscale-setup.md       Mesh VPN setup guide
    ├── archival-strategy.md     Block archival and recovery
    └── security.md              Security model and key management
```

## Config Templates

Template files in `config/template/` contain `${VAR}` placeholders that are resolved using variables from `config/network/<network>/chain.env`. To generate concrete config files, source the env file and run `envsubst`:

```bash
# Source network-specific variables
source deploy/config/network/mainnet/chain.env

# Generate configs for a validator
envsubst < deploy/config/template/config.toml.validator > ~/.sparkdream/config/config.toml
envsubst < deploy/config/template/app.toml.validator    > ~/.sparkdream/config/app.toml
envsubst < deploy/config/template/client.toml.validator > ~/.sparkdream/config/client.toml
```

### chain.env Variables

| Variable | Example | Used in |
|---|---|---|
| `CHAIN_ID` | `sparkdream-1` | client.toml |
| `MIN_GAS_PRICES` | `25000uspark` | app.toml |
| `SNAPSHOT_INTERVAL` | `1000` | app.toml |
| `SNAPSHOT_KEEP_RECENT` | `2` | app.toml |
| `VALIDATOR_NODE_ID` | *(node ID hex)* | config.toml.sentry |
| `VALIDATOR_HOST` | `100.64.0.1` | config.toml.sentry |
| `VALIDATOR_PORT` | `26656` | config.toml.sentry |
| `SENTRY_NODE_ID` | *(node ID hex)* | config.toml.validator |
| `SENTRY_HOST` | `100.64.0.2` | config.toml.validator |
| `SENTRY_PORT` | `26656` | config.toml.validator |

Peer variables are empty by default in `chain.env` and should be set per-deployment after nodes are initialized (see [DEPLOYMENT.md](docs/DEPLOYMENT.md) Phase 6).

## Key Features

**Decentralized infrastructure**: Runs entirely on Akash Network — no corporate cloud dependency.

**Private mesh networking**: Validator communicates exclusively over an encrypted Tailscale mesh coordinated by a self-hosted Headscale server. No sensitive ports exposed to the public internet.

**Sentry architecture**: The validator is hidden behind a sentry node that handles all public P2P and RPC traffic. The validator's IP and existence are invisible to the network.

**Permanent block archival**: Incremental block archives are uploaded to Arweave for permanent storage. A custom `replay-from-archive` command can reconstruct the full chain state from these archives without relying on the network.

**Home LAN integration**: TMKMS signing and archive nodes run on your own hardware with no port forwarding. Tailscale handles NAT traversal automatically.

## Environment Variables

The Docker image is configured via environment variables in the Akash SDL:

| Variable | Required | Description |
|---|---|---|
| `SSH_PUBLIC_KEY` | Yes | Ed25519 public key for SSH access |
| `HEADSCALE_URL` | No | Headscale server URL (enables Tailscale) |
| `TS_AUTHKEY` | No | Tailscale pre-auth key (required if HEADSCALE_URL set) |
| `TS_HOSTNAME` | No | Node hostname on the mesh (default: sparkdream-node) |
| `TS_STATE_DIR` | No | Tailscale state directory (default: /var/lib/tailscale) |
| `WAIT_FOR_CONFIG` | No | Set to "true" to keep container alive for initial setup |

## Documentation

Start with [DEPLOYMENT.md](docs/DEPLOYMENT.md) for the full walkthrough. See [architecture.md](docs/architecture.md) for the design rationale. See [security.md](docs/security.md) for key management and threat model.

## Security Notes

- Never commit real keys, pre-auth tokens, or provider addresses to this repo
- All config templates use placeholder values — replace them for your deployment
- Validator consensus keys should be managed by TMKMS on hardware you control
- Operational transaction signing should be done locally, not on Akash containers
- See [security.md](docs/security.md) for the full security model