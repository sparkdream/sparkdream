# Security Hardening for Spark Dream

## Immutable Parameters

### Inflation Parameters (x/mint)

**Threat Model:** Malicious governance proposals could alter inflation parameters,
breaking economic assumptions and diluting existing token holders.

**Mitigation:** Mint module authority routed through `x/guardian`.

```go
// app/app_config.go
{
    Name: minttypes.ModuleName,
    Config: appconfig.WrapAny(&mintmodulev1.Module{
        Authority: guardianAuthority, // the x/guardian module account
    }),
}
```

x/gov never signs `mint.MsgUpdateParams` directly. A proposal carries a
`guardian.MsgExec` whose inner message is the mint update; on execution
guardian diffs the proposed params against the current ones and rejects any
change to an immutable field with `ErrImmutableField` before forwarding to
x/mint. The filter is compiled code ([x/guardian/keeper/msg_server.go](../x/guardian/keeper/msg_server.go)),
so only a chain upgrade can change what is immutable. See
[x/guardian/README.md](../x/guardian/README.md) for the full filter table.

**Effect:**
- ✅ `inflation_min: 0.02` (2%) - **immutable**
- ✅ `inflation_max: 0.05` (5%) - **immutable**
- ✅ `inflation_rate_change: 0.13` - **immutable**
- ✅ `goal_bonded: 0.67` - **immutable**
- ✅ `mint_denom` (the x/identity bond denom) - **immutable**
- ✅ `blocks_per_year` - **bounded**: issuance per block is
  `annual_provisions / blocks_per_year`, so a free divisor would defeat every
  locked field above (1 mints a year of inflation per block). It stays
  tunable so issuance can follow a real block-time change, but only inside
  `[3_153_600, 63_072_000]` (10 s to 0.5 s blocks) and by at most ±50% of the
  current value per proposal

**Change Procedure:**
1. Community discussion on forum
2. Technical proposal with economic analysis
3. Validator coordination for chain upgrade
4. Upgrade handler modifies genesis params
5. Network upgrades at specified height

**Why This Matters:**
- 100M SPARK supply cap depends on predictable inflation
- Founder vesting schedules assume stable inflation curve
- Stakers expect consistent yield economics
- No single entity (including governance) can unilaterally change monetary policy

---

### Bounded SDK Parameters (x/guardian)

**Threat Model:** A hostile or captured gov majority cannot touch the locked
mint fields, but could reach the same outcomes through neighbouring params:
disable every native send, switch off downtime enforcement, let validators
unbond instantly, halt the chain through the consensus key-type list, make
proposals free, or freeze gov for decades.

**Mitigation:** guardian is the authority of auth, bank, mint, staking,
distribution, gov, slashing and consensus. Every routed update passes a
compiled filter (changing a bound needs a chain upgrade):

| Module | Bound |
|---|---|
| bank | `default_send_enabled` must stay `true` (the native denoms read the default); `MsgSetSendEnabled` cannot disable or reset the native bond/dream denoms |
| staking | `bond_denom` locked; `unbonding_time ≥ 7 d`; `max_validators ≥ 20` |
| slashing | double-sign/downtime fractions above floors; `min_signed_per_window ≥ 0.05`; `downtime_jail_duration ≥ 10 min`; `signed_blocks_window` in `[100, 1M]` |
| consensus | `block.max_bytes ≥ 200 kB`; `block.max_gas` finite and `≥ 1M` (`-1` refused); evidence age floors and `evidence.max_age_duration ≤ unbonding_time`; `validator.pub_key_types` must include `ed25519` |
| gov | `voting_period` in `[6 h, 28 d]`; `expedited_voting_period` in `[1 h, 28 d]`; quorum/threshold/veto floors; `min_deposit` and `expedited_min_deposit` must hold a positive bond-denom amount |
| distribution | `community_tax` in `[0.05, 0.25]`; `MsgCommunityPoolSpend` hard-rejected |
| auth | gas-cost floors; `tx_sig_limit ≥ 7` (native multisig) |

Gov still holds direct, unguarded authority over x/upgrade, x/evidence, IBC
core, IBC transfer, the ICA host/controller params (including the ICA host
`allow_messages` list, which can only narrow the compiled
`app.ICAHostAllowedMsgs`) and legacy x/params proposals. Upgrade is inherently
all-powerful; the others are listed in
[x/guardian/README.md](../x/guardian/README.md#outside-guardian).

---

## Committee Size Constraints (x/commons)

**Threat Model:** Governance capture through single-member committees or oversized,
ineffective committees.

**Mitigation:** Compiled committee size bounds, enforced on every path.

```go
// x/commons/keeper/group_validation.go
CommitteeMinMembers uint64 = 2  // a committee is never one key
CommitteeMaxMembers uint64 = 5  // small enough to remain nimble
```

- The six standing committees (Commons / Technical / Ecosystem Operations
  and Governance Committees) keep `[2,5]` whatever the signer:
  `MsgUpdateGroupConfig` and a gov `MsgRegisterGroup` of a committee name
  refuse anything outside it, x/gov included.
- `min_members` / `max_members` of any other group can only be changed by
  x/gov; a parent council's `MsgUpdateGroupConfig` that touches them is
  refused.
- A council-registered group needs `min_members >= 2`, and only x/gov may
  register a reserved canonical name, so `MsgDeleteGroup` plus
  `MsgRegisterGroup` cannot recreate a committee with looser bounds.
- **One-way ratchet.** Genesis seeds each committee with the founder alone
  (below the minimum, an *unstarted* committee). `MsgUpdateGroupMembers`
  checks the floor on the projected count, so the first add is allowed; once
  a committee has two members it can never shrink below two.
- **Membership changes need a strict majority.** Committees decide with a
  percentage 0.5 policy, so at two members one vote passes ordinary
  proposals. A proposal that changes membership (`MsgUpdateGroupMembers`,
  `MsgRenewGroup`) additionally needs more than half of the body's total
  weight: one member of a two-member committee cannot replace the other, or
  pack the council the committee elects.
- **No self-election.** No committee is its own electoral authority. The
  Commons Operations Committee is elected by the Commons Council; the
  governance committees elect their councils, never themselves.

**Effect:**
- Prevents single-member capture
- Enforces parent council oversight via "golden share" pattern
- Maintains operational efficiency (not too large)

**Change Procedure:**
- Requires code change + chain upgrade
- Cannot be modified via `MsgUpdateGroupConfig`, by a council or by x/gov

---

## Three Pillars Hierarchy (x/commons)

**Threat Model:** Rogue committees bypassing council oversight, or councils acting
without checks and balances.

**Mitigation:** Hierarchical permission enforcement + veto powers.

```
x/gov (Root Authority)
    │
    ├── Commons Council ←────────┐
    │   ├── Commons Ops           │ Electoral
    │   └── Commons Gov           │ Delegation
    │                             │
    ├── Technical Council         │ Supervisory
    │   ├── Tech Ops              │ Board
    │   └── Tech Gov ─────────────┤ Oversight
    │                             │
    └── Ecosystem Council         │
        ├── Eco Ops               │
        └── Eco Gov ──────────────┘
```

**Enforced Rules:**
1. Committees cannot execute without parent council delegation
2. Any council can veto another's decisions (cross-council checks)
3. Supervisory Board can fire governance committees (HR)
4. `x/gov` has ultimate backstop authority

**Cannot Be Bypassed:**
- Permission checks in the x/commons msg server: each policy's
  `AllowedMessages` is checked in `msg_server_proposals.go`, and
  `ForbiddenMessages` is checked when a policy's permissions are set. The
  commons ante decorator
  ([x/commons/ante/group_policy.go](../x/commons/ante/group_policy.go)) only
  enforces the minimum proposal fee; it is not a permission check
- Requires module code changes to modify

---

## Ante-Only Rules and In-Chain Executors

A few rules run only in the ante handler: the tombstoned-signer backstop
(x/rep), the commons minimum proposal fee (`ProposalFeeDecorator`), the gnovm
rule that VM messages are never mixed with other messages, and the session /
shield fee handling. Anything that dispatches messages through the router from
inside the chain skips the ante, so each executor keeps off the affected
types (`x/common/types.AnteOnlyRuleMsgTypeURLs`):

| Executor | Containment |
|---|---|
| x/session `MsgExecSession`, ScheduledOneshot Exec | The audited allowlist; ante-only types are in `ForbiddenAllowlistMsgs`, re-checked at fire time |
| x/vault proposals | Denylist refuses commons `MsgSubmitProposal`; the gnovm no-mix rule is applied to each proposal |
| ICA host | Compiled `app.ICAHostAllowedMsgs` (bank send, staking delegate/undelegate/redelegate, distribution withdraw / set withdraw address, IBC transfer); the gov-controlled `allow_messages` param can only narrow it |
| x/guardian `MsgExec` | Only the SDK param messages in its filter switch |

## Fee and Block Limits

- **Fee-paying decorators enforce minimum gas prices.** `ShieldGasDecorator`
  and `SessionFeeDecorator` pay fees on someone's behalf and set the shared
  typed fee-paid context key (`x/common/types.ContextKeyFeePaid`), which makes
  `SkipIfFeePaidDecorator` skip the SDK `DeductFeeDecorator`. Both apply the
  node's CheckTx minimum-gas-price rule themselves (and the session decorator
  sets the fee priority), so neither path admits zero-fee txs.
- **Finite block gas.** Network geneses set `consensus.params.block.max_gas =
  100000000` (pinned in each network `config.yml`); x/guardian refuses `-1`.
- **Relay refunds are capped** at `gas_limit × 0.1` per fee denom
  (`relayrefund.MaxRefundGasPrice`), so a relayer cannot buy priority for free.

## Module Accounts and Builds

- **Blocked module accounts.** No `MsgSend`, `MultiSend` or IBC transfer may
  credit fee_collector, mint, the bonded pools, distribution (the community
  pool is funded through `MsgFundCommunityPool`), split, commons, rep, forum,
  collect, blog, name, futarchy, season, session, federation, guardian,
  service, vault, IBC transfer or ICA. x/session also refuses them as a pull,
  claim or oneshot recipient (bank `SendCoins` does not check the list). Only
  gov (SDK convention), the accounts topped up by plain sends (shield,
  ecosystem), gnovm (realm coin flows) and artifact (the Harberger tax
  grantee) stay open; `TestBlockedModuleAccounts` pins the list, so a new
  module account is blocked unless it is allowed there with a reason. Bonded
  role and operator reward pools are bank sub-addresses, not module accounts,
  so councils can still top them up with a plain send.
- **Minting permissions.** Only mint, IBC transfer, futarchy and gnovm hold
  `Minter` (`app/app_config.go`). The commons account holds no permission:
  commons never creates SPARK, and its market subsidies are paid from SPARK
  already sent to it. x/name burns its registration fees (`Burner` only).
- **Network builds.** Untagged builds compile the test parameters
  (`*_testparams.go`) and skip x/shield proof verification when no key is
  registered. `checkNetworkBuild` refuses the wrong build on the three
  canonical chain-ids. The daemon also refuses to start an untagged build
  (`CheckTestBuildStart`) on any chain-id other than the local `sparkdream`
  chain unless `SPARKDREAM_ALLOW_TEST_BUILD=1` is set; the multichain and
  replay e2e launchers set it. Sister chains must ship a
  `-tags mainnet|testnet|devnet` binary.

---

## Future Considerations

### Parameters That SHOULD Remain Mutable (via governance)

✅ **Safe for governance:**
- `community_tax` (x/distribution) - treasury funding split
- Governance voting periods (inside guardian's `[6 h, 28 d]` band)
- Minimum deposit amounts (must stay positive in the bond denom)
- Slashing penalties (above guardian's floors)
- `blocks_per_year` (inside guardian's range, ±50% per proposal)

✅ **Safe for councils:**
- `MaxSpendPerEpoch` per council
- Committee membership
- Project budgets (DREAM only, not SPARK inflation)

❌ **Never mutable via governance:**
- Inflation parameters (x/mint)
- Bond denom (sealed at genesis by x/identity → must never change)
- Module authority addresses (except via upgrade)
- Committee size constraints

---

## Verification

### How to Verify Mint Authority

```bash
# Query mint module params
sparkdreamd query mint params

# The guardian module account is the mint authority
sparkdreamd query auth module-account guardian

# A gov proposal that submits mint.MsgUpdateParams directly fails at
# execution: its authority must equal the guardian address, which gov
# cannot sign for. A proposal wrapping it in guardian.MsgExec with a
# changed inflation_min/inflation_max/goal_bonded/mint_denom fails the
# guardian filter with ErrImmutableField. Only a blocks_per_year change
# inside the compiled range and within ±50% of the current value passes.
```

### How to Verify Committee Constraints

```bash
# Check committee config
sparkdreamd query commons group "Technical Operations Committee"

# Verify min/max members in extended group
# MinMembers: 2, MaxMembers: 5
```

---

## Security Audit Checklist

- [x] Mint parameters immutable via governance
- [x] Committee size constraints enforced
- [x] Three Pillars hierarchy cannot be bypassed
- [x] Rate limits on council spending (x/commons per-epoch spend gates)
- [x] Anonymous challenge mechanism (x/shield)
- [x] Jury selection randomness (x/rep)
- [x] ICA host limited to a compiled message allowlist
- [x] Ante-only rules kept off in-chain executors (session, vault, ICA)
- [x] Session and shield fee paths enforce minimum gas prices
- [ ] Founder vesting enforcement (TODO: x/reveal)

---

## Emergency Response

If a critical vulnerability is discovered in the immutability logic:

1. **DO NOT** attempt a governance param change on an immutable field (the guardian filter rejects it by design)
2. Coordinate emergency chain upgrade via validator communication
3. Prepare upgrade handler with fix
4. Execute coordinated upgrade at agreed height
5. Verify fix post-upgrade

**Emergency Upgrade Authority:**
- 67%+ validator stake agreement required
- Technical Council can propose emergency upgrade
- `x/gov` expedited proposal (if time permits)
- Direct validator coordination (if critical)
