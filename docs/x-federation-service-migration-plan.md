# x/federation → x/service Migration Plan

Status: Proposed, not yet implemented.

Goal: replace x/federation's parallel bridge-operator infrastructure (escrow, status state machine, slashing, unbonding queue) with calls into x/service, so federation owns only what's federation-specific and x/service owns the operator economic primitive.

## Scope

**In scope:**
- Bridge operator lifecycle currently in `MsgRegisterBridge`, `MsgUnbondBridge`, `MsgTopUpBridgeStake`, `MsgSlashBridge`, `MsgRevokeBridge`.
- Bond escrow, status state machine, unbonding queue, slashing.
- The TODO'd challenge-resolution slash paths (`MsgSubmitArbiterHash`, `MsgEscalateChallenge`).

**Out of scope (stays as-is):**
- **Verifiers**: DREAM-bonded via `x/rep BondedRole(ROLE_TYPE_FEDERATION_VERIFIER)`. Different role, different bonding primitive, different economic model.
- Content federation flow itself (`MsgFederateContent`, `MsgVerifyContent`, etc.) except where they call into slashing.
- Identity linking, reputation attestation, peer policies (federation-internal).
- Peer registry and `PeerPolicy` (federation owns these; only the `controller_group` field is added).

## Agreed design (hybrid model)

- **Federation owns**: `Peer`, `PeerPolicy`, `BridgeBinding(operator, peer_id, protocol, endpoint, content stats)`, `max_bridges_per_peer`, peer-state transitions, content/identity/attestation flows.
- **x/service owns**: bond escrow, ACTIVE/UNDERFUNDED/UNBONDING/SLASHED/RETIRED status, tier-1 controller verdicts, tier-2 jury escalation, controller-transfer cases, operator lifecycle hooks.
- **Federation never** touches service's bond accounting or status fields directly; it queries through the public API and reacts to hooks.

### Decision 1: per-protocol `service_type`

Two `ServiceTypeConfig`s seeded at genesis:

- `federation-bridge-activitypub`
- `federation-bridge-atproto`

Each carries independent `min_bond`, `unilateral_slash_cap_bps`, `tier1_aggregate_cap_bps`, `unbonding_period_blocks`, tunable later per protocol via gov. Rejected: single bridge type (no protocol-level tuning) and per-peer type (overkill, duplicates Peer-curation work).

### Decision 1a: shared bond across same-protocol bridges per operator address

One `service.Operator` per `(address, service_type)`. An operator address running multiple ActivityPub bridges shares one bond across them, and a slash for one peer's misbehavior affects all of that operator's ActivityPub bridges.

Rationale: matches x/service's keying, simpler economic model, more natural operator UX (one bond, one status). Cost: operators bridging multiple unrelated peers on the same protocol may want distinct addresses per peer to avoid shared-fate risk. Acceptable operational burden, and arguably aligns incentives (operators are accountable for their bridge portfolio, not per peer).

Federation's `MsgRegisterBridge` for a second peer under an already-registered `(address, service_type)` does not call `serviceKeeper.RegisterOperator` again — it just writes a new `BridgeBinding` against the existing Operator. Bond top-ups (if the operator wants to scale up exposure across many peers) happen via `service.MsgTopUpBond` directly.

Rejected: per-(address, peer_id) Operator (would require one ServiceTypeConfig per peer, which we already rejected in Decision 1).

### Decision 2: per-peer controller, OpsComm default

Each `Peer` carries an optional `controller_group` (x/commons Group policy address). If unset, federation resolves it to the Operations Committee at `MsgRegisterBridge` time, and the resolved address is captured on the resulting `service.Operator`.

Rationale (the discussion that led here): a fixed OpsComm-controls-all model was the proposal's main centralization vector. The failure modes that mattered were unresponsiveness (slow weekly cadence), cross-org friction (partner-run peers had zero say in their own bridges), conflict of interest (OpsComm members may run their own bridges), and capture concentration. Per-peer controller lets partner-run peers (e.g. a Mastodon instance run by Org X) nominate a joint-stewardship Group, while most local peers still default to OpsComm so bootstrap stays cheap.

Per-peer is the right granularity because peers are already a vetted, federation-scoped concept. Per-bridge operator-nominated controllers were rejected: they mostly relocate the trust problem (operators handpick their judges).

Visibility note: for partner-org peers, the controller Group's membership is publicly queryable. Whoever files a report against that peer's bridge knows exactly which humans decide their fate. This is a social-dynamics shift, not a privacy bug, but worth flagging in the federation spec.

### Decision 3: auto-escalate on report timeout

A per-`ServiceTypeConfig` `ReportTimeoutAction` enum (`DISMISS` default, `ESCALATE` opt-in). Federation's two configs set it to `ESCALATE` so a silent/captured controller can stall a slash by exactly one timeout window before it goes to jury anyway. The threat of escalation itself disciplines controllers.

### Decision 4: delete `MsgSlashBridge` and `MsgRevokeBridge`

All slashing flows through `service.MsgReportOperator`. Revoke side-effects (unlinking binding from peer, decrementing peer's `bridges_count`, clearing in-flight content state) live in federation's `AfterOperatorDissolved` hook handler.

Note on the "file a report against yourselves" pattern: x/service has no controller-initiated direct slash. If OpsComm notices misbehavior themselves, one member files `MsgReportOperator` from their personal account (escrowing `report_deposit`), then OpsComm-as-body resolves it via `MsgResolveReport(T1_SLASH)`. On T1_SLASH the deposit is refunded, so the cost is one extra message plus gas. Mildly awkward, but it preserves the invariant that every slash has a public report trail with named reporter and stated evidence.

### Decision 5: challenge-derived slashes go through `service.MsgReportOperator`

When `MsgSubmitArbiterHash` reaches quorum-upheld, federation files a system report (PENDING) against the bridge operator. The peer's controller resolves normally; slash amount comes from `ServiceTypeConfig`'s `challenge_default_slash_bps`, not from federation params. Rejected: auto-resolve via federation module account (bypasses controller, creates a second controller-equivalent path, splits economic parameters across two modules).

Accountability asymmetry note: the federation module account is the recorded reporter for system reports. Module accounts cannot be slashed for false reports and have no reputation, so the controller's tier-1 review is the only immediate check. `ReportTimeoutAction=ESCALATE` ensures jury is the effective failsafe.

Variable severity note: `challenge_default_slash_bps` on the `ServiceTypeConfig` sets the *proposed* slash amount when federation files the system report. The controller can adjust the actual slash at `MsgResolveReport` time (downgrade for borderline evidence, upgrade for egregious cases) within the `unilateral_slash_cap_bps` ceiling. So `challenge_default_slash_bps` is a starting point that captures expected severity, not a fixed outcome.

### Decision 6: `max_bridges_per_peer` as kill-switch, default 1000

Keep the param, but frame it explicitly as a kill switch rather than a policy lever. Default 1000 per peer, clearly above any realistic legit use. The real defenses against runaway registration are the `min_bond` economic gate, content-hash deduplication, and per-peer rate limits; this param exists only so governance can dial it down without a chain upgrade if some unknown-unknown materializes.

Rationale: the bond is what actually gates participation, and bridge redundancy is a resilience feature (multiple operators per peer = no single point of failure), so tight caps would fight the property we want. But dropping the param entirely and re-adding it post-mainnet would carry migration cost, so leaving a high-default safety valve in place is the cheaper insurance. The proto definition should carry a comment marking it as kill-switch-only so future gov conversations don't treat it as a normal policy knob. An invariant checker asserts `bridges_count == count(BridgesByPeer for peer)` so any drift from missed hook fires is caught early.

Rejected: tight caps (5–50) become policy by accident, lock out late entrants, and need gov to grow with the federation. Dropping the param entirely was the cleanest option but loses the pre-mainnet-only window to keep it for free.

## Phase 0: x/service prerequisites (lands first, independent)

Multiple additions to x/service, all additive, no existing-consumer impact.

| Change | Files | Notes |
| --- | --- | --- |
| `ReportTimeoutAction` enum on `ServiceTypeConfig` (DISMISS default, ESCALATE opt-in) | `proto/sparkdream/service/v1/service_type_config.proto`, `x/service/keeper/abci.go`, `x/service/keeper/msg_server_update_service_type_config.go` | EndBlocker reads the per-type action; ESCALATE opens a jury case via the existing `CreateAppealInitiative` adapter. |
| `challenge_default_slash_bps` field on `ServiceTypeConfig` | same as above | Default slash amount when a system report is filed without an explicit override. Validated as `≤ unilateral_slash_cap_bps` (otherwise gov could side-step the cap). This is the *proposed* slash; the controller can adjust at `MsgResolveReport` time within the cap. |
| `AfterOperatorUnderfunded` / `AfterOperatorReFunded` hooks on `ServiceHooks` | `x/service/types/expected_keepers.go`, `x/service/keeper/abci.go` (when bond drops below `min_bond` after a slash), `x/service/keeper/msg_server_top_up_bond.go` (when bond crosses back above) | Lets consumers gate operator-active checks without polling status on every operation. `AfterOperatorUnderfunded` does **not** fire if the operator is already in `UNBONDING` (they're exiting anyway); `AfterOperatorReFunded` is a no-op for UNBONDING operators (top-ups during unbonding don't reactivate). |
| `TopUpBond(ctx, operatorAddr sdk.AccAddress, amount sdk.Coin) error` public API | `x/service/keeper/public_api.go` | Keeper-level top-up so consumers (federation's `MsgRegisterBridge` re-registration path) don't have to dispatch `MsgTopUpBond` through the msg router from inside a handler. Same checks as the msg server (operator must exist, be in `ACTIVE`/`UNDERFUNDED`, etc.). |
| `OpenSystemReport(ctx, callerModuleAddr sdk.AccAddress, operator, serviceType, slashBps, evidenceURI, dedupeKey []byte) (reportID, error)` public API | `x/service/keeper/public_api.go` | Bypasses `min_reporter_trust_level` and `report_deposit`. `callerModuleAddr` must equal the calling module's own module account address. **Authorization pattern (forward-derive, not reverse-lookup)**: x/service iterates the allowlist and for each `name` checks `authKeeper.GetModuleAddress(name).Equals(callerModuleAddr)`; first match wins. Cosmos SDK's auth keeper does not expose a reverse `address → module name` lookup, so the forward derivation is the SDK-native pattern. O(allowlist_size) per call, but the allowlist is tiny (1 today). `dedupeKey` enforces idempotency: a second call with the same `(matchedModuleName, dedupeKey)` within `report_timeout_blocks` returns the existing report_id instead of opening a new one. Emits `system_report_opened` event including `caller_module` for auditability. |
| Caller allowlist for `OpenSystemReport` and for `RegisterOperator`'s `source` parameter | `x/service/keeper/public_api.go` (constant), `x/service/types/errors.go` (`ErrUnauthorizedSystemCaller`) | Sorted slice (not map): `var allowedSystemCallers = []string{"federation"}`. Go map iteration is non-deterministic and would risk a consensus break if "first match wins" returned different results across nodes once the list grows beyond one entry. Slice iteration is deterministic. Adding new callers requires a source patch (intentional privilege-gate friction). **Auth model**: the allowlist + auth-keeper lookup is *defense-in-depth + auditability*, not the primary authorization barrier. Real authorization is keeper-wiring discipline (only federation receives a `ServiceKeeper` reference at app wiring). The address-then-name lookup prevents accidental misuse and forces any spoofer to explicitly impersonate a named module account, which shows up in logs and is detectable. |
| Per-caller rate limit on `OpenSystemReport` | `proto/sparkdream/service/v1/params.proto` (`max_system_reports_per_caller_per_window`, default 50; `rate_limit_window_blocks`, default ~1 day in blocks), `x/service/keeper/public_api.go` (sliding-window counter per `(callerModuleName, window)`) | Caps blast radius if a consumer module bugs out and produces unique-dedupeKey reports in a loop. x/service has no native epoch concept, so the rate window is an explicit block-count param rather than reusing season/shield epochs. Default 50 per ~1-day window is a starting guess; **tunable, calibrate from observed federation challenge volume** post-launch. Emits `system_report_rate_limited` event on rejection so observers can alert. Idempotent re-calls (same `dedupeKey`) don't count against the limit. |
| Cross-field validation on `MsgUpdateServiceTypeConfig` | `x/service/keeper/msg_server_update_service_type_config.go` | Every update re-validates `challenge_default_slash_bps ≤ unilateral_slash_cap_bps` in both directions: raising the default above the cap is rejected, and lowering the cap below the existing default is also rejected (gov must lower the default first, then the cap). Prevents the "lower the cap, default silently exceeds it" state. **Gov UX note**: a proposal that only changes the cap will fail validation if the existing default would be left above the new cap. Gov must either split into two proposals (lower default first, then cap) or use a single-proposal update that sets both fields. Document this in the x/service spec to avoid surprise rejections. |

Acceptance criteria for the x/service prerequisites PR:

- Unit test for each new path.
- Negative test: an unallowed module (e.g., x/blog) attempting `OpenSystemReport` with its own module address gets `ErrUnauthorizedSystemCaller`.
- Caller-spoofing test: an unallowed module attempts `OpenSystemReport` with `callerModuleAddr = <federation module account address>`. Because the spoofer is not actually federation, this requires the spoofer to have a `ServiceKeeper` reference in the first place (which keeper-wiring should prevent); the test verifies that *if* such a reference somehow existed, the auth-keeper lookup still returns "federation" and the call proceeds. The test documents that the real defense is wiring scope, with the auth lookup providing auditability via the `caller_module` event field.
- Idempotency test: repeated calls with the same `dedupeKey` return the existing report_id; replay across `report_timeout_blocks` boundary opens a new report (TTL expiry is documented behavior).
- Rate-limit test: 51st report from `federation` in one epoch returns `ErrSystemReportRateLimited` and emits the rate-limit event; counter resets next epoch.
- E2E test: `ReportTimeoutAction=ESCALATE` opens a jury case on timeout instead of dismissing.

## Phase 1: federation type and schema changes

| Change | Files | Notes |
| --- | --- | --- |
| Add `controller_group` (optional string) to `Peer` | `proto/sparkdream/federation/v1/types.proto` | Optional at registration; resolved to OpsComm policy address at consumption time if empty. |
| Add `MsgRegisterPeer.controller_group` and new `MsgUpdatePeerController` | `proto/sparkdream/federation/v1/tx.proto`, `x/federation/keeper/msg_server_register_peer.go` | If `controller_group` is non-empty, validate at peer-registration time via `commonsKeeper.IsGroupPolicyAddress(controller_group)`; reject garbage addresses up-front rather than silently falling back to OpsComm at bridge-registration time (which would surprise the peer registrant). `MsgUpdatePeerController` is gov-authority only and applies only to bridges registered *after* the update; existing bridges keep their original controller; transferring those requires `service.MsgOpenControllerTransferCase`. |
| Slim `BridgeOperator` → `BridgeBinding` | `proto/sparkdream/federation/v1/types.proto` | Strip `stake`, `status`, `unbonding_end_time`, `slash_count` (those live on the `service.Operator`). Keep `address`, `peer_id`, `protocol`, `endpoint`, `registered_at`, `last_submission_at`, `content_submitted/verified/rejected/unverified`. Add `suspended bool` (toggled by underfunded hooks). |
| Add reverse index `(service_type, address, peer_id)` as `cosmossdk.io/collections.KeySet` | `x/federation/keeper/keeper.go` (new `BindingsByOperator` KeySet) | Multi-valued by design (consequence of Decision 1a: one Operator can back multiple bindings, one per peer). Written at `MsgRegisterBridge`, entries removed by hook handlers. Hook handlers iterate `BindingsByOperator.Iterate(prefix=(service_type, address))` to find all peer_ids for that operator. A single-valued map shape would fail to enumerate multi-binding operators and is the wrong choice here. |
| Drop params: `min_bridge_stake`, `bridge_unbonding_period`, `bridge_operator_max_slashes`, `bridge_revocation_cooldown` | `proto/sparkdream/federation/v1/params.proto` | All migrate to `ServiceTypeConfig`. |
| Keep `max_bridges_per_peer`, default `1000`, kill-switch framing | `proto/sparkdream/federation/v1/params.proto` | Proto comment must mark this as kill-switch-only (gov leaves it alone unless abuse emerges). Real participation gating is `min_bond` + content-hash dedup + per-peer rate limits. |
| Add `MsgResyncBridgeCount(authority, peer_id)` (Operations Committee or gov) | `proto/sparkdream/federation/v1/tx.proto`, `x/federation/keeper/msg_server_resync_bridge_count.go` | Re-counts `BridgesByPeer` for the peer and overwrites `bridges_count`. Recovery path when the count-invariant triggers without needing a chain upgrade. Authority is OpsComm OR gov (same rationale as `MsgPruneOrphanBindings`). |
| Add `MsgPruneOrphanBindings(authority, peer_id)` (Operations Committee or gov) | `proto/sparkdream/federation/v1/tx.proto`, `x/federation/keeper/msg_server_prune_orphan_bindings.go` | Re-runs the binding cleanup logic for any `BridgeBinding` whose referenced `service.Operator` is `SLASHED`/`RETIRED`/missing. Recovery path when fail-soft hook handlers swallowed a panic and left an orphan. **Authority is OpsComm OR gov**: this is pure cleanup (no value mutation), and gov-only would mean orphans sit for weeks during high-volume incidents. OpsComm authority lets the operational response happen in hours; gov retains parallel authority as fallback. Pairs with `MsgResyncBridgeCount` (also dual-authority for the same reason). |

Pre-mainnet means no migration handler needed.

### Initial ServiceTypeConfig values (genesis seed)

Both configs share defaults; tune per protocol later if risk model diverges. Ports forward the current federation `min_bridge_stake` and `bridge_unbonding_period`.

| Field | `federation-bridge-activitypub` | `federation-bridge-atproto` |
| --- | --- | --- |
| `min_bond` | 1000 SPARK (1_000_000_000 uspark) | 1000 SPARK |
| `unbonding_period_blocks` | ~14 days at chain block time | ~14 days |
| `unilateral_slash_cap_bps` | 500 (5%, x/service default) | 500 |
| `tier1_aggregate_cap_bps` | 1500 (15%) | 1500 |
| `tier1_cooldown_blocks` | ~7 days | ~7 days |
| `challenge_default_slash_bps` | 100 (1%) | 100 |
| `report_timeout_blocks` | ~14 days | ~14 days |
| `report_timeout_action` | `ESCALATE` | `ESCALATE` |
| `report_deposit` | 10 SPARK | 10 SPARK |
| `min_reporter_trust_level` | `TRUST_LEVEL_ESTABLISHED` | `TRUST_LEVEL_ESTABLISHED` |
| `enabled` | true | true |

## Phase 2: wire service into federation

| Change | Files |
| --- | --- |
| Add `ServiceKeeper` interface to federation expected_keepers | `x/federation/types/expected_keepers.go` |
| Inject `ServiceKeeper` via `Set*Keeper` (avoids cycle, matches existing late-keeper pattern) | `x/federation/keeper/keeper.go`, `app/app.go` |
| Register federation as `ServiceHooks` subscriber | `app/service_adapters.go` (extend `CommonsServiceHooks` to multiplex, or add a parallel `FederationServiceHooks` + a composite wrapper) |

Required surface from `ServiceKeeper`: `RegisterOperator`, `GetOperator`, `HasSlashedRecord`, `OpenSystemReport`, `TopUpBond`, `GetServiceTypeConfig`.

**App init order dependency**: federation's `MsgRegisterBridge` resolves OpsComm policy address at registration time. If federation initializes before commons in `app.go`'s ordering, the OpsComm Group doesn't exist yet and the lookup fails. Current `app.go` order has commons before federation, which is correct, but the constraint should be documented at the top of `x/federation/keeper/keeper.go` so a future module-order refactor doesn't silently break this.

**Genesis init order dependency**: x/service's genesis must run before x/federation's, since federation `BridgeBinding` initialization references `service.Operator` records that must already exist in state. Add x/service before x/federation in `app.go`'s `genesisModuleOrder` (and `initGenesisOrder` if separately defined). The ServiceTypeConfigs for both bridge types must be seeded in x/service's genesis state (not federation's), even though they conceptually belong to the federation use case. Add a comment in federation's genesis loader pointing at x/service's genesis for the bridge ServiceTypeConfigs.

**EndBlocker order dependency**: x/service must run before x/federation in `app.go`'s `endBlockerOrder`. Service's EndBlocker fires hooks (UNDERFUNDED sweep, escrow release, timeout escalation) that re-enter federation's hook handlers and may mutate `BridgeBinding` state (suspend/unsuspend, prune). Federation's own EndBlocker (peer pruning, cleanup) must see settled state to make correct decisions. If federation runs first, a peer-pruning iteration could miss bindings that were about to be suspended in the same block.

## Phase 3: reshape `MsgRegisterBridge`

`x/federation/keeper/msg_server_register_bridge.go` becomes a thin orchestrator. **Step ordering is load-bearing: any call into x/service that can fire a hook back into federation must happen *after* the new `BridgeBinding` is written**, otherwise the hook handler iterates `BindingsByOperator` and silently misses the in-flight binding.

1. Load peer; check `bridges_count < max_bridges_per_peer`; resolve `controller := peer.controller_group ?? opsCommPolicyAddr`.
2. Map `peer.type` to `service_type` (`PROTO_ACTIVITYPUB` → `federation-bridge-activitypub`, etc.).
3. Check if `service.Operator` for `(msg.creator, service_type)` already exists (consequence of Decision 1a):
   - **Exists**: skip `RegisterOperator`. Validate that the existing Operator's controller matches the resolved controller (mismatch is a hard error: "use a different address for the new peer, or transfer the existing Operator's controller via `service.MsgOpenControllerTransferCase`"). Do **not** call `TopUpBond` yet; defer to step 5.
   - **New**: call `serviceKeeper.RegisterOperator(ctx, RegisterArgs{callerModuleAddr: federationModuleAccount, address: msg.creator, controller, service_type, bond: msg.stake, metadata: serialized_bridge_metadata})`. Escrows the bond and creates the `service.Operator` atomically. `RegisterOperator` validates `bond >= min_bond` from the ServiceTypeConfig — federation does **not** re-validate this; service is the single source of truth for `min_bond` and pre-checking in federation just creates drift risk if the value changes. `RegisterOperator` does not fire hooks, so no re-entrance risk here. Skip step 5 (initial bond is already in).
4. Write the `BridgeBinding` and the reverse index entry `(service_type, address) → peer_id`. After this point, any hook firing on the operator sees the new binding.
5. **Only on the "Exists" branch with `msg.stake > 0`**: call `serviceKeeper.TopUpBond(ctx, msg.creator, msg.stake)` (the Phase 0 public API; not the msg server, since we're inside a handler). If the operator was UNDERFUNDED, this fires `AfterOperatorReFunded` synchronously; the hook iterates `BindingsByOperator` and now correctly includes the binding written in step 4.
6. Transition peer PENDING → ACTIVE on first bridge (existing logic, preserved).

If any service call fails (`HasSlashedRecord`, underfunded with no top-up, service_type disabled, controller mismatch), federation aborts the whole tx cleanly. The atomic-tx guarantee rolls back any partial writes from earlier steps.

## Phase 4: delete duplicated lifecycle messages

Delete from federation:

- `MsgUnbondBridge` → operator uses `service.MsgUnbondOperator` directly. Federation's `AfterOperatorRetired` hook prunes the binding and decrements peer's `bridges_count`.
- `MsgTopUpBridgeStake` → operator uses `service.MsgTopUpBond`.
- `MsgSlashBridge` → misbehavior reports use `service.MsgReportOperator`.
- `MsgRevokeBridge` → covered by `AfterOperatorDissolved` hook when controller or jury upholds a slash with `dissolve=true`.
- `BridgeUnbondingQueue` storage and `releaseUnbondedBridgeStakes` EndBlocker phase → x/service owns this.

Keep: `MsgRegisterBridge`, `MsgUpdateBridge` (endpoint-only edits, federation-specific).

`MsgUpdateBridge` status policy: operators in `ACTIVE`, `UNDERFUNDED`, or `UNBONDING` may all update their endpoint URL. Allowing UNBONDING operators to redirect is intentional (they may need to forward traffic to a successor during wind-down). `SLASHED` and `RETIRED` (archived) operators cannot update; the binding is being cleaned up by hooks anyway. Federation queries `serviceKeeper.GetOperator` to gate this.

UX note: operator-facing CLI now mixes `tx federation register-bridge` with `tx service unbond-operator` / `tx service top-up-bond`. Document this asymmetry clearly in the federation spec; the underlying reason is that economic actions (bond changes, exit) are pure operator decisions while register + endpoint binding need federation orchestration.

**x/session allowed-msg-types update**: x/session's allowed-msg-types list (the bounded allowlist of message types operators can scope session keys to) must be updated. Add `service.MsgUnbondOperator`, `service.MsgTopUpBond`, `service.MsgUpdateMetadata`, `service.MsgClaimUnbondedBond`. Remove federation's now-deleted messages if they were on the list. This is a gov-authority change to the x/session params and should land in the same migration window.

## Phase 5: federation `ServiceHooks` implementation

Federation subscribes to four service hooks:

| Hook | Action |
| --- | --- |
| `AfterOperatorDissolved(operator, service_type)` | Look up all bindings for `(service_type, operator)` via reverse index (there may be more than one per Decision 1a); for each, mark binding inactive (or delete), decrement that peer's `bridges_count`, clear in-flight content claims. |
| `AfterOperatorRetired(operator, service_type)` | Same cleanup as Dissolved, no punitive side effects. |
| `AfterOperatorUnderfunded(operator, service_type)` | Mark all bindings for that `(service_type, operator)` as `suspended=true`. Suspended bindings refuse new content submissions (federation gates `MsgFederateContent` etc. on `!binding.suspended`). Bindings are not deleted. |
| `AfterOperatorReFunded(operator, service_type)` | Clear `suspended` flag on all bindings for that `(service_type, operator)`. |

**Fail-soft pattern (required)**: each hook handler in `x/federation/keeper/hooks.go` wraps its body in `defer func() { if r := recover(); r != nil { ctx.Logger().Error("federation hook panic", "hook", "...", "operator", op, "panic", r); ctx.EventManager().EmitEvent("federation_hook_failure", ...) } }()`. A bug in federation must never roll back a service slash, which would brick all bridge accountability chain-wide. Tests must include an artificial-panic case proving the slash still goes through.

**Consequence: orphan bindings.** Fail-soft means a panicking hook leaves a `BridgeBinding` pointing at a `SLASHED`/`RETIRED` `service.Operator`. The `bridges_count` invariant catches *count* drift but not *content* drift. Two mitigations:

1. **Second invariant** (`x/federation/keeper/invariants.go`): for every `BridgeBinding`, the referenced `service.Operator` must be in `{ACTIVE, UNDERFUNDED, UNBONDING}`. Orphans (live binding referencing SLASHED/RETIRED/missing Operator) violate it.
2. **Recovery**: `MsgPruneOrphanBindings(peer_id)` (Phase 1, gov-only) re-runs cleanup on any violating bindings. Pairs with `MsgResyncBridgeCount`.
3. **Observability**: the `federation_hook_failure` event is what off-chain monitors watch to alert humans into running the recovery messages.

A periodic EndBlocker reconciliation sweep was considered but deferred: it adds per-block cost for a rare failure mode, and the invariant + gov-callable recovery covers the case adequately.

**`AfterOperatorReFunded` during `UNBONDING`**: x/service does not fire `AfterOperatorReFunded` for operators in `UNBONDING` (top-ups during unbonding don't reactivate the operator, per Phase 0). Federation's hook handler additionally defensively no-ops if the operator's status query returns `UNBONDING`, in case x/service's behavior ever changes.

**Hook ordering (required)**: in `app/service_adapters.go`, the composite ServiceHooks implementation invokes federation hooks **before** commons hooks on Dissolved (federation cleans binding first, then commons cancels recurring spends). On Retired, same order. The composite handler has an explicit comment documenting this ordering and the reason (deterministic external event order; federation cleanup is cheaper and shouldn't depend on commons state, while commons' recurring-spend cancellation is unrelated to binding state). Commons hooks also follow the fail-soft pattern; if both fail-soft on the same event, chain liveness is preserved but the orphan-binding invariant + commons-side reconciliation are the catches.

### Peer removal with active bridges

Federation's existing peer-removal flow (`PeerRemovalState` and the EndBlocker prune cursor) must handle the case where a peer is removed while it still has active bridges. Two options were considered:

- **(Block)** Reject peer removal if `bridges_count > 0`. Operators must unbond via `service.MsgUnbondOperator` first, the unbonding period passes, and only then can the peer be removed. Cleanest behavior, but couples peer-removal latency to the longest bridge unbonding period (~14 days).
- **(Cascade)** Auto-dissolve all operators bound to the peer at removal time. Requires a new `serviceKeeper.TerminateOperatorsByController` or similar bulk-dissolve API (out of scope for Phase 0; would be a Phase 0 addition).

**Recommendation**: go with **Block**. Peer removal is rare and gov-authority anyway; making it wait for clean unbonding is appropriate, and avoids adding a bulk-dissolve API that could be misused. Update `MsgRemovePeer` (or the peer removal flow it triggers) to reject with `ErrPeerHasActiveBridges` and a message pointing the gov proposer at the unbond-first workflow. Operators see the gov proposal coming and have time to initiate unbonding before the proposal passes.

**Abandoned-peer escape hatch (required)**: Block creates a stuck state if operators stop responding (network partition, project abandonment, the peer's external network shuts down). The operator's `service.Operator` stays ACTIVE forever and the peer can never be removed. Solution: peer-removal gov proposals can **bundle force-dissolve actions** in the same proposal — the proposal body includes `MsgReportOperator(operator, T1_SLASH, dissolve=true)` for each active bridge, signed by the gov authority, followed by `MsgResolveReport(T1_SLASH)` from the peer's controller (which by Decision 2 is OpsComm by default). One atomic gov vote dissolves stranded operators and removes the peer. Reuses existing primitives, no new authority surface, accountability for the dissolve action is on the gov proposal itself. Document this pattern in the federation spec's "stranded peer recovery" section.

## Phase 6: content-challenge slash path

`x/federation/keeper/msg_server_submit_arbiter_hash.go:91` currently has a TODO for slashing on challenge-upheld quorum. Replace with:

```go
dedupeKey := crypto.MimcHash(challenge.id, challenge.evidence_hash)
reportID, err := k.serviceKeeper.OpenSystemReport(
    ctx,
    k.federationModuleAccountAddr,      // caller, validated via auth keeper
    bridge.address,
    serviceTypeFor(peer),
    cfg.ChallengeDefaultSlashBps,       // proposed slash; controller adjusts within cap at resolve time
    challenge.evidence_uri,
    dedupeKey,                          // idempotency, keyed by challenge.id
)
// Store reportID on challenge for cross-reference
challenge.service_report_id = reportID
k.SetChallenge(ctx, challenge)
```

Report enters PENDING, controller (per-peer or OpsComm) resolves normally, slash flows through the standard tier-1 / escrow / contest path. `ReportTimeoutAction=ESCALATE` ensures a silent controller can't park the slash forever. The controller's `MsgResolveReport` can downgrade or upgrade the final slash amount within `unilateral_slash_cap_bps`; `ChallengeDefaultSlashBps` is just the proposed starting point.

**Idempotency**: the `dedupeKey` includes `challenge.id`, so distinct challenges always produce distinct dedupeKeys (even if two challenges happen to share an `evidence_hash` because they reference the same underlying content). A re-run of `MsgSubmitArbiterHash` for the same challenge (re-org, replay bug) returns the existing `reportID` instead of opening a duplicate. Federation also stores `service_report_id` on the challenge so the linkage is queryable. The Phase 0 per-caller rate limit caps the worst case if some bug starts producing many *unique* challenge.ids in a loop.

Same change applies to `x/federation/keeper/msg_server_escalate_challenge.go` (the other stubbed TODO at line 72).

## Phase 7: query API, CLI, events

| Touchpoint | Action |
| --- | --- |
| `Query/Bridge` in federation | Joins `BridgeBinding` with `service.Operator` (live `bond`, `status`, `unbond_complete_at`). Single-call UX preserved for tooling. |
| `Query/BridgesByPeer` | Same enrichment; returns `[]EnrichedBridge` with both federation-side and service-side fields. |
| `Query/BridgesByOperator` (new) | Lists all bindings for an operator address via the new reverse index. Useful for operator dashboards. |
| CLI: `tx federation unbond-bridge`, `top-up-bridge-stake`, `slash-bridge`, `revoke-bridge` | All removed. Federation CLI README adds a migration guide: "Old `unbond-bridge` is now `tx service unbond-operator --service-type federation-bridge-<proto>`; `top-up-bridge-stake` is now `tx service top-up-bond ...`; slashing is now community-reported via `tx service report-operator ...`." |
| Events | Federation emits `bridge_bound` (on register), `bridge_unbound` (on hook-driven cleanup), `bridge_suspended` / `bridge_resumed` (on underfunded hooks), `federation_hook_failure` (on swallowed panic). x/service emits `operator_registered`, `operator_slashed`, `operator_dissolved`, `system_report_opened`, `system_report_rate_limited`. **Re-registration asymmetry**: first registration for `(address, service_type)` emits both `operator_registered` and `bridge_bound`. Subsequent registrations under the same Operator (Decision 1a, new bindings for other peers) emit only `bridge_bound`. Indexers expecting paired events must handle this; document in the federation spec's event reference. |

## Phase 8: tests and docs

### Tests

| Test | Purpose |
| --- | --- |
| `OpenSystemReport` from federation, valid + invalid caller | Allowlist enforcement via auth-keeper module-name lookup |
| `OpenSystemReport` caller spoofing | Unallowed module attempting `OpenSystemReport` with `callerModuleAddr = <federation module account address>` (raw keeper call); test documents that wiring scope is the primary defense, auth-keeper lookup is auditability, and the `caller_module` event field records the lookup result |
| `OpenSystemReport` repeated `dedupeKey` | Idempotency returns existing report_id within TTL; replay across `report_timeout_blocks` boundary opens a new report |
| `OpenSystemReport` rate limit | 51st report from federation in one epoch rejected with `ErrSystemReportRateLimited`; counter resets next epoch; idempotent re-calls don't count |
| `RegisterOperator` `source` allowlist | Same gate as system reports |
| All four hook handlers (Dissolved / Retired / Underfunded / ReFunded) | Binding cleanup, count decrement, suspend/resume |
| Hook fail-soft | Artificial panic in federation hook doesn't roll back service slash; `federation_hook_failure` event emitted |
| Orphan binding sweep | Force fail-soft path to leave an orphan; `MsgPruneOrphanBindings` cleans it up; orphan-binding invariant fires before cleanup |
| `AfterOperatorReFunded` during UNBONDING | Top-up while UNBONDING does not clear `suspended` flag; binding stays inactive through unbonding completion |
| `MsgUpdatePeerController` | New bridges get new controller; existing bridges keep their original |
| `ReportTimeoutAction=ESCALATE` | Pending report times out, jury case opens (vs DISMISS dropping it) |
| `MsgResyncBridgeCount` | Artificial drift in `bridges_count` is healed by gov call |
| `challenge_default_slash_bps` | Federation challenge → report with correct proposed slash amount; controller adjusts at resolve time within cap; cap enforcement (`> unilateral_slash_cap_bps` rejected at gov time) |
| Per-(address, service_type) uniqueness | Second registration for same protocol reuses existing Operator + writes new binding; controller-mismatch is rejected with the documented "use a different address or transfer the controller" error |
| E2E: full challenge flow | `MsgFederateContent` → `MsgChallengeVerification` → `MsgSubmitArbiterHash` quorum → `OpenSystemReport` → controller `MsgResolveReport(T1_SLASH)` → escrow → release |
| E2E: controller-transfer for stranded bridge | `MsgOpenControllerTransferCase` against a bridge with dead controller Group; jury resolves; new controller takes over |
| E2E: abandoned-peer recovery | Peer with active bridges whose operators never respond; gov bundles `MsgReportOperator(T1_SLASH, dissolve=true)` per bridge plus `MsgRemovePeer` in one proposal; one vote dissolves operators and removes the peer atomically |
| Multi-binding hook enumeration | One operator address has 3 bindings (3 different peers, same protocol); `AfterOperatorDissolved` cleanup must process all 3 bindings via the multi-valued `BindingsByOperator` KeySet, not just the first |

**Test infrastructure**: federation unit tests need a `ServiceKeeperMock` (test double satisfying federation's `ServiceKeeper` interface) since wiring real x/service in unit tests is heavyweight. Add to `x/federation/testutil/` alongside any existing mocks. E2E tests use the real x/service via the chain binary.

### Docs

- `docs/x-federation-spec.md`: rewrite §3.4 (bridge operator lifecycle), §3.7 (session keys, list updated msg types), §3.8 (slashing). Point at x/service spec for everything economic. Add Decision 1a footnote on shared bond and Decision 2 visibility note.
- `docs/x-service-spec.md`: document `ReportTimeoutAction`, `challenge_default_slash_bps`, `OpenSystemReport` + caller allowlist, all four operator-lifecycle hooks. Add consumer-contract section.
- `docs/x-session-spec.md`: update allowed msg types section.
- CLI migration guide for operators (a committed doc, e.g. `docs/federation-bridge-operator-migration.md` — operator-facing guides must not live in temporary handoff docs).
- `docs/x-federation-spec.md` cross-module integration section: document the x/federation ↔ x/service surface (hooks, keeper calls, recovery messages).
- Dev/test chains require reset (pre-mainnet, no upgrade handler). Call this out in the migration PR description.
- **E2E snapshot regeneration**: `test/federation/snapshots/post-setup/` is invalidated by schema changes (BridgeBinding shape, new params, removed messages). Regenerate via `save-setup` skill after Phase 1 lands; Phase 8 PR description must call out that reviewers need to re-snapshot locally before running federation E2E tests.
- **Cross-reference review**: scan `docs/x-federation-review.md`, `docs/x-federation-second-review.md`, `docs/x-federation-third-review.md`, `docs/x-federation-fourth-review.md` for any pre-mainnet hardening items that overlap with the migration (e.g. role-separation between bridge op and verifier, IBC timeout handling, challenge cooldowns). Either fold those items into this migration if cheap, or explicitly defer them with a note pointing at the original review.

## Risks and things to watch

- **Per-(address, service_type) uniqueness (Decision 1a)**: operators running multiple bridges for the same protocol must accept shared slashing fate. Documented in operator guide; controller-mismatch on second registration is a hard error to prevent confusion.
- **Hook fail-soft is load-bearing**: a panic in federation's hook would otherwise brick all bridge slashing chain-wide. Phase 8 tests must include artificial-panic coverage. Pattern enforced via `defer recover` in every hook handler.
- **`OpenSystemReport` caller allowlist drift**: future modules wanting to file system reports must edit x/service's allowlist. This is intentional friction (privilege-escalation gate), not an oversight. Document this in the x/service spec.
- **Hook fan-out shape**: both commons and federation subscribe to `AfterOperatorDissolved` and `AfterOperatorRetired`. `service_adapters.go` multiplexes with documented ordering (federation first, then commons).
- **Per-peer controller bootstrapping**: when a peer is registered without `controller_group`, the resolved OpsComm address is captured at `MsgRegisterBridge` time, not at peer-registration time. Means changing the OpsComm policy address mid-flight (e.g. committee Group migration) doesn't auto-rebind existing bridges. Worth a one-line comment in the code.
- **`MsgUpdatePeerController` semantics**: existing bridges keep their original controller, only new registrations pick up the new one. Documented as a feature, not a bug; transferring existing bridges is what `service.MsgOpenControllerTransferCase` is for. Locked in by Phase 8 test.
- **`max_bridges_per_peer` enforcement**: stays in federation (peer-specific); service is unaware. Federation's `MsgRegisterBridge` checks it before calling `serviceKeeper.RegisterOperator`, and federation's hook decrements `bridges_count` on Dissolved/Retired so freed slots are reusable. Invariant checker (`x/federation/keeper/invariants.go`) catches drift; `MsgResyncBridgeCount` is the recovery path.
- **Per-peer controller visibility**: for partner-org peers, controller Group membership is publicly queryable. Whoever files a report knows exactly which humans decide their fate. Social-dynamics shift, not a privacy bug, but worth flagging in spec.
- **Cross-org partner expectations**: per-peer controller is most valuable for partner-run peers, but the partner org needs a Spark Dream x/commons Group to nominate. For ActivityPub/AT Proto partners with no on-chain presence, the controller still defaults to OpsComm. Bridging that gap (e.g. lightweight proxy Groups for external partners) is out of scope here but worth flagging.
- **Federation module account as reporter**: accountability-asymmetric vs member-filed reports (no slashable identity, no reputation). Controller review + auto-escalate-on-timeout are the effective checks; documented in Decision 5.
- **Caller authorization is keeper-wiring, not allowlist**: the `OpenSystemReport` / `RegisterOperator(source=...)` allowlist + auth-keeper module-name lookup is defense-in-depth and auditability. The primary authorization barrier is "only federation receives a `ServiceKeeper` reference at app wiring." A future depinject refactor that grants `ServiceKeeper` more broadly would erode this; if a new consumer module legitimately needs the keeper, the allowlist must be updated *in lockstep* via a source patch.
- **x/service interface drift**: federation pins x/service's hook interface and public-API signatures. Future x/service additions (new `ReportTimeoutAction` values, new hooks, signature changes) require coordinated federation upgrades. Treat the x/service public API surface used by federation as a versioned contract; flag breaking changes in the x/service spec.
- **Observability/metrics deferred**: the plan covers events but not prometheus counters or dashboards. Off-chain monitoring must subscribe to `federation_hook_failure`, `system_report_rate_limited`, `operator_slashed`, and the invariant-failure events to detect anomalies. Adding native metrics is tracked as follow-up, not blocking this migration.

## Sequencing

Phase 0 (service prerequisites) lands as its own PR, value-neutral for x/service consumers, unblocks federation cleanly. Phases 1–8 then go as a single federation-migration PR. No need to interleave or coordinate beyond that.
