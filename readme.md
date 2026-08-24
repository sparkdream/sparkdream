# Spark Dream

> A Cosmos SDK appchain for **commons-based coordination** — where reputation is earned, governance is layered and accountable, and contributions are paid in a productivity-backed internal token.

Spark Dream is an experiment in moving past pure token-voting. It combines:

- **Three Pillars Governance** — specialised councils with guaranteed funding, held accountable by prediction markets, instead of one undifferentiated DAO.
- **Dual tokens** — `SPARK` (native, tradable, validator economy) and `DREAM` (internal, earned, productivity-backed; not externally tradable).
- **Reputation as the coordination substrate** — per-tag scores, seasonal resets, lifetime archive, and a trust tree of staked invitations.
- **Privacy where it matters** — a unified shielded-execution layer (ZK proofs + threshold timelock encryption) for anonymous voting, posting, and reporting.
- **No oracles** — every verification depends on internal actors and onchain mechanisms: stakers, jurors, council vote, futarchy markets, conviction thresholds.
- **Accountability for the position, not the person** — reputation/balance reset if warranted, never a permanent address exclusion.

The full design is laid out in [docs/architecture.md](docs/architecture.md) and the per-module specs under [docs/](docs/).

---

## The Three Pillars

Authority is partitioned across three councils, each with a different mandate, decision policy, and treasury share:

| Pillar | Mandate | Pillar share | Council / Ops split |
|---|---|---|---|
| **Commons Council** | Identity, membership, social policy, name registry | 50% | 47.5% / 2.5% |
| **Technical Council** | Protocol upgrades, infrastructure, code reveals | 30% | 28.5% / 1.5% |
| **Ecosystem Council** | Partnerships, grants, external integrations | 20% | 19% / 1% |

Within each pillar, 5% of the share is routed continuously to the pillar's Operations Committee so day-to-day spending has its own budget without a council proposal cycle.

Each council is a native `x/commons` Group whose `AllowedMessages` set bounds what it can execute on chain. Funding is automated: 15% of all chain revenue flows to the community pool, and `x/split` redistributes it to council treasuries every block. Tenure is **elastic** — `x/futarchy` runs confidence markets per council; high confidence extends terms, low confidence cuts them short.

---

## Tokens

### SPARK — native, validator-secured

```
Total supply        100,000,000 SPARK
Inflation           2% – 5% annual    (immutable; only chain upgrades can change it)
Validators          85% of inflation + fees
Community Pool      15% → x/split → councils
Genesis             5% to founders, 95% to community pool
```

SPARK secures consensus, pays gas, and funds councils. The mint module's parameter authority is `x/guardian`, not `x/gov`, and guardian's filter for `mint.MsgUpdateParams` rejects any change to `inflation_min`, `inflation_max`, `goal_bonded`, or `inflation_rate_change` — a passing gov proposal cannot inflate the supply. `blocks_per_year` stays tunable. See [docs/security-hardening.md](docs/security-hardening.md).

### DREAM — earned, productivity-backed

DREAM is not bought; it is **minted on completed work** (initiative completion, seasonal staking pool, committee compensation, retroactive public-goods funding) and **burned on failure** (slashing, failed challenges/invitations, idle decay, transfer tax). Transfer is restricted: tips capped, gifts only to invitees, no IBC, no external trading. DREAM tracks contribution; it is not a speculation vehicle.

Full economics: [docs/tokenomics.md](docs/tokenomics.md).

---

## Module Map

17 custom modules layered by responsibility, plus the external `x/gnovm`. Specs live under `docs/x-<module>-spec.md`.

```
GOVERNANCE      x/gov            x/commons         x/futarchy          x/guardian
                params           Three Pillars     confidence markets  param authority proxy

ECONOMIC        x/distribution   x/split           x/ecosystem
                15% rev tax      council split     ecosystem treasury

COORDINATION    x/rep            x/season          x/reveal
                members,DREAM    seasonal reset    progressive open-source
                reputation,tags  XP, retro PGF     tranched contributor payouts
                invitations,
                bonded roles,
                challenges

CONTENT         x/blog           x/forum           x/collect
                posts            threads,sentinels curated collections

IDENTITY        x/name           x/identity        x/federation        x/service
                handle registry  chain denoms      IBC + ActivityPub   SPARK-staked
                                 and tickers       /AT bridges         operator bonds

PRIVACY/UTIL    x/shield         x/session         x/gnovm
                ZK + TLE,        session keys +    Gno smart contracts
                shielded exec    fee delegation    (external module)
```

A few non-obvious choices worth flagging:

- **`x/rep` is the coordination hub.** Tag registry, bonded-role accountability (forum sentinels, collect curators, federation verifiers), member reports, and the jury system all live here so that other modules don't reinvent them. See [docs/bonded-role-generalization.md](docs/bonded-role-generalization.md).
- **`x/shield` replaces what would have been `x/vote`.** All ZK proof verification, the DKG ceremony, master public key, Shamir shares, and per-domain nullifiers are unified behind a single `MsgShieldedExec`. The shield module account pays gas, so anonymous submitters need zero balance. See [docs/x-shield-spec.md](docs/x-shield-spec.md).
- **`x/session` replaces `x/authz` + `x/feegrant`.** Purpose-built session keys with scoped message-type delegation, integrated fee delegation, hardcoded anti-recursion. Avoids licensing constraints and the recursion attack surface of authz. See [docs/x-session-spec.md](docs/x-session-spec.md).
- **`x/federation` is sovereignty-first.** Bilateral peer relationships only — no cross-chain tokens, no binding reputation, no supergovernment. Reputation crossing a peer boundary is heavily discounted (50%, capped at PROVISIONAL, 30-day TTL). See [docs/x-federation-spec.md](docs/x-federation-spec.md).
- **`x/guardian` owns the authority address for sensitive SDK modules.** Gov cannot call `mint`/`staking`/`distribution` `MsgUpdateParams` directly; it must route through `MsgExec`, which runs each inner message through a per-type field filter that rejects forbidden fields and clamps tunable ones. This is what makes the immutable-inflation promise enforceable rather than aspirational. See [x/guardian/README.md](x/guardian/README.md).
- **`x/identity` fixes each chain's denoms at genesis.** `bond_denom`, `dream_denom`, display symbols and decimals are written once at `InitGenesis` and sealed — there is no mutation message. Federated chains therefore get distinct tickers and denoms without forking the source tree. See [docs/x-identity-spec.md](docs/x-identity-spec.md).
- **`x/service` is the generic SPARK-staked accountability primitive.** Off-chain agents doing work the chain cannot verify (federation bridge operators today) register as Operators keyed by `(address, service_type)`, put up a SPARK bond, and answer to a controller Group. New service types need a gov-managed config entry, not a proto bump. See [docs/x-service-spec.md](docs/x-service-spec.md).

---

## Status

Active development. Mainnet has not launched.

| Network | Chain ID | Status |
|---|---|---|
| Devnet  | `sparkdream-dev-1`  | Live |
| Testnet | `sparkdream-test-1` | Live |
| Mainnet | `sparkdream-1`      | Pre-launch |

### Devnet endpoints

The devnet is the current integration target — newest code, loosest guarantees. It is wiped and re-genesised whenever a breaking change lands, so treat every address, balance, and object ID on it as disposable.

| Service | URL |
|---|---|
| RPC       | https://rpc-dev.sparkdream.io |
| REST API  | https://api-dev.sparkdream.io |
| Explorer  | https://explorer-devnet.sparkdream.io |
| Frontend  | https://app-dev.sparkdream.io |

Devnet runs its own `x/identity` denoms rather than the generic ones — `uspark.sparkdreamdev` for SPARK and `udream.sparkdreamdev` for DREAM, both 6 decimals (see [deploy/config/network/devnet/chain.env](deploy/config/network/devnet/chain.env)). Point the CLI at it with `--node https://rpc-dev.sparkdream.io:443 --chain-id sparkdream-dev-1`.

Two agent-driven simulations exercise the devnet end to end and double as worked examples of the coordination flows:

- [docs/devnet-team-simulation-agent-guide.md](docs/devnet-team-simulation-agent-guide.md) — four accounts proposing initiatives, discussing them in the forum, and staking conviction on each other's work ([sim/devnet/rep/](sim/devnet/rep/))
- [docs/devnet-governance-simulation-agent-guide.md](docs/devnet-governance-simulation-agent-guide.md) — council proposal and voting flows ([sim/devnet/gov/](sim/devnet/gov/))

### Testnet endpoints

| Service | URL |
|---|---|
| RPC       | https://rpc-test.sparkdream.io |
| REST API  | https://api-test.sparkdream.io |
| Explorer  | https://explorer-testnet.sparkdream.io |
| Frontend  | https://app-test.sparkdream.io |

APIs, genesis, and module parameters may change without notice until mainnet. Validator and sentry deployment lives on Akash with archive nodes streaming blocks to Arweave; see [deploy/docs/DEPLOYMENT.md](deploy/docs/DEPLOYMENT.md) and [provenance.md](provenance.md) for the archival record.

---

## Build & Run Locally

Requires Go 1.25+ and [Ignite CLI](https://docs.ignite.com/welcome/install).

```bash
# Build the binary (installs to $GOPATH/bin)
ignite chain build

# Initialise genesis from config.yml
ignite chain init

# Start the node
sparkdreamd start --home ~/.sparkdream
```

> Prefer this `build` + `init` + `start` flow over `ignite chain serve`. Serve uses an internal build pipeline that has produced depinject cycle panics on this codebase even when `chain build` succeeds; the rationale is captured in [docs/development-conventions.md](docs/development-conventions.md).

The default `config.yml` provisions four test accounts (`alice`, `bob`, `carol`, `dave`) with mnemonics committed intentionally — they derive the addresses hard-coded in `x/commons/keeper/genesis_vals.go` for governance bootstrap. **They hold no real funds and are never used in production.**

### Production-style local run

For a network closer to a real deployment (sentry architecture, mesh networking, archive node) see the deployment guide at [deploy/docs/DEPLOYMENT.md](deploy/docs/DEPLOYMENT.md).

---

## Testing

```bash
# Unit tests (vet + govulncheck + go test)
make test

# Race detector
make test-race

# Coverage report
make test-cover
```

Shell-based end-to-end tests live under [test/](test/), one directory per module. The parallel runner is the canonical way to execute them:

```bash
test/run_parallel.sh                # all suites in parallel with snapshot caching
test/run_all_tests.sh               # sequential
```

The runner builds the chain with the `testparams` build tag (shorter epochs, smaller thresholds), saves a per-suite post-setup snapshot, and isolates each suite's home directory so suites can run concurrently. IBC scenarios run in a multichain harness.

---

## Documentation

Start here:

- [docs/architecture.md](docs/architecture.md) — system overview and fund flows
- [docs/tokenomics.md](docs/tokenomics.md) — full SPARK and DREAM mechanics

Module specifications:

- [docs/x-commons-spec.md](docs/x-commons-spec.md) — Three Pillars, native proposals, decision policies
- [docs/x-rep-spec.md](docs/x-rep-spec.md) — reputation, DREAM, invitations, projects, initiatives, stakes, challenges, tags, bonded roles
- [docs/x-season-spec.md](docs/x-season-spec.md) — seasonal state machine, gamification, retroactive PGF
- [docs/x-reveal-spec.md](docs/x-reveal-spec.md) — progressive open-source with tranched contributor payouts
- [docs/x-shield-spec.md](docs/x-shield-spec.md) — ZK + TLE unified privacy layer
- [docs/x-session-spec.md](docs/x-session-spec.md) — session keys + fee delegation
- [docs/x-forum-spec.md](docs/x-forum-spec.md) · [docs/x-blog-spec.md](docs/x-blog-spec.md) · [docs/x-collect-spec.md](docs/x-collect-spec.md) — content modules
- [docs/x-name-spec.md](docs/x-name-spec.md) — handle registry
- [docs/x-identity-spec.md](docs/x-identity-spec.md) — genesis-sealed chain denoms and tickers
- [docs/x-federation-spec.md](docs/x-federation-spec.md) — IBC + ActivityPub/AT Protocol bridges
- [docs/x-service-spec.md](docs/x-service-spec.md) — SPARK-staked operator bonds and slashing
- [docs/x-futarchy-spec.md](docs/x-futarchy-spec.md) — confidence markets
- [x/guardian/README.md](x/guardian/README.md) — param authority proxy and field filters

Operational:

- [deploy/docs/DEPLOYMENT.md](deploy/docs/DEPLOYMENT.md) — Akash + Headscale + Arweave deployment
- [deploy/docs/archival-strategy.md](deploy/docs/archival-strategy.md) — block archival to Arweave
- [deploy/docs/state-sync.md](deploy/docs/state-sync.md) — state sync setup
- [deploy/docs/replay-from-archive.md](deploy/docs/replay-from-archive.md) — restoring a node from archived blocks
- [docs/security-hardening.md](docs/security-hardening.md) — immutable parameters and what they protect
- [docs/development-conventions.md](docs/development-conventions.md) — proto, amino-name, and build conventions

---

## License

Apache License 2.0 — see [LICENSE](LICENSE).
