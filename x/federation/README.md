# x/federation

Cross-chain content exchange, reputation bridging, and identity linking for federated Spark Dream chains (via IBC) and external social protocols (ActivityPub, AT Protocol) via off-chain bridges.

Full spec: [docs/x-federation-spec.md](../../docs/x-federation-spec.md). Service-migration plan: [docs/x-federation-service-migration-plan.md](../../docs/x-federation-service-migration-plan.md).

## Sovereignty Principles

- **Bilateral relationships only.** No supergovernment; each peer pair is negotiated independently.
- **No cross-chain tokens.** SPARK and DREAM never cross a federation boundary.
- **No binding reputation.** Bridged reputation is attested, heavily discounted (50%, capped at PROVISIONAL equivalent, 30-day TTL).
- **Unilateral suspend/remove.** Either side can pause or terminate the relationship at any time.

## Three Layers

1. **On-chain primitives** — peer registry, peer policies, identity links, federated content records.
2. **IBC protocol** — chain-to-chain, trustless. Port `federation`, version `federation-1`, UNORDERED.
3. **Off-chain bridges** — ActivityPub / AT Protocol relayers. SPARK-staked operators owned by x/service.

## Peers

A `Peer` is a remote network (another Spark Dream chain, an ActivityPub server cluster, etc.). Each peer carries:

- `peer_id` — opaque identifier (typically `chain://<chain-id>` or `protocol://<host>`)
- `protocol` — `IBC` / `ACTIVITYPUB` / `ATPROTO`
- `controller_group` — optional x/commons Group address that resolves tier-1 reports against this peer's bridge operators. Empty → defaults to Operations Committee at `MsgRegisterBridge` time and the resolved address is captured on the resulting `service.Operator`.
- `policy` — content type allowlists, inbound/outbound rate limits, moderation rules
- `status` — ACTIVE / SUSPENDED

Commons Council registers/removes peers. Operations Committee manages policies.

## Bridge Operators (Federation-Owned Binding, x/service-Owned Economics)

Bridge operators are **x/service Operators** keyed by `(address, service_type)` where `service_type` is `federation-bridge-activitypub` or `federation-bridge-atproto`. The bond, status, unbonding queue, and slashing history live on the `service.Operator`. Per Decision 1a of the federation→service migration, one operator address holds one bond per protocol and shares it across multiple peer bindings under that protocol.

Federation owns the per-peer **`BridgeBinding`** record: endpoint URL, content statistics, `suspended` flag. The `suspended` flag is toggled by `AfterOperatorUnderfunded` / `AfterOperatorReFunded` hooks — federation refuses content submissions from suspended bindings.

Bridge operators top up via `service.MsgTopUpBond`, unbond via `service.MsgUnbondOperator`; slashing flows through `service.MsgReportOperator`. **Bridge operator unbond/top-up/slash are NOT federation messages.**

## Content Verification

Bridge content enters as `PENDING_VERIFICATION`. Resolution paths:

1. **Anonymous community quorum (fast).** Verifiers fetch source content and submit hashes via x/shield (ZK-proven membership, scoped nullifiers). Quorum-based auto-resolution.
2. **Human jury (fallback).** Via x/rep when quorum fails.
3. **IBC content.** Verified by light client — no verifier needed.

Verifiers are DREAM-bonded via `BondedRole(ROLE_TYPE_FEDERATION_VERIFIER)` in x/rep (ESTABLISHED+ trust required). On `CHALLENGE_UPHELD`, federation files a system report against the bridge operator via `serviceKeeper.OpenSystemReport`; the controller resolves via the standard `service.MsgResolveReport` pipeline.

## Reputation Bridging

IBC attestation model. A peer can request attestation of a member's reputation; the source chain replies with a signed attestation that the requesting chain stores with:

- **50% discount** (configurable)
- **Cap at PROVISIONAL** equivalent (no high-trust import)
- **30-day TTL** (must re-attest)

## Identity Linking

Two-phase IBC challenge-response proving key ownership. Bridge-verified links for external protocols.

## Creator-Signed Outbound

`MsgFederateContent` requires the content creator's signature (or x/session delegation). Relayers cannot fabricate content.

## Compensation

Both federation roles are paid **SPARK**, by different modules, on different
scoring rules. Neither routes through x/split — that design was specced, never
built, and dropped.

**Bridge operators — paid here.** Federation takes its own capped daily claim
on the community pool in `BeginBlock` (`FundOperatorRewardPool`) and pays out
in `EndBlock` (`DistributeOperatorRewards`), with the residual overflow burned
after. Pay **is** volume-weighted on independently-verified submissions,
because an operator's throughput is attested by somebody else: the
`BridgeBinding` counters federation owns are incremented by verifiers, not by
the operator. `epoch_rejected` is an eligibility gate, so a submission
falsified by quorum costs the operator the epoch's pay. Operators are x/service
Operators — x/service owns the SPARK bond, status and slashing.

**Content verifiers — paid by x/rep.** A SPARK pool share plus a flat DREAM
stipend, distributed by x/rep's EndBlocker because the accuracy it is scored
from lives in the shared `RoleActivity` record x/rep owns, and paying resets
that record's per-epoch counters. Verifier pay is deliberately **not**
volume-weighted: a verifier self-certifies their own count, so paying per
verification would fund rubber-stamping. Volume is a floor
(`min_epoch_verifications`), never a weight. Federation keeps the verification
*mechanics* — bond sizing, slash amount, challenge windows, cooldown base,
`upheld_to_reset_overturns`; the reward params live in x/rep. See
[x/rep/README.md](../rep/README.md#federation-verifier-pay).

Verifiers bond **DREAM** via x/rep's `BondedRole` while being paid SPARK: the
bond denom and the reward denom are separate decisions. Bridge operators stake
SPARK on x/service.

## Messages (24)

### Peer Management

| Msg | Access | Purpose |
|---|---|---|
| `MsgRegisterPeer` | Commons Council | Register a peer (with optional `controller_group`) |
| `MsgRemovePeer` | Commons Council | Tombstone a peer (cursor-based pruning in EndBlocker) |
| `MsgSuspendPeer` / `MsgResumePeer` | Commons Council | Toggle peer status |
| `MsgUpdatePeerPolicy` | Operations Committee | Content type allowlists, rate limits, moderation |
| `MsgUpdatePeerController` | gov | Change controller_group for a peer |

### Bridge Bindings

| Msg | Signer | Purpose |
|---|---|---|
| `MsgRegisterBridge` | bridge operator | Create binding + register operator in x/service in one shot |
| `MsgUpdateBridge` | bridge operator | Edit endpoint / metadata |
| `MsgResyncBridgeCount` | Ops Committee OR gov | Recovery — re-count `BridgesByPeer` |
| `MsgPruneOrphanBindings` | Ops Committee OR gov | Recovery — prune BridgeBindings whose service.Operator is missing or terminal |

### Content Flow

| Msg | Purpose |
|---|---|
| `MsgFederateContent` | Outbound — creator-signed |
| `MsgSubmitFederatedContent` | Inbound from bridge |
| `MsgAttestOutbound` | Attestation of relayed content |
| `MsgModerateContent` | Hide / unhide federated content |
| `MsgVerifyContent` | Verifier submits source-hash match |
| `MsgChallengeVerification` | Challenge a verification |
| `MsgSubmitArbiterHash` | Anonymous arbiter quorum (via x/shield) |
| `MsgEscalateChallenge` | Escalate to x/rep jury |
| `MsgResolveEscalatedChallenge` | Apply jury verdict |

### Identity & Reputation

| Msg | Purpose |
|---|---|
| `MsgLinkIdentity` | Begin two-phase challenge-response link |
| `MsgConfirmIdentityLink` | Complete the link |
| `MsgUnlinkIdentity` | Sever an existing link |
| `MsgRequestReputationAttestation` | IBC request to source chain |

### Governance

`MsgUpdateParams`, `MsgUpdateOperationalParams`.

## Queries (19)

`Params`, `GetPeer`, `ListPeers`, `GetPeerPolicy`, `GetBridgeBinding`, `ListBridgeBindings`, `GetFederatedContent`, `ListFederatedContent`, `GetIdentityLink`, `ListIdentityLinks`, `ResolveRemoteIdentity`, `GetPendingIdentityChallenge`, `ListPendingIdentityChallenges`, `GetReputationAttestation`, `ListOutboundAttestations`, `VerifierActivity` (federation-local counters; bond/status queried via `query rep bonded-role ROLE_TYPE_FEDERATION_VERIFIER <addr>`), `GetVerificationRecord`, `GetEscalatedChallenge`, `OperatorRewardPool`.

`OperatorRewardPool` reports the bridge-operator SPARK pool's balance, cap,
headroom, today's draw, the daily funding cap and the inflation share. Without
it, "eligible but the pool was empty" and "not eligible" are indistinguishable
from outside. The verifier pool has an equivalent on the rep side
(`query rep role-reward-pools`).

Live bridge operator economic state is queried via `query service operator <addr> federation-bridge-<protocol>`.

## ServiceHooks (Federation as a Consumer)

Federation implements `servicetypes.ServiceHooks` in [keeper/hooks.go](keeper/hooks.go):

| Hook | Behavior |
|---|---|
| `AfterOperatorDissolved` | Prune all bindings for the operator under the given service_type |
| `AfterOperatorRetired` | Same as Dissolved |
| `AfterOperatorUnderfunded` | Set `Suspended=true` on all bindings |
| `AfterOperatorReFunded` | Clear `Suspended` on all bindings |

All four wrap their bodies in `defer recoverHookPanic` so a federation bug never rolls back an x/service slash. Composite ordering: **federation hooks fire before commons hooks**. Configured in [app/service_adapters.go](../../app/service_adapters.go).

## BeginBlocker

Draws one capped claim on the community pool into the bridge-operator reward
pool (`FundOperatorRewardPool`), bounded per UTC day by a share of inflation
and ledgered in state so the allowance survives restarts. Federation is ordered
**before x/split** in `BeginBlockers` for the same reason x/rep is: x/split
distributes whatever remains in the community pool to the councils in full, so
a module that skims must skim first or find an empty pool.

## EndBlocker (12 phases)

1. Prune expired federated content
2. Prune expired reputation attestations
3. Prune expired unverified identity links
4. Prune expired identity challenges
5. *(removed — x/service owns operator unbonding; federation reacts via `AfterOperatorRetired` hook)*
6. Expire unverified content (`PENDING_VERIFICATION → HIDDEN` after verification_window)
7. Release verifier bond commitments (challenge_window expired without challenge)
8. Expire arbiter resolution windows (no quorum → escalate to jury)
9. Finalize auto-resolutions (escalation window expired → verdict final)
10. Process peer removal queue (cursor-based)
11. Bridge-operator reward distribution, then residual overflow burn
    — distribution runs first so the pool drains to the operators who earned
    it and the burn only ever targets what is left
12. Bridge binding monitoring (inactivity warnings only — bond/underfunded signals come from x/service hooks)
13. Clean stale rate limit counters (inbound + outbound)

Verifier epoch rewards used to be a phase here. They are now distributed by
x/rep's EndBlocker; federation reports verifications and verdicts and pays
verifiers nothing.

## IBC Application

- Port: `federation`
- Channel ordering: UNORDERED
- Version: `federation-1`
- Packet types: `ReputationQueryPacket`, `ContentPacket`, `IdentityVerificationPacket`, `IdentityVerificationConfirmPacket`

## Invariants

`OrphanBindingsInvariant` (every BridgeBinding references a live `service.Operator`) and `BindingsByOperatorIndexInvariant` (reverse index agrees with primary) — both registered with `crisis`. Drift from fail-soft hook panics is caught here. `MsgPruneOrphanBindings` and `MsgResyncBridgeCount` are the dual-authority (Ops Committee OR gov) recovery messages — pure cleanup, no value mutation.

## Dependencies

| Module | Usage |
|---|---|
| `x/commons` | Auth, `IsGroupPolicyAddress` for controller validation, Ops Committee policy address |
| `x/service` | Bridge operator economics — `RegisterOperator`, `TopUpBond`, `OpenSystemReport`; subscribes to all 4 hooks |
| `x/rep` | Reputation, verifier DREAM bonds via `BondedRole(ROLE_TYPE_FEDERATION_VERIFIER)`, jury |
| `x/name` | Identity link resolution |
| `x/bank` | Challenge/escalation fees |
| `x/shield` | Anonymous arbiter resolution via ZK proofs |
| `x/distribution` + `x/mint` | Bridge-operator pool funding — the daily community-pool claim is sized as a share of inflation, so it reads `AnnualProvisions` and `GetCommunityTax`. Late-wired |
| `ibc-go` | IBC application |

### Late Wiring (app.go)

```
x/federation ← SetCommonsKeeper(commons), SetRepKeeper(rep)
             ← SetNameKeeper(name), SetShieldKeeper(shield)
             ← SetIdentityKeeper(identity)
             ← SetServiceKeeper(NewFederationServiceAdapter(service))
             ← SetDistrKeeper(NewDistrKeeperAdapter(distr))
             ← SetMintKeeper(NewMintProvisionsAdapter(mint))
             ← SetIBCKeeperFn(func() *ibckeeper.Keeper { ... })
```

### EndBlocker Ordering

x/service runs **before** x/federation in `app/app.go` so hook-fired binding state mutations settle before federation's own per-block work.

In `BeginBlockers`, x/federation runs **before x/split**, because its operator-pool
funding skims the community pool and x/split distributes the remainder to the
councils in full. Same constraint x/rep is under.

## Leaf-ness

Nothing depends on x/federation. It exports no hooks and no keeper interface to
another module; it only consumes (x/service's hooks, and the commons / rep /
name / shield / service / distribution / mint keepers late-wired in `app.go`).
The only cross-module mention is a module-account *name* string constant in
[x/service/types/keys.go](../service/types/keys.go), not a code dependency.

x/split does **not** query federation. An earlier design had operator and
verifier pay flowing through x/split, weighted by federation-owned counters;
it was specced, never built, and dropped in favour of the two direct
community-pool claims described under [Compensation](#compensation).
