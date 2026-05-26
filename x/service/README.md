# x/service

Generic SPARK-staked accountability primitive for off-chain agents performing work the chain cannot natively verify. Federation bridges today; in the future Akash funders, storage-pinning agents, external RPC providers, etc.

Full spec: [docs/x-service-spec.md](../../docs/x-service-spec.md).

## Concepts

### Operator

```
Operator key = (address, service_type)
            + controller
            + SPARK bond
            + status
```

- **`service_type`** — free-form `[a-z0-9-]{1,64}` string gated by a gov-managed `ServiceTypeConfig` allowlist. Any new module can propose a new service type without a proto bump.
- **`controller`** — the x/commons `Group` that owns tier-1 reports against this operator. Per-peer for federation bridges; typically Operations Committee otherwise.
- **`status`** — state machine: `ACTIVE → UNDERFUNDED → UNBONDING → RETIRED` (voluntary) or `SLASHED` (terminal).

### Service Type Registry

Each `ServiceTypeConfig` (gov-mutable via `MsgUpdateServiceTypeConfig`) carries:

- `min_bond_uspark` — registration threshold
- `unbonding_period_blocks`
- `unilateral_slash_cap_bps` (default 5%)
- `tier1_aggregate_cap_bps` (default 15% over ~90 days; snapshotted at window open to prevent gaming)
- `tier1_cooldown_blocks` (default ~7 days)
- `report_contest_window_blocks` (escrow window before T1 slashes go to community pool)
- `report_timeout_action` — `DISMISS` (default) or `ESCALATE` (used by `federation-bridge-*` types)
- `max_escalated_blocks` — jury verdict deadline before auto-REJECT

### Two-Tier Slashing

**Tier 1 (controller-resolved).** A controller can unilaterally slash up to `unilateral_slash_cap_bps` of *current* bond per report, subject to a sliding-window aggregate cap (`tier1_aggregate_cap_bps`) and per-controller cooldown. Slashes are **escrowed** in a tagged sub-pool for `report_contest_window_blocks` before going to the community pool — the operator can `MsgContestSlash` during that window and force escalation.

**Tier 2 (x/rep jury).** Larger slashes, operator-contested T1, or system reports against captured controllers all escalate to an x/rep jury via `MsgResolveReportByJury`. Tier-2 verdicts skip escrow and go straight to `FundCommunityPool`.

### Reversibility

Tier-1 escrow is per-report and per-operator. If the operator contests within the window, the escrowed coins return to bond. Otherwise the EndBlocker (or an eager inline path on `MsgClaimUnbondedBond`) releases them to the community pool.

### Reporter Anti-Griefing

- **`report_deposit`** SPARK refunded on T1_SLASH / ACCEPT / REDUCE; forfeited on REJECT.
- Sliding ring-buffer rate limit per `(reporter, operator)` pair.
- Reporters who are members of the operator's controller Group are blocked — closes a self-route drainage attack where a malicious controller member could open shell reports and resolve them in their favor.
- **No bounty by design** — preserves the deposit-only incentive structure.

### System Reports (Privileged Caller API)

Allowlisted modules (`federation` today) file reports via the keeper-level `OpenSystemReport` with a `dedupeKey` for idempotency and a per-caller sliding-window rate limit. The federation module account is the recorded reporter. Used for federation challenge-quorum-upheld slashes. Auth is forward-derived via `authKeeper.GetModuleAddress` — no Msg path, no off-chain key.

### Operator-Lifecycle Hooks

`ServiceHooks` interface, four callbacks:

| Hook | Fired on | Used by |
|---|---|---|
| `AfterOperatorDissolved` | SLASHED terminal | x/commons (cancel matching RecurringSpend), x/federation (prune bindings) |
| `AfterOperatorRetired` | RETIRED terminal | x/federation (prune bindings) |
| `AfterOperatorUnderfunded` | bond drops below `min_bond_uspark` | x/federation (suspend bindings) |
| `AfterOperatorReFunded` | bond restored | x/federation (resume bindings) |

**Hook ordering:** federation hooks fire **before** commons hooks (federation cleans bindings first; commons then cancels recurring spends). Both wrap their bodies in `defer recover` (fail-soft) — a bug in either consumer must never roll back an x/service slash, which would brick all bridge accountability chain-wide.

### Controller Transfer

Stranded operators (controller Group dissolved or empty) recover via `MsgOpenControllerTransferCase` → x/rep jury → `MsgFinalizeControllerTransfer`. The opener escrows `report_deposit`, refunded on ACCEPT, forfeited on REJECT.

## Messages

### Registration & Lifecycle (operator-signed)

| Msg | Purpose |
|---|---|
| `MsgRegisterOperator` | Bond SPARK, set service_type + controller + metadata |
| `MsgUpdateMetadata` | Edit metadata URI / fields |
| `MsgUnbondOperator` | Begin unbonding window |
| `MsgClaimUnbondedBond` | Withdraw bond after `unbonding_period_blocks` |
| `MsgTopUpBond` | Add SPARK to bring an UNDERFUNDED operator back to ACTIVE |

### Reports & Slashing

| Msg | Signer | Purpose |
|---|---|---|
| `MsgReportOperator` | anyone (with deposit) | Open a tier-1 report |
| `MsgResolveReport` | operator's controller | Tier-1 resolve (slash / accept / reduce / reject) within caps |
| `MsgContestSlash` | operator | Escalate T1 slash to jury within contest window |
| `MsgResolveReportByJury` | Ops Committee on commons council | Tier-2 verdict |

### Service Type Registry (gov-authority)

| Msg | Purpose |
|---|---|
| `MsgUpdateServiceTypeConfig` | Create/update/disable a ServiceTypeConfig |
| `MsgUpdateParams` | Update module params |

### Controller Transfer (jury-authority)

| Msg | Purpose |
|---|---|
| `MsgOpenControllerTransferCase` | Open jury case for a stranded operator |
| `MsgFinalizeControllerTransfer` | Apply jury verdict (transfer or reject) |

### Keeper-Only APIs (no Msg)

- `OpenSystemReport(ctx, callerModuleAddr, operator, serviceType, slashBps, evidenceURI, dedupeKey)` — gated by `params.system_report_callers` allowlist.
- `TopUpBond(ctx, opBytes, serviceType, additionalBond)` — same effect as `MsgTopUpBond`, exposed for modules.

## Queries

| Query | Description |
|---|---|
| `Params` | Module parameters |
| `Operator` | Single operator by `(address, service_type)` |
| `ServiceType` | `ServiceTypeConfig` by service_type |
| `ServiceTypes` | All ServiceTypeConfigs (paginated) |
| `Operators` | All operators (paginated) |
| `OperatorsByController` | Operators under a given controller (paginated) |
| `OperatorsByServiceType` | Operators within a service_type (paginated) |
| `Report` | Single report by ID |
| `ReportsByOperator` | Reports against an operator (paginated) |
| `OperatorReputationSnapshot` | Lazy bond-block reputation accrual snapshot |

## EndBlocker

Four sweeps, gas-bounded by `endblocker_sweep_limit`:

1. **Underfunded sweep** — force-unbond operators whose grace window expired.
2. **Pending report auto-action** — DISMISS or ESCALATE based on per-type `report_timeout_action`.
3. **Escalated report auto-timeout** — REJECT-equivalent if no jury verdict within `max_escalated_blocks`; release contested-T1 escrow back to bond.
4. **Tier-1 escrow release** — move uncontested escrowed slashes to community pool after `report_contest_window_blocks`.

## Reputation Accrual

Lazy O(1) bond-block tracking on the `service-operator` tag in x/rep, with an anti-gaming cap (max bond-blocks per address, not summed across operators). Read via `QueryOperatorReputationSnapshot`.

## Genesis

`DefaultGenesis` seeds:

- `params`
- **Two ServiceTypeConfigs** at genesis: `federation-bridge-activitypub` and `federation-bridge-atproto`, both with `ReportTimeoutAction=ESCALATE`. This is owned by x/service (not federation) so federation's `BridgeBinding` records can reference live `service.Operator`s at boot.

Init order: x/commons → x/service → x/federation.

## Dependencies

| Module | Usage |
|---|---|
| `x/auth` | Forward-derive auth for `OpenSystemReport` |
| `x/bank` | Bond escrow, slash transfers |
| `x/commons` | `IsGroupAddress`, controller validation |
| `x/rep` | Jury via `CreateAppealInitiative`, `service-operator` reputation tag |
| `x/distribution` | Community pool deposit for finalized slashes |
| `x/session` | Operator key delegation |

### Late Wiring (app.go)

- `SetCrossModuleKeepers(commons, rep, distr)` — trimmed adapter interfaces.
- `SetHooks(NewMultiServiceHooks(federation, commons))` — **order is load-bearing**: federation hooks fire before commons hooks on `AfterOperatorDissolved` / `AfterOperatorRetired`.

### EndBlocker Ordering

x/service runs **before** x/federation in `app/app.go` so hook-fired binding state mutations (suspend/resume, prune) settle before federation's own per-block work runs.

## Invariants

`OperatorBondInvariant`, `Tier1EscrowInvariant`, `SystemReportRateLimitInvariant` — all registered with `crisis`.

## CLI

```bash
# Queries
sparkdreamd query service operator <addr> <service-type>
sparkdreamd query service operators-by-service-type federation-bridge-activitypub
sparkdreamd query service service-types

# Tx
sparkdreamd tx service register-operator <service-type> <controller> <bond-uspark> <metadata-uri> --from operator
sparkdreamd tx service top-up-bond <service-type> <bond-uspark> --from operator
sparkdreamd tx service unbond-operator <service-type> --from operator
sparkdreamd tx service report-operator <operator> <service-type> <slash-bps> <evidence-uri> <deposit-uspark> --from reporter
```
