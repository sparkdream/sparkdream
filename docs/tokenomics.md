# Spark Dream Tokenomics

## Dual Token Model

Spark Dream uses two tokens with distinct purposes:

| Property | SPARK | DREAM |
|----------|-------|-------|
| Type | Native chain token | Internal coordination token |
| Supply | 100M genesis, 2-5% inflation | Uncapped, productivity-backed |
| Transferable | Yes, freely | Limited (tips, gifts, bounties) |
| External trading | Yes | No |
| IBC enabled | Yes | No |
| Primary use | Gas, governance, staking | Initiative rewards, conviction, reputation |
| Value anchor | Market price | Contribution-backed |

## SPARK Token

### Genesis Allocation

```
Total: 100,000,000 SPARK (100M)

┌─────────────────────┬──────────────┬─────────┐
│ Category            │ Amount       │ %       │
├─────────────────────┼──────────────┼─────────┤
│ Founders            │  5,000,000   │  5%     │
│ Community Pool      │ 95,000,000   │ 95%     │
├─────────────────────┼──────────────┼─────────┤
│ TOTAL               │ 100,000,000  │ 100%    │
└─────────────────────┴──────────────┴─────────┘

Community Pool is distributed to councils via x/split on chain start:
├── Commons Council (50%):    47,500,000 SPARK
├── Technical Council (30%):  28,500,000 SPARK
└── Ecosystem Council (20%):  19,000,000 SPARK
```

### Founder Allocation Details

```
Total Founders: 9 (1 Tier 1, 1 Tier 2, 5 Tier 3, 2 Tier 4)
Total Allocation: 5,000,000 SPARK (5%)

┌──────────────────────┬───────┬────────────┬───────────┐
│ Tier                 │ Count │ Per Person │ Total     │
├──────────────────────┼───────┼────────────┼───────────┤
│ Tier 1 (Lead Vocal)  │   1   │ 1,250,000  │ 1,250,000 │
│ Tier 2 (Vocal)       │   1   │   750,000  │   750,000 │
│ Tier 3 (Public)      │   5   │   450,000  │ 2,250,000 │
│ Tier 4 (Anon)        │   2   │   375,000  │   750,000 │
├──────────────────────┼───────┼────────────┼───────────┤
│ TOTAL                │   9   │            │ 5,000,000 │
└──────────────────────┴───────┴────────────┴───────────┘

Tier Descriptions:
- Tier 1: First vocal public founder who recruited the others
- Tier 2: Second vocal public founder
- Tier 3: Public founders including primary technical contributor
- Tier 4: Anonymous supporters
```

### Inflation Model

```
Annual Inflation: 2% - 5% (dynamic based on bonding ratio)

Distribution of Inflation + Fees:
├── Validators/Delegators: 85%
└── Community Pool: 15%

Community Pool → x/split:
├── Commons Council:    50%
├── Technical Council:  30%
└── Ecosystem Council:  20%
```

**SECURITY: Immutable Inflation Parameters**

The inflation parameters (`inflation_min`, `inflation_max`, `inflation_rate_change`,
`goal_bonded`) are **immutable by design**. They cannot be changed via governance
proposals (`MsgUpdateParams`).

- ✅ **Protected:** Inflation parameters are set at genesis
- ❌ **Blocked:** `x/gov` cannot modify via parameter change proposals
- ✅ **Only changeable:** Through coordinated chain upgrades

This prevents:
- Malicious governance attacks inflating supply
- Sudden monetary policy changes
- Erosion of economic guarantees

The mint module authority is set to an impossible address (`sprkdrm1qqqqqq...` burn
address) that has no private key, making parameter updates cryptographically impossible
without a chain upgrade.

### SPARK Utility

1. **Gas Fees**: Pay for transaction execution
2. **Governance**: Vote on x/gov proposals
3. **Staking**: Delegate to validators for rewards
4. **Treasury**: Fund external expenses (audits, infrastructure)

## DREAM Token

### Supply Model

DREAM has no maximum supply. It is minted based on productivity and burned through various sinks, creating a self-regulating economy.

```
Supply Dynamics:
├── Minting tied to completed work
├── Burning from penalties and decay
├── Target: slight net inflation during growth
├── Long-term: equilibrium as ecosystem matures
```

### Minting Sources

```
1. INITIATIVE COMPLETION (Primary)
   ├── Completer reward: Budget × 90% × ReputationMultiplier
   └── Treasury share: Budget × 10%

2. STAKING REWARDS
   ├── APY: 10% annual
   ├── Calculation: Stake × APY × (Duration / Year)
   └── Paid on initiative completion to all stakers

3. COMMITTEE WORK (Interim Compensation)
   ├── Simple: 50 DREAM
   ├── Standard: 150 DREAM
   ├── Complex: 400 DREAM
   ├── Expert: 1000 DREAM
   ├── Solo bonus: +50% during bootstrap
   └── ADJUDICATION: 0 DREAM (civic duty, no compensation)

   Note: Escalated adjudication decisions (from inconclusive jury
   verdicts) are intentionally uncompensated to ensure committee
   members make these critical decisions on principle, not profit.
   See x-rep-spec.md "Committee Incentive Structure" for details.

4. PARTICIPATION REWARDS
   ├── Voting in governance
   ├── Jury duty completion
   └── Season-end bonuses

5. RETROACTIVE PUBLIC GOODS FUNDING
   ├── Budget: 50,000 DREAM per season (governance-adjustable)
   ├── Distributed to top nominations by conviction
   ├── Max recipients: 20 per season
   ├── Min conviction threshold: 50.0
   └── Triggered during season transition (PhaseRetroRewards)
```

### Burning Sources

```
1. SLASHING PENALTIES
   ├── Minor: 5% of balance
   ├── Moderate: 15% of balance
   ├── Severe: 30% of balance
   └── Zeroing: 100% of balance

2. FAILED CHALLENGES
   └── Challenger stake burned if challenge rejected

3. FAILED INVITATIONS
   └── Inviter stake burned if invitee misbehaves

4. UNSTAKED DECAY
   ├── Rate: 1% per epoch
   ├── Applied only to unstaked DREAM
   └── Staked DREAM does not decay

5. TRANSFER TAX
   ├── Rate: 3% of transfer amount
   └── Applied to all DREAM transfers

6. COSMETIC PURCHASES (Optional)
   └── Titles, badges, profile items
```

### Transfer Rules

DREAM has limited transferability to maintain its "earned not bought" nature.

```
ALLOWED TRANSFERS:

1. TIPS
   ├── Purpose: Thank/reward other members
   ├── Max amount: 100 DREAM per tip
   ├── Max frequency: 10 tips per epoch
   ├── Recipients: Any member
   └── Tax: 3% burned

2. GIFTS
   ├── Purpose: Help onboard invitees
   ├── Max amount: 500 DREAM per gift
   ├── Recipients: Only your invitees
   └── Tax: 3% burned

3. BOUNTIES
   ├── Purpose: Fund specific work
   ├── Mechanism: Escrowed until initiative completion
   ├── Tied to: Specific initiative ID
   └── Tax: 3% burned on release

PROHIBITED:
├── External market trading
├── IBC transfers
├── Transfers to non-members
├── Bulk transfers (>500 DREAM without escrow)
└── Buying DREAM with SPARK
```

### DREAM Decay Mechanism

```go
// Applied each epoch in EndBlocker
func ApplyDREAMDecay(member) {
    unstaked := member.Balance - member.StakedDREAM
    
    if unstaked > 0 {
        decay := unstaked * DecayRate  // 1% per epoch
        member.Balance -= decay
        BurnDREAM(decay)
    }
    
    // Staked DREAM does not decay
}
```

Purpose of decay:
- Encourages active participation
- Penalizes hoarding without contribution
- Creates burn pressure to balance minting
- Incentivizes staking on projects/initiatives

### Genesis DREAM Allocation

```
Founder DREAM (one-time bootstrap):

┌──────────────────────┬───────┬────────────┬─────────┐
│ Tier                 │ Count │ Per Person │ Total   │
├──────────────────────┼───────┼────────────┼─────────┤
│ Tier 1 (Lead Vocal)  │   1   │    5,000   │   5,000 │
│ Tier 2 (Vocal)       │   1   │    3,500   │   3,500 │
│ Tier 3 (Public)      │   5   │    2,500   │  12,500 │
│ Tier 4 (Anon)        │   2   │    2,000   │   4,000 │
├──────────────────────┼───────┼────────────┼─────────┤
│ TOTAL                │   9   │            │  25,000 │
└──────────────────────┴───────┴────────────┴─────────┘

This is the ONLY pre-minted DREAM.
All future DREAM must be earned through contribution.
```

### Founder Invitation Credits

```
┌──────────────────────┬───────┬────────────┬─────────┐
│ Tier                 │ Count │ Per Person │ Total   │
├──────────────────────┼───────┼────────────┼─────────┤
│ Tier 1 (Lead Vocal)  │   1   │     10     │    10   │
│ Tier 2 (Vocal)       │   1   │      7     │     7   │
│ Tier 3 (Public)      │   5   │      5     │    25   │
│ Tier 4 (Anon)        │   2   │      3     │     6   │
├──────────────────────┼───────┼────────────┼─────────┤
│ TOTAL                │   9   │            │    48   │
└──────────────────────┴───────┴────────────┴─────────┘

Credits allow inviting new members.
Replenished as invitees become Established.
```

## Initiative Reward Economics

### Budget to Reward Calculation

```
Initiative Budget: B (DREAM)
Completer Reputation: R (in relevant tags)
Tier Reputation Cap: C

Reputation Multiplier: M = max(0.1, min(R/C, 1.0))

Completer Reward = B × 0.90 × M
Treasury Reward = B × 0.10

Example:
├── Budget: 500 DREAM
├── Tier: Standard (cap = 100)
├── Completer Rep: 80
├── Multiplier: 80/100 = 0.8
├── Completer gets: 500 × 0.90 × 0.8 = 360 DREAM
└── Treasury gets: 500 × 0.10 = 50 DREAM
```

### Staker Reward Calculation

```
Stake Amount: S (DREAM)
Stake Duration: D (epochs)
Annual Yield: Y (10% = 0.10)
Epochs Per Year: E (365)

Staker Reward = S × Y × (D / E)

Example:
├── Stake: 1000 DREAM
├── Duration: 14 epochs (2 weeks)
├── APY: 10%
├── Reward: 1000 × 0.10 × (14/365) = 3.84 DREAM
```

### Conviction Calculation

```
Stake Amount: S (DREAM)
Time Staked: T (epochs)
Half-Life: H (7 epochs)
Staker Reputation: R
Reputation Weight: W = sqrt(R) / 10 + 1 (1.0 to ~2.0 range)

Raw Conviction = S × (1 - 0.5^(T/H))
Weighted Conviction = sqrt(Raw) × W

Example after 7 epochs:
├── Stake: 1000 DREAM
├── Raw: 1000 × (1 - 0.5) = 500
├── After sqrt: sqrt(500) = 22.4
├── With rep weight 1.5: 22.4 × 1.5 = 33.6 conviction units
```

## Economic Sustainability

### Treasury Income (SPARK)

```
Annual SPARK Income Estimate (Year 1):

Inflation (at 4% average):
├── Total supply: 100M
├── New SPARK: 4M
├── To community pool (15%): 600K
├── To councils: 600K SPARK/year

Transaction Fees:
├── Assume 1M transactions/year
├── Average fee: 0.1 SPARK
├── Total fees: 100K SPARK
├── To community pool (15%): 15K
├── To councils: 15K SPARK/year

Total to councils: ~615K SPARK/year
├── Commons (50%): ~307K SPARK
├── Technical (30%): ~185K SPARK
└── Ecosystem (20%): ~123K SPARK
```

### DREAM Equilibrium

```
Target: Minting ≈ Burning (slight inflation during growth)

Season 1 Projection (conservative):
├── Initiatives completed: 200
├── Average budget: 300 DREAM
├── Initiative minting: 60,000 DREAM
├── Staking rewards: 10,000 DREAM
├── Committee work: 5,000 DREAM
├── Retroactive public goods: 50,000 DREAM
├── Total minted: ~125,000 DREAM

Burns:
├── Decay (1%/epoch avg): ~20,000 DREAM
├── Failed challenges: ~2,000 DREAM
├── Transfer tax: ~1,000 DREAM
├── Slashing: ~2,000 DREAM
├── Total burned: ~25,000 DREAM

Net: +100,000 DREAM circulating
(Acceptable during growth phase — retro PGF rewards incentivize unplanned contributions)
```

### Long-term Sustainability

```
Growth Phase (Years 1-3):
├── Net DREAM inflation: 5-10% per season
├── Treasury income: Growing with activity
├── Purpose: Bootstrap ecosystem

Maturity Phase (Years 3+):
├── Target: Burn rate ≈ mint rate
├── Decay becomes primary burn
├── Healthy equilibrium

Key Metrics to Monitor:
├── DREAM velocity (transfers per DREAM)
├── Stake ratio (staked / total)
├── Burn rate vs mint rate
├── Initiative completion rate
└── Member retention
```

## Anti-Gaming Mechanisms

### Preventing Fake Initiatives

```
1. Project budgets approved by Operations
   └── Can't create budget from nothing

2. External conviction required (50%)
   └── Need non-affiliated stakers

3. Reputation gates rewards
   └── Low rep = low multiplier

4. Challenge period
   └── Community can dispute

5. Human verification
   └── No auto-completion
```

### Preventing Sybil Attacks

```
1. Invitation cost (stake required)
2. Time to build trust (seasons)
3. Invitation chain tracking
4. Cascading inviter penalties
5. Behavioral correlation detection
6. Quadratic influence (diminishing returns)
```

### Gaming Analysis

```
Attack: Create fake initiative, complete yourself

Requirements:
├── Project with approved budget (need Ops approval)
├── External conviction (need non-affiliated stakers)
├── Pass challenge period (human review possible)

Even if successful:
├── Low reputation = low multiplier
├── Budget comes from existing pool
├── Multiple attempts trigger detection
└── Inviter at risk if new account

Conclusion: Not profitable at scale
```

## Parameter Governance

### Adjustable Parameters

```
Economic:
├── StakingAPY: Currently 10%
├── UnstakedDecayRate: Currently 1%/epoch
├── TransferTaxRate: Currently 3%
├── CompleterShare: Currently 90%
├── TreasuryShare: Currently 10%

Initiative Tiers:
├── MinReputation per tier
├── MaxBudget per tier
├── ReputationCap per tier
├── RewardMultiplier per tier

Conviction:
├── HalfLifeEpochs: Currently 7
├── ExternalConvictionRatio: Currently 50%

Challenges:
├── MinChallengeStake: Currently 50 DREAM
├── ChallengerRewardRate: Currently 20%
├── JurySize: Currently 5
```

### Change Process

```
Parameter Tweaks:
├── Proposer: Operations Committee
├── Approval: 60% committee vote
├── Timeline: 1 week voting
├── Veto: Council can veto

Structural Changes:
├── Proposer: Any council member
├── Approval: 66% council vote
├── Timeline: 2 weeks voting
├── Veto: Cross-council veto period

Economic Changes (major):
├── Proposer: Council proposal
├── Approval: 75% cross-council
├── Timeline: 1 month
├── Implementation: Phased rollout
```
