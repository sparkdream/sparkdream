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
- `blocks_per_year` remains governable through the same `MsgExec` path

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

## Committee Size Constraints (x/commons)

**Threat Model:** Governance capture through single-member committees or oversized,
ineffective committees.

**Mitigation:** Hard-coded committee size constraints in genesis bootstrap.

```go
// x/commons/keeper/genesis_bootstrap.go
MinMembers: 2  // Golden share: founder + parent council oversight
MaxMembers: 5  // Small enough to remain nimble
```

**Effect:**
- Prevents single-member capture
- Enforces parent council oversight via "golden share" pattern
- Maintains operational efficiency (not too large)

**Change Procedure:**
- Requires code change + chain upgrade
- Cannot be modified via `MsgUpdateGroupConfig`

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
- Permission checks in `ante/handler.go`
- Hard-coded in `AllowedMessages` enforcement
- Requires module code changes to modify

---

## Future Considerations

### Parameters That SHOULD Remain Mutable (via governance)

✅ **Safe for governance:**
- `community_tax` (x/distribution) - treasury funding split
- Governance voting periods
- Minimum deposit amounts
- Slashing penalties

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
# guardian filter with ErrImmutableField. Only blocks_per_year passes.
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
