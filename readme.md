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

| Pillar | Mandate | Treasury share |
|---|---|---|
| **Commons Council** | Identity, membership, social policy, name registry | 50% |
| **Technical Council** | Protocol upgrades, infrastructure, code reveals | 30% |
| **Ecosystem Council** | Partnerships, grants, external integrations | 20% |

Each council is a wrapped `x/group` whose `AllowedMessages` set bounds what it can execute on chain. Funding is automated: 15% of all chain revenue flows to the community pool, and `x/split` redistributes it to council treasuries every block. Tenure is **elastic** — `x/futarchy` runs confidence markets per council; high confidence extends terms, low confidence cuts them short.

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

SPARK secures consensus, pays gas, and funds councils. The mint module's parameter authority is set to a burn address, so `MsgUpdateParams` from `x/gov` cannot inflate the supply. See [docs/security-hardening.md](docs/security-hardening.md).

### DREAM — earned, productivity-backed

DREAM is not bought; it is **minted on completed work** (initiative completion, seasonal staking pool, committee compensation, retroactive public-goods funding) and **burned on failure** (slashing, failed challenges/invitations, idle decay, transfer tax). Transfer is restricted: tips capped, gifts only to invitees, no IBC, no external trading. DREAM tracks contribution; it is not a speculation vehicle.

Full economics: [docs/tokenomics.md](docs/tokenomics.md).

---

## Module Map

15 custom modules layered by responsibility. Specs live under `docs/x-<module>-spec.md`.

```
GOVERNANCE      x/gov            x/commons        x/futarchy
                params           Three Pillars     confidence markets

ECONOMIC        x/distribution   x/split          x/ecosystem
                15% rev tax      council split     ecosystem treasury

COORDINATION    x/rep            x/season         x/reveal
                members,DREAM    seasonal reset    progressive open-source
                reputation,tags  XP, retro PGF     tranched contributor payouts
                invitations,
                bonded roles,
                challenges

CONTENT         x/blog           x/forum          x/collect
                posts            threads,sentinels curated collections

IDENTITY        x/name           x/federation
                handle registry  IBC + ActivityPub/AT bridges

PRIVACY/UTIL    x/shield         x/session
                ZK + TLE,        session keys +
                shielded exec    fee delegation
```

A few non-obvious choices worth flagging:

- **`x/rep` is the coordination hub.** Tag registry, bonded-role accountability (forum sentinels, collect curators, federation verifiers), member reports, and the jury system all live here so that other modules don't reinvent them. See [docs/bonded-role-generalization.md](docs/bonded-role-generalization.md).
- **`x/shield` replaces what would have been `x/vote`.** All ZK proof verification, the DKG ceremony, master public key, Shamir shares, and per-domain nullifiers are unified behind a single `MsgShieldedExec`. The shield module account pays gas, so anonymous submitters need zero balance. See [docs/x-shield-spec.md](docs/x-shield-spec.md).
- **`x/session` replaces `x/authz` + `x/feegrant`.** Purpose-built session keys with scoped message-type delegation, integrated fee delegation, hardcoded anti-recursion. Avoids licensing constraints and the recursion attack surface of authz. See [docs/x-session-spec.md](docs/x-session-spec.md).
- **`x/federation` is sovereignty-first.** Bilateral peer relationships only — no cross-chain tokens, no binding reputation, no supergovernment. Reputation crossing a peer boundary is heavily discounted (50%, capped at PROVISIONAL, 30-day TTL). See [docs/x-federation-spec.md](docs/x-federation-spec.md).

---

## Status

Active development. Mainnet has not launched.

| Network | Chain ID | Status |
|---|---|---|
| Devnet  | `sparkdream-dev-1`  | Live |
| Testnet | `sparkdream-test-1` | Live |
| Mainnet | `sparkdream-1`      | Pre-launch |

### Testnet endpoints

| Service | URL |
|---|---|
| RPC       | https://rpc-test.sparkdream.io |
| REST API  | https://api-test.sparkdream.io |
| Explorer  | https://explorer-test.sparkdream.io |
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

> Prefer this `build` + `init` + `start` flow over `ignite chain serve`. Serve uses an internal build pipeline that has produced depinject cycle panics on this codebase even when `chain build` succeeds; the rationale is captured in [CLAUDE.md](CLAUDE.md).

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
- [docs/x-name-spec.md](docs/x-name-spec.md) — identity registry
- [docs/x-federation-spec.md](docs/x-federation-spec.md) — IBC + ActivityPub/AT Protocol bridges
- [docs/x-futarchy-spec.md](docs/x-futarchy-spec.md) — confidence markets

Operational:

- [deploy/docs/DEPLOYMENT.md](deploy/docs/DEPLOYMENT.md) — Akash + Headscale + Arweave deployment
- [deploy/docs/archival-strategy.md](deploy/docs/archival-strategy.md) — block archival to Arweave
- [deploy/docs/state-sync.md](deploy/docs/state-sync.md) — state sync setup
- [docs/security-hardening.md](docs/security-hardening.md) — immutable parameters and what they protect

---

## License

Apache License 2.0 — see [LICENSE](LICENSE).
