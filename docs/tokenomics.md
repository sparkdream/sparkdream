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
   └── Treasury share: Budget × 10% (recycled, not minted — see Treasury Recycling)

2. SEASONAL STAKING REWARD POOL
   ├── Pool: 25,000 DREAM minted at season start
   ├── Distributed pro-rata each epoch: pool / remaining_epochs
   ├── Effective APY: pool / total_staked (self-adjusting)
   ├── More staked → lower yield; less staked → higher yield
   └── Pool exhaustion = zero rewards until next season

3. COMMITTEE WORK (Interim Compensation)
   ├── Simple: 50 DREAM
   ├── Standard: 150 DREAM
   ├── Complex: 400 DREAM
   ├── Expert: 1000 DREAM
   ├── Solo bonus: +50% during bootstrap
   ├── ADJUDICATION: 0 DREAM (civic duty, no compensation)
   └── Funded from treasury first; mint only if treasury is empty

   Note: Escalated adjudication decisions (from inconclusive jury
   verdicts) are intentionally uncompensated to ensure committee
   members make these critical decisions on principle, not profit.
   See x-rep-spec.md "Committee Incentive Structure" for details.

4. PARTICIPATION REWARDS
   ├── Voting in governance
   ├── Jury duty completion
   └── Season-end bonuses

5. RETROACTIVE PUBLIC GOODS FUNDING
   ├── Budget: 25% of initiative minting that season
   │   ├── Floor: 10,000 DREAM (minted if treasury + ratio < floor)
   │   └── Ceiling: 75,000 DREAM
   ├── Funded from treasury first; mint remainder
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
   ├── Rate: 0.2% per epoch (~73% annualized)
   └── Applied to unstaked DREAM only

5. STAKED DECAY (NEW)
   ├── Rate: 0.05% per epoch (~18% annualized)
   ├── Applied to all staked DREAM
   ├── Active stakers outpace decay with seasonal rewards
   └── Abandoned/idle stakes erode over time

6. NEW MEMBER GRACE PERIOD
   ├── Duration: 30 epochs (~1 month)
   └── Both unstaked and staked decay waived for new members

7. TRANSFER TAX
   ├── Rate: 3% of transfer amount
   └── Applied to all DREAM transfers

8. TREASURY OVERFLOW BURN
   ├── Trigger: Treasury balance > MaxTreasuryBalance (100,000 DREAM)
   └── Excess burned each epoch in EndBlocker

9. COSMETIC PURCHASES (Optional)
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
// Applied lazily via GetMember/GetBalance (not bulk EndBlocker)
func ApplyDREAMDecay(member) {
    epochsElapsed := currentEpoch - member.LastDecayEpoch

    // Grace period: new members exempt for first 30 epochs (~1 month)
    if currentEpoch - member.JoinedEpoch < NewMemberDecayGraceEpochs {
        return
    }

    // Unstaked decay: 0.2% per epoch
    unstaked := member.Balance - member.StakedDREAM
    if unstaked > 0 {
        unstakedDecay := unstaked * UnstakedDecayRate * epochsElapsed
        member.Balance -= unstakedDecay
        BurnDREAM(unstakedDecay)
    }
    
    // Staked decay: 0.05% per epoch (~18% annualized)
    if member.StakedDREAM > 0 {
        stakedDecay := member.StakedDREAM * StakedDecayRate * epochsElapsed
        member.StakedDREAM -= stakedDecay
        BurnDREAM(stakedDecay)
    }
}
```

Purpose of decay:
- **Unstaked decay** (0.2%/epoch): Gentle inactivity tax — nudges members to stake without
  forcing panic-staking that would dilute conviction signals. 4:1 ratio vs staked decay
  is enough to incentivize staking without distorting it
- **Staked decay** (0.05%/epoch): Prevents infinite compounding — active stakers earning
  seasonal rewards easily outpace it, but idle/abandoned stakes erode over time
- **Grace period** (30 epochs): Gives new members time to earn and stake DREAM before
  decay applies, preventing onboarding friction
- Creates real system-wide burn pressure regardless of staking behavior

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

## Treasury Recycling

The 10% treasury share from every completed initiative flows into the x/rep module treasury.
This DREAM is **recycled** (not minted fresh) to fund operational costs before minting new tokens.

### Treasury Outflow Priority

```
1. INTERIM COMPENSATION (first priority)
   ├── Committee work (jury duty, project approval, etc.) paid from treasury
   ├── If treasury has sufficient balance: pay from treasury, zero new minting
   ├── If treasury is empty: mint fresh DREAM (fallback)
   └── Reduces inflationary pressure from committee operations

2. RETROACTIVE PUBLIC GOODS FUNDING (second priority)
   ├── Season-end retro PGF budget drawn from treasury first
   ├── Remainder (if budget > treasury) is minted
   └── During high-activity seasons, treasury may cover the full retro budget

3. OVERFLOW BURN (enforced each epoch)
   ├── Threshold: MaxTreasuryBalance = 100,000 DREAM
   ├── If balance > threshold: excess burned in EndBlocker
   └── Prevents unbounded treasury accumulation
```

### Why This Matters

Without recycling, the 10% treasury share is a dead sink — DREAM enters the treasury
and never leaves, reducing circulating supply while the system mints fresh DREAM for
interims and retro PGF elsewhere. Treasury recycling turns this into a closed loop:

```
Initiative completion → 10% to treasury → funds interims + retro PGF
                                        → excess burned
                                        → only mint if treasury empty
```

This reduces net minting by the amount the treasury can cover, creating a natural
dampening effect on inflation during high-activity periods.

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
Seasonal Pool: P (25,000 DREAM per season)
Epochs Per Season: N (150)
Epoch Reward Slice: R = P / N (~166.7 DREAM per epoch)
Staker's Stake: S (DREAM)
Total Staked (all stakers): T (DREAM)

Staker Epoch Reward = R × (S / T)
Effective APY = (P / T) annualized

Example (Season 1, total staked = 200,000 DREAM):
├── Seasonal pool: 25,000 DREAM
├── Epoch slice: 25,000 / 150 = 166.7 DREAM
├── Your stake: 1,000 DREAM
├── Your share: 1,000 / 200,000 = 0.5%
├── Your epoch reward: 166.7 × 0.005 = 0.83 DREAM
├── Your season reward: 0.83 × 150 = 125 DREAM
├── Effective APY: 125 / 1,000 × (365/150) = ~30%

Example (Season 3, total staked = 500,000 DREAM):
├── Same pool: 25,000 DREAM
├── Your stake: 1,000 DREAM
├── Your share: 1,000 / 500,000 = 0.2%
├── Your season reward: 25,000 × 0.002 = 50 DREAM
├── Effective APY: 50 / 1,000 × (365/150) = ~12%

Note: As more DREAM enters the system and gets staked, effective
APY naturally decreases. This prevents runaway compounding while
still rewarding early and active participants.
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

Season 1 Projection (conservative, 200 initiatives, avg 300 DREAM):

GROSS MINTING:
├── Initiative completion (completer share): 54,000 DREAM
├── Initiative treasury share (recycled):     6,000 DREAM → treasury
├── Seasonal staking pool:                   25,000 DREAM (capped)
├── Committee work:                           5,000 DREAM
├── Retroactive public goods:                15,000 DREAM (25% of 60K)
├── Gross total:                            ~99,000 DREAM

TREASURY RECYCLING OFFSET:
├── Treasury receives: 6,000 DREAM (initiative shares)
├── Treasury funds interims: -5,000 DREAM (covers committee work)
├── Treasury funds retro PGF: -1,000 DREAM (partial cover)
├── Net treasury change: 0 DREAM
├── Net minting avoided: -6,000 DREAM (interims + partial retro funded by treasury)
├── Actual new DREAM minted: ~93,000 DREAM

BURNS:
├── Unstaked decay (0.2%/epoch on ~50K avg):  ~3,000 DREAM
├── Staked decay (0.05%/epoch on ~200K):     ~15,000 DREAM
├── Failed challenges:                        ~2,000 DREAM
├── Transfer tax:                             ~1,000 DREAM
├── Slashing:                                 ~2,000 DREAM
├── Total burned:                            ~23,000 DREAM

NET: +70,000 DREAM circulating

Key differences from original model:
├── Staking rewards capped at 25K (was uncapped ~10K but would grow)
├── Staked decay adds ~15K burn (was zero) — primary burn mechanism
├── Unstaked decay reduced to ~3K (was ~20K) — conviction signal quality > burn volume
├── Treasury recycling offsets ~6K minting (was zero)
├── Retro PGF reduced to 15K (was fixed 50K)
├── Mint/burn ratio: 4:1 (was 5:1, improves as supply grows due to proportional burn)
```

### Long-term Sustainability

```
Growth Phase (Years 1-3):
├── Net DREAM inflation: 3-7% per season (reduced from 5-10%)
├── Seasonal staking pool caps reward minting
├── Treasury recycling absorbs increasing share of operational costs
├── Staked decay creates burn floor proportional to total supply
├── Purpose: Bootstrap ecosystem with controlled inflation

Maturity Phase (Years 3+):
├── Target: Burn rate ≈ mint rate
├── Staked decay scales with supply (larger supply → more burn)
├── Seasonal pool APY naturally decreases as more DREAM exists
├── Treasury overflow burns excess during high-activity periods
├── Equilibrium is mechanically enforced, not assumed

Equilibrium Mechanics (why convergence is guaranteed):
├── Minting: Capped (seasonal pool) + linear (initiative budgets)
├── Burning: Proportional to total supply (staked + unstaked decay)
├── As supply grows, burn grows proportionally but minting is capped
├── Crossover point: when proportional burns ≥ capped + linear minting
├── Governance can tune MaxStakingRewardsPerSeason down to accelerate

Key Metrics to Monitor (via QueryDreamSupplyStats, QueryMintBurnRatio):
├── DREAM velocity (transfers per DREAM)
├── Stake ratio (staked / total) — via QueryDreamSupplyStats
├── Mint/burn ratio per season — via QueryMintBurnRatio
├── Effective APY — via QueryEffectiveApy
├── Treasury balance and flows — via QueryTreasuryStatus
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
├── MaxStakingRewardsPerSeason: Currently 25,000 DREAM (seasonal pool cap)
├── UnstakedDecayRate: Currently 0.2%/epoch
├── StakedDecayRate: Currently 0.05%/epoch
├── NewMemberDecayGraceEpochs: Currently 30 (~1 month)
├── TransferTaxRate: Currently 3%
├── CompleterShare: Currently 90%
├── TreasuryShare: Currently 10%

Treasury:
├── MaxTreasuryBalance: Currently 100,000 DREAM (excess burned)
├── TreasuryFundsInterims: Currently true
├── TreasuryFundsRetroPgf: Currently true

Retro PGF:
├── RetroRewardBudgetRatio: Currently 25% of initiative minting
├── RetroRewardBudgetMin: Currently 10,000 DREAM
├── RetroRewardBudgetMax: Currently 75,000 DREAM

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
