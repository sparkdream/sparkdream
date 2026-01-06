# Security Hardening for Spark Dream

## Immutable Parameters

### Inflation Parameters (x/mint)

**Threat Model:** Malicious governance proposals could alter inflation parameters,
breaking economic assumptions and diluting existing token holders.

**Mitigation:** Mint module authority set to burn address.

```go
// app/app_config.go
{
    Name: minttypes.ModuleName,
    Config: appconfig.WrapAny(&mintmodulev1.Module{
        Authority: "sprkdrm1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqn2ccpe",
    }),
}
```

**Effect:**
- ✅ `inflation_min: 0.02` (2%) - **immutable**
- ✅ `inflation_max: 0.05` (5%) - **immutable**
- ✅ `inflation_rate_change: 0.13` - **immutable**
- ✅ `goal_bonded: 0.67` - **immutable**
- ✅ `mint_denom: "uspark"` - **immutable**

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
- Bond denom (`uspark` → must never change)
- Module authority addresses (except via upgrade)
- Committee size constraints

---

## Verification

### How to Verify Mint Authority

```bash
# Query mint module params
sparkdreamd query mint params

# Check authority (should be burn address)
sparkdreamd query auth module-account mint

# Attempt to update params (should fail)
sparkdreamd tx gov submit-proposal \
  /cosmos.mint.v1beta1.MsgUpdateParams \
  --from alice \
  --authority sprkdrm1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqn2ccpe
# ERROR: unauthorized - authority cannot sign
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
- [ ] Rate limits on council spending (TODO: x/rep)
- [ ] Anonymous challenge mechanism (TODO: x/vote)
- [ ] Jury selection randomness (TODO: x/rep)
- [ ] Founder vesting enforcement (TODO: x/reveal)

---

## Emergency Response

If a critical vulnerability is discovered in the immutability logic:

1. **DO NOT** attempt governance param change (will fail by design)
2. Coordinate emergency chain upgrade via validator communication
3. Prepare upgrade handler with fix
4. Execute coordinated upgrade at agreed height
5. Verify fix post-upgrade

**Emergency Upgrade Authority:**
- 67%+ validator stake agreement required
- Technical Council can propose emergency upgrade
- `x/gov` expedited proposal (if time permits)
- Direct validator coordination (if critical)
