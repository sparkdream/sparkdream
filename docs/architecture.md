# Spark Dream Architecture

## System Overview

Spark Dream implements a hierarchical, reputation-based, and market-assisted governance architecture known as **"Three Pillars Governance"**.

This system moves beyond simple token-voting by delegating authority to specialized councils ("Pillars"), ensuring they have guaranteed funding, and holding them accountable via prediction markets ("Futarchy"). The architecture is extended with a reputation-based coordination layer, content platforms for community discourse, identity management, and privacy-preserving anonymous actions via zero-knowledge proofs.

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           SPARK DREAM                                   │
│                        Cosmos SDK Appchain                              │
│                                                                         │
│  13 custom modules · Dual tokens (SPARK/DREAM) · ZK anonymous actions   │
└─────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│                         GOVERNANCE LAYER                                │
├─────────────────────────────────────────────────────────────────────────┤
│  x/gov (SPARK)     x/commons (Councils)       x/futarchy (Markets)      │
│       │                   │                          │                  │
│  Parameter         Three Pillars              Elastic Tenure            │
│  Changes           Governance                 Confidence Markets        │
└─────────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                         ECONOMIC LAYER                                  │
├─────────────────────────────────────────────────────────────────────────┤
│  x/distribution          x/split              x/ecosystem               │
│       │                     │                      │                    │
│  15% Revenue Tax     Council Treasuries      Ecosystem Treasury         │
│  to Community Pool   (50/30/20 split)        (Governance-gated)         │
└─────────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                       COORDINATION LAYER                                │
├─────────────────────────────────────────────────────────────────────────┤
│  x/rep                     x/season                 x/reveal            │
│    │                         │                        │                 │
│  Members, DREAM            Seasonal Resets          Progressive         │
│  Reputation, Initiatives   Gamification (XP,        Open-Source         │
│  Stakes, Challenges          Guilds, Quests)        Tranched Funding    │
│  Content Challenges        Retro Public Goods                           │
│  Author Bonds              Nominations                                  │
└─────────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                     CONTENT & DISCUSSION LAYER                          │
├─────────────────────────────────────────────────────────────────────────┤
│  x/blog                  x/forum                  x/collect             │
│    │                       │                        │                   │
│  Posts, Replies          Threads, Categories      Curated Collections   │
│  Reactions               Sentinel Moderation      Collaborators         │
│  Ephemeral TTL           Bounties, Tag Budgets    Curator Bonding       │
│  Anonymous Posting       Appeals, Archival        Endorsements          │
│  Pin/Hide System         Anonymous Posting        Anonymous Collections │
└─────────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                       IDENTITY & PRIVACY LAYER                          │
├─────────────────────────────────────────────────────────────────────────┤
│  x/name                                    x/vote                       │
│    │                                         │                          │
│  Human-Readable Names                      ZK-SNARK Anonymous Voting    │
│  Dispute Resolution                        Groth16/BN254 Proofs         │
│  Scavenging                                Anonymous Action Proofs      │
│                                            TLE Threshold Encryption     │
│                                            Member Trust Tree            │
└─────────────────────────────────────────────────────────────────────────┘
```

## Module Inventory

| Module | Messages | Queries | EndBlocker | BeginBlocker | Purpose |
|--------|----------|---------|------------|--------------|---------|
| x/commons | 10 | 3 | — | — | Three Pillars governance orchestrator |
| x/split | 0 | 2 | — | BeginBlock | Automated revenue distribution |
| x/futarchy | 6 | 3 | — | — | LMSR prediction markets, elastic tenure |
| x/ecosystem | 1 | 1 | — | — | Governance-gated ecosystem treasury |
| x/rep | 28 | 29 | 12 phases | — | Reputation, DREAM, initiatives, challenges |
| x/season | 42 | 50 | — | 3 phases | Seasons, XP, guilds, quests, retro funding |
| x/reveal | 9 | 11 | Yes | — | Progressive open-source, tranched funding |
| x/blog | 18 | 14 | 3 phases | — | Blog posts, replies, reactions |
| x/forum | 52 | 59 | 4 phases | — | Discussion threads, moderation, bounties |
| x/collect | 31 | 27 | 6 phases | — | Curated collections, curation, endorsements |
| x/name | 7 | 5 | — | Yes | Human-readable identity registry |
| x/vote | 12 | 28 | Yes | — | ZK anonymous voting, TLE encryption |
| x/common | — | — | — | — | Shared types (tags, flags, moderation) |

## Core Module Architecture

### x/commons (The Orchestrator)

**Purpose:** Central engine for the "Three Pillars" governance.

**Mechanism:** Wraps `x/group` with a `Group` structure defined in [x/commons/types/group.pb.go](x/commons/types/group.pb.go).

**Key Logic (MsgRegisterGroup):**
- **Hierarchy:** Enforces parent-child trust chains (Gov → Council → Committee) and prevents cyclic dependencies.
- **Funding:** Automatically registers the group with `x/split` to receive `FundingWeight` share of revenue.
- **Accountability:** Initializes "Elastic Tenure" by triggering the first "Confidence Vote" market via `x/futarchy`.
- **Permissions:** Uses `AllowedMessages` to restrict council powers (e.g., only Technical Council can run `MsgSoftwareUpgrade`).

**Messages (10):** `register_group`, `renew_group`, `update_group_members`, `update_group_config`, `delete_group`, `spend_from_commons`, `policy_permissions` (create/update/delete), `force_upgrade`, `emergency_cancel_gov_proposal`, `veto_group_proposals`

### x/split (The Treasury)

**Purpose:** Automated revenue distribution engine.

**Mechanism:**
- **Shares:** Manages a registry of `Share` objects (Address + Weight).
- **Distribution Loop:** Runs in `BeginBlock` (via [x/split/module/module.go](x/split/module/module.go)). It iterates through the funds in the `x/distribution` module account (Community Pool).
- **Calculation:** Distributes funds to shareholders proportional to their weight: `(Balance × ShareWeight) / TotalWeight`.
- **Optimization:** Includes a "dust protection" check (in [x/split/keeper/split.go](x/split/keeper/split.go)) to skip insignificant transfers and save gas.

**Integration:** `x/commons` registers council policy addresses here, ensuring they automatically receive their allocated funding (e.g., 50% for Commons, 30% for Technical, 20% for Ecosystem).

### x/futarchy (The Accountability Engine)

**Purpose:** Implements "Elastic Tenure" via prediction markets.

**Mechanism:**
- **LMSR AMM:** Logarithmic Market Scoring Rule automated market maker with gas-metered exp/ln calculations.
- **Confidence Markets:** The system automatically creates "Confidence Vote" prediction markets for each council.
- **Elastic Tenure Hook:** When a market resolves, a hook in `x/commons` adjusts the council's term:
  - **High Confidence (YES):** Term extended by **20%**.
  - **Low Confidence (NO):** Term slashed by **50%** (potentially triggering immediate re-election).
- **Two-tier authorization:** Governance + ops committee for market management.

**Messages (6):** `create_market`, `trade`, `redeem`, `withdraw_liquidity`, `cancel_market`, `update_operational_params`

### x/ecosystem (Ecosystem Treasury)

**Purpose:** Independent governance-gated spending from the ecosystem module account.

Separate from the `x/split` distribution pipeline, this module holds funds for ecosystem grants and partnerships, spending only via `x/gov` authority.

**Messages (1):** `spend`

## Coordination Layer

### x/rep (Reputation & Coordination)

**Purpose:** Primary module for the reputation-based task system, member lifecycle, and DREAM token economics.

**Key Features:**
- **Member lifecycle:** Invitation-based membership with 5 trust levels and "zeroing" instead of banning
- **DREAM token:** Minting via initiative completion, burning via slashing/decay/transfer tax, limited transfers (tips/gifts only)
- **Reputation:** Per-tag scores with seasonal resets, anti-gaming caps (0.5%/epoch decay, 33% max conviction share, 50 rep/tag/epoch cap)
- **Projects & initiatives:** Council-approved budgets, tiered initiatives (Apprentice/Standard/Expert/Epic), conviction-based completion
- **Conviction staking:** Time-weighted engagement (half-life: 7 epochs), 50% external conviction requirement
- **Challenges:** Named + anonymous (2.5x fee multiplier), jury resolution (5 members, 67% supermajority)
- **Content challenges:** Cross-module author bonds, slashable on moderation (50% to challenger)
- **Interim compensation:** Fixed-rate delegated duties (Simple 50 → Expert 1,000 DREAM)
- **MasterChef staking rewards:** Epoch-based pool distribution (10% APY)
- **Trust tree:** Sparse Merkle tree (depth 20) of member public keys + trust levels for ZK anonymous action proofs

**Messages (28):** `invite_member`, `accept_invitation`, `transfer_dream`, `propose_project`, `approve_project_budget`, `cancel_project`, `create_initiative`, `assign_initiative`, `submit_initiative_work`, `approve_initiative`, `complete_initiative`, `abandon_initiative`, `stake`, `unstake`, `create_challenge`, `respond_to_challenge`, `submit_juror_vote`, `submit_expert_testimony`, `challenge_content`, `respond_to_content_challenge`, `create_interim`, `assign_interim`, `submit_interim_work`, `approve_interim`, `complete_interim`, `abandon_interim`, `claim_rewards`, `update_operational_params`

**EndBlocker (12 phases per block):**
1. Update conviction for active initiatives
2. Check initiative completion thresholds
3. Finalize unchallenged initiatives
4. DREAM decay (lazy, O(1) per block)
5. Process expired challenge responses
6. Process expired content challenge responses
7. Process jury review deadlines
8. Process assigned initiative deadlines
9. Distribute staking rewards
10. Trust levels (updated lazily on reputation/interim events)
11. Process expired accountability (invitations)
12. Rebuild member trust tree (incremental for dirty members)

### x/season (Seasonal System)

**Purpose:** Seasonal cycles with gamification, retroactive public goods funding, and community engagement features.

**Key Features:**
- **Seasonal cycles:** ~150 epochs (~5 months) with multi-phase transitions
- **Reputation management:** Archival to lifetime records and seasonal reset to baseline
- **XP & leveling:** Seasonal + lifetime XP from voting (5 XP), proposals (10 XP), forum engagement (2-5 XP), invitation success (20-50 XP)
- **Guilds:** Member-created groups (3-100 members), roles (founder/officer/member), seasonal membership, hop cooldown (30 epochs)
- **Quests:** Governance-defined objectives with XP rewards (max 100 XP), quest chains
- **Achievements & titles:** Governance-created, rarity tiers, seasonal display
- **Display names & usernames:** With moderation, DREAM-staked appeals (50/100 DREAM)
- **Retroactive public goods funding:** Nomination window (~5 epochs before season end), conviction-weighted, budget of 50,000 DREAM/season, max 20 recipients, min ESTABLISHED trust to nominate

**Messages (42):** Profile management (3), guild operations (16), quest operations (3), governance-created content (9), nominations (3), display name moderation (3), season management (5), `update_operational_params`

**BeginBlocker (3 phases per block):**
1. Auto-resolve expired display name moderations
2. Check nomination phase (transition ACTIVE → NOMINATION ~1 week before end)
3. Season transition management (start/continue/finalize transitions, 100/block batch)

### x/reveal (Progressive Open-Source)

**Purpose:** Tranched progressive disclosure of closed-source code contributions into open-source community ownership.

**Key Features:**
- **Tranched disclosure:** Up to 10 tranches per contribution, max 50,000 DREAM total valuation
- **Conviction staking:** Community locks DREAM to show interest (returned after verification, not payment)
- **Contributor bonds:** 10% of total valuation locked at proposal, slashable on timeout
- **Stake-weighted verification:** 60% threshold, minimum 3 votes (scales with stake)
- **Three-way dispute resolution:** ACCEPT/IMPROVE/REJECT via Commons Council proposals
- **Payout holdback:** 20% per tranche retained until all tranches complete
- **Self-dealing prevention:** Contributors cannot stake on or vote on own contributions
- **Community ownership:** Transitions to x/rep Project post-completion

**Messages (9):** `propose`, `approve`, `reject`, `cancel`, `stake`, `withdraw`, `reveal`, `verify`, `resolve_dispute`

**EndBlocker:** Processes deadlines for staking (cancels contribution), reveal (slashes bond), verification (auto-tallies), and dispute phases.

## Content & Discussion Layer

### x/blog (Content Management)

**Purpose:** On-chain blog posts with threaded replies, reactions, and anonymous posting support.

**Key Features:**
- **Posts & replies:** CRUD with configurable length constraints (title 200, body 10,000, reply 2,000 chars), max reply depth of 5
- **Reactions:** Like, Insightful, Disagree, Funny with per-reaction fees (50 uspark)
- **Rate limiting:** Max 10 posts/day, 50 replies/day, 100 reactions/day per address
- **Ephemeral content:** Non-member posts expire after TTL (7 days default); conviction renewal extends TTL if conviction ≥ threshold
- **Anonymous posting:** ZK proof of membership + trust level (ESTABLISHED+ default), nullifier scoped by epoch (posts) or post_id (replies)
- **Storage fees:** 100 uspark/byte, charged to submitter or subsidized for approved relays
- **Pin/hide system:** Trust-level-gated pinning (CORE trust for blog), owner/admin hiding
- **Anonymous subsidy:** 100 SPARK/epoch budget from Commons treasury for relay reimbursement

**Messages (18):** Post CRUD (3), reply CRUD (3), hide/unhide post (2), hide/unhide reply (2), pin post (1), pin reply (1), react (1), remove reaction (1), anonymous react (1), create anonymous post (1), create anonymous reply (1), `update_operational_params`

**EndBlocker (3 phases):**
1. TTL expiry (upgrade to permanent if creator becomes member, conviction renewal, or tombstone)
2. Subsidy draw (transfer budget from Commons treasury)
3. Nullifier pruning (remove stale epoch-scoped nullifiers)

### x/forum (Decentralized Discussion)

**Purpose:** Full-featured discussion platform with hierarchical content, dual-token sentinel moderation, bounties, and economic incentives.

**Key Features:**
- **Hierarchical content:** Governance-controlled categories, member-created tags with usage tracking and expiry
- **Sentinel moderation:** DREAM-bonded sentinels (100 DREAM commit) with hide/report authority, accountability via challenges
- **Content lifecycle:** Ephemeral TTL (24h) for non-members, permanent for members, conviction renewal for initiative-linked content
- **Bounties:** Thread-attached DREAM bounties with assignment, cancellation (10% fee), and expiry
- **Tag budgets:** Governance-funded tag-scoped budgets for rewarding quality contributions
- **Anonymous posting:** ZK proof-based (domain 3 for threads, 4 for replies, 5 for reactions), epoch/thread scoped nullifiers
- **Appeals:** Jury-based appeals for hide, lock, move, and governance actions (5 SPARK fee, 14-day deadline)
- **Thread operations:** Lock, freeze, move, archive, pin/unpin, follow/unfollow
- **Member reports:** Multi-step reporting with cosigning, defense, and resolution
- **Rate limiting:** 50 posts/day, 100 reactions/day, 20 downvotes/day, 20 flags/day

**Messages (52):** Post operations (7), moderation (6), thread control (5), reactions (3), reply management (5), bounties (5), tag budgets (5), tags (2), appeals (4), anonymous (3), thread ops (3), emergency (2), governance (2)

**EndBlocker (4 phases):**
1. Prune expired ephemeral posts (max 100/block, conviction renewal check for initiatives)
2. Expire hidden posts (max 50/block, soft-delete after 7 days)
3. Expire bounties (max 50/block, refund escrowed funds)
4. Expire tags (max 50/block, reserved tags never expire)

### x/collect (Curated Collections)

**Purpose:** Decentralized curated collections with collaborators, quality curation, and community engagement.

**Key Features:**
- **Collection types:** NFTs, links, on-chain references, custom data with max 500 items per collection
- **Collaboration:** Role-based permissions (EDITOR, ADMIN) with up to 20 collaborators
- **Privacy:** Client-side encryption for private collections
- **Two-tier content:** Members get permanent storage, non-members get TTL + PENDING status
- **Curator bonding:** DREAM-staked curator registration (min 500 DREAM, PROVISIONAL+ trust), quality ratings, challenge mechanism (250 DREAM deposit)
- **Community reactions:** Upvotes free (max 100/day), downvotes cost 25 SPARK (max 20/day)
- **Endorsements:** 10 SPARK creation fee + 100 DREAM stake for 30 days, 80% fee share to endorser
- **Sponsorship:** Non-members can request sponsorship (1 SPARK fee) for ESTABLISHED+ members to sponsor
- **Sentinel moderation:** Shared with x/forum sentinel system, 100 DREAM bond, appeals
- **Anonymous collections:** ZK proof-based (ESTABLISHED+ trust), management key tracking, max 3 per key
- **Pinning:** ESTABLISHED+ trust, max 10 pins/day
- **On-chain references:** Validate content existence in x/blog and x/forum

**Messages (31):** Collection CRUD (3), items (5), collaborators (3), curation (4), sponsorship (3), endorsement (2), reactions (3), moderation (3), anonymous (3), pin (1), `update_operational_params`

**EndBlocker (6 phases, cap 100/block total):**
1. Prune expired collections (conviction renewal for anonymous collections)
2. Prune sponsorship requests (refund escrowed deposits)
3. Prune hide records (delete unappealed, restore appealed)
4. Prune expired flags
5. Prune unendorsed collections (refund minus burn)
6. Release endorsement stakes

## Identity & Privacy Layer

### x/name (Identity Registry)

**Purpose:** Human-readable name registration with council-gated access and dispute resolution.

**Key Features:**
- **Name registration:** Council members only (Commons Council), 1-20 chars
- **Resolution:** Forward (name → address) and reverse (address → primary name)
- **Scavenging:** Auto-expire after 1 year of owner inactivity
- **Disputes:** DREAM-staked filing and contesting, jury arbitration via x/rep
- **Blocked names:** 100+ reserved names (crypto projects, real people)
- **Per-address limits:** Max names per address (configurable)

**Messages (7):** `register_name`, `update_name`, `set_primary`, `file_dispute`, `contest_dispute`, `resolve_dispute`, `update_operational_params`

**BeginBlocker:** Auto-resolve expired disputes.

### x/vote (Anonymous Voting with ZK Proofs)

**Purpose:** Privacy-preserving voting and anonymous action verification using zero-knowledge proofs.

**Key Features:**
- **ZK-SNARK circuits:** Groth16/BN254 with MiMC hashing (~14,000 constraints)
- **Voter registration:** ZK key commitments, public key = MiMC(secretKey)
- **Merkle tree snapshots:** Depth 20 (~1M voters), snapshotted at proposal creation
- **Nullifier-based double-vote prevention:** `nullifier = MiMC(secretKey, proposalID)`
- **Voting modes:** PUBLIC (immediate tally), SEALED (commit-reveal via TLE), PRIVATE (ZK anonymous)
- **Threshold Timelock Encryption (TLE):** Validator DKG shares, 2/3 threshold for decryption, sealed vote auto-reveal
- **Voter key rotation:** Atomic key replacement
- **Anonymous action proofs:** Separate circuit for cross-module anonymous posting (x/blog, x/forum, x/collect)

**Messages (12):** `register_voter`, `deactivate_voter`, `rotate_voter_key`, `create_proposal`, `create_anonymous_proposal`, `cancel_proposal`, `vote`, `submit_sealed_vote`, `reveal_vote`, `register_tle_share`, `submit_decryption_share`, `store_srs`

**EndBlocker:**
- TLE liveness tracking (per-epoch validator checks)
- Proposal lifecycle transitions (PUBLIC → finalize, SEALED → TALLYING → auto-reveal → finalize)

## Cross-Module Anonymous Action System

The anonymous posting system extends x/vote's ZK infrastructure across content modules (x/blog, x/forum, x/collect). A separate "Anonymous Action Circuit" proves membership and trust level without revealing identity.

### How It Works

```
CLIENT-SIDE                                    ON-CHAIN

1. Derive ZK keys
   secretKey = derive(account_sig)
   publicKey = MiMC(secretKey)

2. Fetch trust tree root                  ◀── x/rep provides current root
   + Merkle proof for leaf                    (rebuilt incrementally in EndBlocker)

3. Compute nullifier
   null = MiMC(domain, secretKey, scope)

4. Generate Groth16 proof (~2-5s)          ── Proves:
   Public:  merkleRoot, nullifier,            - "I know a secretKey whose publicKey
            minTrustLevel, scope                 is in the tree at this trust level"
   Private: secretKey, trustLevel,            - "This nullifier is correctly derived"
            merklePath, pathIndices            - "My trust level ≥ minimum required"

5. Submit via relay (optional)            ──▶ x/vote verifies proof (~2-5ms)
                                              Content module checks nullifier
                                              Creator set to module account (anonymous)
                                              Ephemeral TTL applied
```

### Nullifier Domains

Each anonymous action type has a unique domain to prevent cross-module collisions:

| Domain | Module | Action | Scope | Rate Limit |
|--------|--------|--------|-------|------------|
| 1 | x/blog | Anonymous post | Current epoch | 1 per epoch |
| 2 | x/blog | Anonymous reply | post_id | 1 per post |
| 8 | x/blog | Anonymous post reaction | post_id | 1 per post |
| 9 | x/blog | Anonymous reply reaction | reply_id | 1 per reply |
| 3 | x/forum | Anonymous thread | Current epoch | 1 per epoch |
| 4 | x/forum | Anonymous reply | thread_id | 1 per thread |
| 5 | x/forum | Anonymous reaction | post_id | 1 per post |
| 6 | x/collect | Anonymous collection | Current epoch | 1 per epoch |
| 10 | x/collect | Anonymous reaction | collection_id | 1 per collection |

### Anonymous Subsidy System

Each content module draws a per-epoch budget from Commons Council treasury to reimburse approved relay addresses for anonymous posting fees. This ensures members can post anonymously without revealing identity through fee payment patterns.

- Blog: 100 SPARK/epoch, max 2 SPARK per post
- Collect: 50 SPARK/epoch, max 2 SPARK per action
- Relays: Approved addresses that broadcast anonymous transactions on behalf of members

## Module Dependency Graph

```
                    ┌──────────────┐
                    │   x/gov      │
                    │   (SPARK)    │
                    └──────┬───────┘
                           │
              ┌────────────┴────────────┐
              │                         │
              ▼                         ▼
      ┌──────────────┐          ┌──────────────┐
      │ x/distribution│         │  x/futarchy  │
      │  (15% tax)   │          │  (markets)   │
      └──────┬───────┘          └──────┬───────┘
             │                         │
             ▼                         │
      ┌──────────────┐                 │
      │   x/split    │                 │
      │ (50/30/20)   │                 │
      └──────┬───────┘                 │
             │                         │
             ▼                         ▼
      ┌─────────────────────────────────────┐
      │            x/commons                │
      │     (Three Pillars Governance)      │
      │                                     │
      │  ┌─────────┐ ┌─────────┐ ┌─────────┐│
      │  │ Commons │ │Technical│ │Ecosystem││
      │  │ Council │ │ Council │ │ Council ││
      │  │  (50%)  │ │  (30%)  │ │  (20%)  ││
      │  └────┬────┘ └────┬────┘ └────┬────┘│
      │       │           │           │     │
      │    ┌──┴──┐     ┌──┴──┐     ┌──┴──┐  │
      │    │HR   │     │HR   │     │HR   │  │
      │    │Ops  │     │Ops  │     │Ops  │  │
      │    └─────┘     └─────┘     └─────┘  │
      └──────────────────┬──────────────────┘
                         │
          ┌──────────────┼──────────────┐
          ▼              ▼              ▼
   ┌──────────────┐ ┌──────────┐ ┌──────────────┐
   │   x/rep      │ │ x/name   │ │  x/reveal    │
   │              │ │          │ │              │
   │ Members      │ │ Identity │ │ Tranched     │
   │ DREAM Token  │ │ Registry │ │ Funding      │
   │ Reputation   │ │ Disputes │ │ Verification │
   │ Initiatives  │ │          │ │              │
   │ Challenges   │ └──────────┘ └──────────────┘
   │ Trust Tree   │
   └──────┬───────┘
          │
    ┌─────┼─────────────────┐
    ▼     ▼                 ▼
┌────────┐┌────────┐┌──────────────┐
│x/season││ x/vote ││  x/common    │
│        ││        ││  (types)     │
│Seasons ││ZK Proofs│└──────────────┘
│XP/Guild││Anon    │       │
│Quests  ││Actions │       │
│Retro $ ││TLE     │       ▼
└────────┘└────┬───┘┌──────────────┐
               │    │  x/forum     │
               │    │  (tags,      │
               │    │   TagKeeper) │
               │    └──────────────┘
               │
    ┌──────────┼──────────┐
    ▼          ▼          ▼
┌────────┐┌────────┐┌──────────┐
│x/blog  ││x/forum ││x/collect │
│        ││        ││          │
│Posts   ││Threads ││Collections│
│Replies ││Bounties││Curation  │
│Anon ZK ││Anon ZK ││Anon ZK   │
└────────┘└────────┘└──────────┘
```

### Cross-Module Keeper Wiring (app.go)

Many keepers are wired manually after `depinject.Inject()` to break cyclic dependencies:

```
x/futarchy   ← SetCommonsKeeper(commons)

x/season     ← SetRepKeeper(rep), SetNameKeeper(name), SetCommonsKeeper(commons)
             ← SetBlogKeeper(blog), SetForumKeeper(forum), SetCollectKeeper(collect)

x/blog       ← SetRepKeeper(rep), SetVoteKeeper(vote), SetSeasonKeeper(season)

x/forum      ← SetSeasonKeeper(season)

x/collect    ← SetSeasonKeeper(season), SetRepKeeper(rep)

x/rep        ← SetSeasonKeeper(season)  [via shared lateKeepers pointer]
             ← SetVoteKeeper(vote)      [via lateKeepers]
             ← SetTagKeeper(forum)      [via lateKeepers — forum implements TagKeeper]
```

The `lateKeepers` pattern in x/rep uses a shared pointer struct so that `Set*Keeper()` mutations are visible to all keeper value copies (including the one inside AppModule's msgServer).

## Fund Flows

### SPARK Flow (External Value)

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          SPARK FLOW                                     │
└─────────────────────────────────────────────────────────────────────────┘

Transaction Fees + Inflation (2% - 5% annual, IMMUTABLE)
                    │
                    ▼
            x/distribution
                    │
            ┌───────┴───────┐
            │               │
            ▼               ▼
    85%: Staking      15%: Community Pool
    Rewards                 │
    (validators/            │
    delegators)             ▼
                        x/split
                            │
            ┌───────────────┼───────────────┐
            │               │               │
            ▼               ▼               ▼
    50%: Commons      30%: Technical   20%: Ecosystem
    Treasury          Treasury         Treasury
    (SPARK)           (SPARK)          (SPARK)
            │               │               │
            └───────────────┴───────────────┘
                            │
                            ▼
                Operations Committees
                approve project budgets
                            │
                            ▼
            SPARK for external expenses only:
            ├── Security audits
            ├── Infrastructure hosting
            ├── External grants
            └── Third-party services
```

### DREAM Flow (Internal Value)

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          DREAM FLOW                                     │
└─────────────────────────────────────────────────────────────────────────┘

Council Operations Committee
approves project DREAM budget
            │
            ▼
    Project created with
    DREAM minting authorization
            │
            ▼
    Initiative created from
    project budget allocation
            │
            ▼
    Assignee completes work
    + Stakers build conviction
            │
            ▼
    Conviction threshold met
    + Challenge period passed
            │
            ▼
    ┌───────────────────────────────────────┐
    │           DREAM MINTED                │
    ├───────────────────────────────────────┤
    │ Completer: Budget × 90% × RepMult     │
    │ Treasury:  Budget × 10%               │
    │ Stakers:   Stake × APY × Duration     │
    └───────────────────────────────────────┘
            │
            ▼
    DREAM in circulation
            │
    ┌───────┴───────┐
    │               │
    ▼               ▼
    Staked          Unstaked
    (working)       (decaying)
    │               │
    │               ▼
    │           1%/epoch decay
    │           (burned)
    │
    ▼
    Various sinks:
    ├── Slashing penalties (burned)
    ├── Failed challenges (burned)
    ├── Failed invitations (burned)
    ├── Transfer tax 3% (burned)
    ├── Content author bonds (slashable)
    ├── Curator challenge bonds (slashable)
    └── Display name appeal stakes (burned on loss)

Additional DREAM minting sources:
    ├── Staking rewards (10% APY, time-proportional)
    ├── Interim compensation (50-1000 DREAM by complexity)
    ├── Retroactive public goods funding (50,000 DREAM/season)
    └── x/reveal tranche payouts (minus 20% holdback)
```

### Initiative Reward Distribution

```
Initiative Budget: 500 DREAM
Completer Reputation: 80 (in 0-100 scale for Standard tier)
Rep Multiplier: 80/100 = 0.8

┌─────────────────────────────────────────┐
│         REWARD DISTRIBUTION             │
├─────────────────────────────────────────┤
│                                         │
│  Completer Pool: 500 × 90% = 450 DREAM  │
│  After Rep Mult: 450 × 0.8 = 360 DREAM  │
│                                         │
│  Treasury: 500 × 10% = 50 DREAM         │
│                                         │
│  Staker A (1000 DREAM, 14 days):        │
│    1000 × 10% APY × (14/365) = 3.8 DREAM│
│                                         │
│  Staker B (500 DREAM, 7 days):          │
│    500 × 10% APY × (7/365) = 0.96 DREAM │
│                                         │
└─────────────────────────────────────────┘

Total Minted: 360 + 50 + 3.8 + 0.96 = ~415 DREAM
```

## Governance Structure

### The Three Pillars

The governance power is distributed across three specialized councils, each with a specific mandate and funding allocation:

- **Commons Council (50% Funding):** Focuses on Culture, Arts, and Events.
- **Technical Council (30% Funding):** Responsible for Infrastructure, Upgrades, and Security.
- **Ecosystem Council (20% Funding):** Manages the Treasury and Grants for growth.

This architecture is designed to solve the "voter apathy" and "uninformed voting" problems of DAOs. Instead of every token holder voting on every upgrade or grant:

1. **Token holders** elect and fund expert **Councils** and **Committees**.
2. **Councils** and **Committees** make day-to-day decisions.
3. **Markets** (Futarchy) continuously evaluate the councils' performance, automatically rewarding success with longer terms or punishing failure with shorter terms.

### Council Hierarchy

```
┌─────────────────────────────────────────────────────────────────────────┐
│                     GOVERNANCE HIERARCHY                                │
└─────────────────────────────────────────────────────────────────────────┘

x/gov (SPARK holders)
    │
    ├── Parameter changes (chain-wide)
    ├── Emergency actions
    └── Constitutional amendments
            │
            ▼
┌───────────────────────────────────────────────────────────────┐
│                    THREE PILLARS                              │
├───────────────────┬───────────────────┬───────────────────────┤
│   Commons (50%)   │  Technical (30%)  │   Ecosystem (20%)     │
│                   │                   │                       │
│ Culture, Arts,    │ Infrastructure,   │ Treasury, Grants,     │
│ Events, Community │ Upgrades, Security│ Partnerships, Growth  │
├───────────────────┼───────────────────┼───────────────────────┤
│ ┌───────────────┐ │ ┌───────────────┐ │ ┌───────────────────┐ │
│ │ HR Committee  │ │ │ HR Committee  │ │ │ HR Committee      │ │
│ │ (2-5 members) │ │ │ (2-5 members) │ │ │ (2-5 members)     │ │
│ │               │ │ │               │ │ │                   │ │
│ │ - Membership  │ │ │ - Membership  │ │ │ - Membership      │ │
│ │ - Slashing    │ │ │ - Slashing    │ │ │ - Slashing        │ │
│ │ - Mediation   │ │ │ - Mediation   │ │ │ - Mediation       │ │
│ └───────────────┘ │ └───────────────┘ │ └───────────────────┘ │
│ ┌───────────────┐ │ ┌───────────────┐ │ ┌───────────────────┐ │
│ │ Ops Committee │ │ │ Ops Committee │ │ │ Ops Committee     │ │
│ │ (2-5 members) │ │ │ (2-5 members) │ │ │ (2-5 members)     │ │
│ │               │ │ │               │ │ │                   │ │
│ │ - Budgets     │ │ │ - Budgets     │ │ │ - Budgets         │ │
│ │ - Projects    │ │ │ - Projects    │ │ │ - Projects        │ │
│ │ - Operations  │ │ │ - Operations  │ │ │ - Operations      │ │
│ └───────────────┘ │ └───────────────┘ │ └───────────────────┘ │
└───────────────────┴───────────────────┴───────────────────────┘

Cross-Council Mechanisms:
├── Veto power (any council can veto another's decisions)
├── Joint proposals (require multiple council approval)
└── Appeals (committee → council → cross-council)
```

### Decision Routing

```
Decision Type              │ Handler                │ Approval Required
───────────────────────────┼────────────────────────┼──────────────────────
Minor slash (<5%)          │ HR Committee           │ Simple majority
Moderate slash (5-30%)     │ HR Committee + Appeal  │ Supermajority
Severe slash / Zeroing     │ Random jury (5)        │ 2/3 supermajority
                           │                        │
Small project (<3K DREAM)  │ Ops Committee          │ Simple majority
Medium project (<10K)      │ Ops Committee + Review │ Council notification
Large project (>10K)       │ Council vote           │ Conviction threshold
                           │                        │
Parameter tweak            │ Ops Committee          │ 60% + 1 week
Structural change          │ Council vote           │ 66% + 2 weeks
Constitutional change      │ All councils + x/gov   │ 75% + 1 month
```

## Member Lifecycle

```
┌─────────────────────────────────────────────────────────────────────────┐
│                       MEMBER LIFECYCLE                                  │
└─────────────────────────────────────────────────────────────────────────┘

                    ┌──────────────┐
                    │   Outsider   │
                    │  (no access) │
                    └──────┬───────┘
                           │
                    Receives invitation
                    (inviter stakes DREAM)
                           │
                           ▼
                    ┌──────────────┐
                    │     New      │ ─────────────────────────────────┐
                    │  Trust Lvl 0 │                                  │
                    └──────┬───────┘                                  │
                           │                                          │
              Complete 3+ initiatives, earn 50+ rep                   │
                           │                                          │
                           ▼                                          │
                    ┌──────────────┐                                  │
                    │ Provisional  │                                  │
                    │  Trust Lvl 1 │                                  │
                    └──────┬───────┘                                  │
                           │                                          │
              Complete 10+ initiatives, earn 200+ rep                 │
                           │                                     Accountability
                           ▼                                     Period
                    ┌──────────────┐                             (~1 season)
                    │ Established  │                                  │
                    │  Trust Lvl 2 │ ◀────────────────────────────────┘
                    └──────┬───────┘   Inviter no longer responsible
                           │
              500+ rep, 1+ full season active
                           │
                           ▼
                    ┌──────────────┐
                    │   Trusted    │
                    │  Trust Lvl 3 │
                    └──────┬───────┘
                           │
              1000+ rep, 2+ seasons, community recognition
                           │
                           ▼
                    ┌──────────────┐
                    │     Core     │
                    │  Trust Lvl 4 │
                    └──────────────┘


Trust Level Permissions:
──────────────────────────────────────────────────────────────────────────
Level       │ Initiative Tiers   │ Invitations │ Jury     │ Committee
──────────────────────────────────────────────────────────────────────────
New         │ Apprentice only    │ 0           │ No       │ No
Provisional │ + Standard         │ 2           │ No       │ No
Established │ + Expert           │ 5           │ Yes      │ Deputy only
Trusted     │ + Epic             │ 10          │ Yes      │ Eligible (2-5)
Core        │ All                │ 20          │ Priority │ Eligible (2-5)
──────────────────────────────────────────────────────────────────────────

Note: Committees require 2-5 members. Initial bootstrap uses "golden share"
pattern with founder + parent council policy address (2 members).
```

## Initiative Lifecycle

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         INITIATIVE LIFECYCLE                            │
└─────────────────────────────────────────────────────────────────────────┘

┌──────────────────┐
│ Project Created  │  Operations Committee approves budget
│ (Budget approved)│
└────────┬─────────┘
         │
         ▼
┌─────────────────────┐
│  Initiative Created │  Creator allocates from project budget
│                     │  Sets: tier, tags, description, template
└────────┬────────────┘
         │
         ▼
┌──────────────────┐
│      Open        │  Visible to eligible members
│                  │  Stakers can begin staking
└────────┬─────────┘
         │
         │ Member claims initiative
         │ (meets reputation requirements)
         ▼
┌──────────────────┐
│    Assigned      │  Assignee working
│                  │  Stakers continue staking
│                  │  Conviction accumulating
└────────┬─────────┘
         │
         │ Assignee submits deliverable
         │
         ▼
┌──────────────────┐
│    Submitted     │  Work under review
│                  │  Review period: 7 epochs
│                  │  Stakers can adjust stakes
└────────┬─────────┘
         │
         │ Review period ends
         │ Conviction check
         │
         ├─────────────────────────────────┐
         │                                 │
         ▼                                 ▼
┌──────────────────┐              ┌──────────────────┐
│ Conviction Met   │              │ Conviction Not   │
│                  │              │ Met              │
└────────┬─────────┘              └────────┬─────────┘
         │                                 │
         ▼                                 ▼
┌──────────────────┐              Extended deadline
│ Challenge Period │              or initiative fails
│ (7 epochs)       │
└────────┬─────────┘
         │
    ┌────┴────┐
    │         │
    ▼         ▼
Challenged  No Challenge
    │              │
    ▼              ▼
┌──────────┐  ┌──────────────┐
│  Jury    │  │  Completed   │
│  Review  │  │              │
└────┬─────┘  │ DREAM minted │
     │        │ Rep granted  │
┌────┴─────┐  │ Stakes return│
│          │  └──────────────┘
▼          ▼
Upheld     Rejected
│          │
▼          ▼
Initiative Initiative
Rejected   Completed
```

## Challenge & Jury System

```
┌─────────────────────────────────────────────────────────────────────────┐
│                      CHALLENGE SYSTEM                                   │
└─────────────────────────────────────────────────────────────────────────┘

Challenge Created
(challenger stakes DREAM, min 50)
(anonymous: 2.5x fee multiplier + 1 SPARK escrow)
        │
        ▼
┌───────────────────┐
│  Automatic Triage │
├───────────────────┤
│ Auto-checks:      │
│ - Required checks │
│   obviously fail? │
│   → Auto-uphold   │
│                   │
│ - No evidence,    │
│   low stake?      │
│   → Auto-reject   │
│                   │
│ - Otherwise:      │
│   → Route to jury │
└────────┬──────────┘
         │
         ▼
┌────────────────────┐
│   Jury Selection   │
├────────────────────┤
│ Criteria:          │
│ - Reputation in    │
│   initiative domain│
│ - Not affiliated   │
│   with parties     │
│ - Not recent juror │
│                    │
│ Selection:         │
│ - Weighted random  │
│ - 5 jurors         │
│ - Optional expert  │
│   witnesses        │
└────────┬───────────┘
         │
         ▼
┌─────────────────────────┐
│     Jury Review         │
├─────────────────────────┤
│ Evidence:               │
│ - Initiative deliverable│
│ - Challenger claim      │
│ - Assignee response     │
│ - Expert testimony      │
│                         │
│ Voting:                 │
│ - Per-criteria          │
│ - Overall verdict       │
│ - Confidence level      │
│ - Reasoning             │
└────────┬────────────────┘
         │
         ▼
┌───────────────────┐
│   Vote Tally      │
├───────────────────┤
│ Uphold challenge: │
│   >2/3 majority   │
│                   │
│ Reject challenge: │
│   >1/2 majority   │
│                   │
│ Inconclusive:     │
│   Escalate        │
└────────┬──────────┘
         │
    ┌────┴────┐
    │         │
    ▼         ▼
┌───────────┐ ┌───────────┐
│ Upheld    │ │Rejected   │
├───────────┤ ├───────────┤
│Work bad   │ │Work good  │
│           │ │           │
│Assignee:  │ │Challenger:│
│- No pay   │ │- Stake    │
│- Rep hit  │ │  burned   │
│           │ │- Rep hit  │
│Challenger:│ │           │
│- Stake    │ │Assignee:  │
│  back     │ │- Normal   │
│- Reward   │ │  reward   │
│           │ │           │
│Stakers:   │ │Stakers:   │
│- Stakes   │ │- Normal   │
│  return   │ │  rewards  │
└───────────┘ └───────────┘
```

### Content Challenges

Content challenges (cross-module) target author bonds on blog posts, forum threads, and collection items:

```
Content Challenge Created
(challenger stakes DREAM)
        │
        ▼
Author has 3 epochs to respond
        │
        ├── No response → Auto-uphold
        │
        ├── Response submitted → Jury review
        │
        ▼
Upheld: Author bond slashed (50% to challenger, 50% burned)
Rejected: Challenger stake burned, author bond returned
```

## Seasonal System

```
┌─────────────────────────────────────────────────────────────────────────┐
│                       SEASON LIFECYCLE                                  │
└─────────────────────────────────────────────────────────────────────────┘

Season Duration: ~150 epochs (~5 months)

┌─────────────────────────────────────────────────────────────────────────┐
│ SEASON START                                                            │
├─────────────────────────────────────────────────────────────────────────┤
│ - New DREAM minting authorization set                                   │
│ - All members start with baseline reputation                            │
│ - DREAM balances unchanged (no reset)                                   │
│ - Previous season archived                                              │
│ - New season theme announced                                            │
│ - Season pass/XP reset to 0                                             │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ SEASON ACTIVE                                                           │
├─────────────────────────────────────────────────────────────────────────┤
│ Normal operations:                                                      │
│ - Initiatives created and completed                                     │
│ - DREAM minted and burned                                               │
│ - Reputation earned (decays 0.5%/epoch, ~47% retained over season)      │
│ - XP accumulated (capped at 200/epoch)                                  │
│ - Achievements unlocked                                                 │
│ - Titles earned                                                         │
│ - Guild activities                                                      │
│ - Quests progressed                                                     │
│ - Leaderboards updated                                                  │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ NOMINATION PHASE (~5 epochs before season end)                          │
├─────────────────────────────────────────────────────────────────────────┤
│ Retroactive Public Goods Funding:                                       │
│ - ESTABLISHED+ members nominate content (blog/forum/collect)            │
│ - Max 3 nominations per member                                          │
│ - PROVISIONAL+ members stake conviction on nominations                  │
│ - Conviction half-life: 3 epochs (~3 days, fast turnover)               │
│ - Budget: 50,000 DREAM per season                                       │
│ - Max 20 recipients, min 50.0 conviction threshold                      │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ SEASON TRANSITION (~7 epochs)                                           │
├─────────────────────────────────────────────────────────────────────────┤
│ Multi-phase processing (100 members/block batch):                       │
│ 1. Snapshot all member stats                                            │
│ 2. Archive reputation to lifetime records                               │
│ 3. Calculate final leaderboards                                         │
│ 4. Grant seasonal titles (Champion, etc.)                               │
│ 5. Distribute retroactive rewards (conviction-weighted)                 │
│ 6. Reset reputation to baseline (0.5)                                   │
│ 7. Keep DREAM balances (no reset)                                       │
│ 8. Start new season                                                     │
│                                                                         │
│ Awards:                                                                 │
│ - Season Champion (highest XP)                                          │
│ - Domain Champions (per tag)                                            │
│ - Rising Star (most improved)                                           │
│ - Talent Scout (best inviter)                                           │
│ - Guild of the Season                                                   │
└─────────────────────────────────────────────────────────────────────────┘

Reputation Flow:
───────────────────────────────────────────────────────────────────────
           │ Season 1  │ Season 2  │ Season 3  │ Lifetime
───────────────────────────────────────────────────────────────────────
Start      │    0      │    0      │    0      │   N/A
Earned     │  +150     │  +200     │  +180     │   N/A
End        │   150     │   200     │   180     │   530
Archived   │   150     │   200     │   180     │   530
Reset to   │   0.5     │   0.5     │   0.5     │   N/A
───────────────────────────────────────────────────────────────────────
```

### Gamification Features

```
XP SOURCES                              │ Amount   │ Cap/Epoch
────────────────────────────────────────┼──────────┼──────────
Vote cast                               │    5 XP  │  10 XP
Proposal created                        │   10 XP  │   —
Forum reply received                    │    2 XP  │  50 XP
Forum marked helpful                    │    5 XP  │  50 XP
Invitee completes first initiative      │   20 XP  │   —
Invitee reaches ESTABLISHED             │   50 XP  │   —
Total cap per epoch                     │    —     │ 200 XP

LEVELS (10 tiers):
XP thresholds: [0, 100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500]

GUILDS:
├── Creation cost: 100 DREAM
├── Size: 3-100 members, max 5 officers
├── Max per season: 3 guilds
├── Hop cooldown: 30 epochs
└── Invite TTL: 30 epochs

QUESTS:
├── Max 5 objectives per quest
├── Max 100 XP reward per quest
├── Max 10 active quests per member
└── Quest chains supported
```

## Progressive Reveal (x/reveal)

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    PROGRESSIVE REVEAL FLOW                              │
└─────────────────────────────────────────────────────────────────────────┘

ESTABLISHED+ member proposes contribution
(existing closed-source project)
Contributor bond: 10% of total valuation
              │
              ▼
┌─────────────────────────────────────────┐
│ Operations Committee Review             │
├─────────────────────────────────────────┤
│ - Evaluate proposed valuation           │
│ - Review tranche structure (max 10)     │
│ - Approve or negotiate terms            │
│ - Max total valuation: 50,000 DREAM     │
└─────────────┬───────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│ Tranche 1: Foundation                   │
│ Min stake: 100 DREAM                    │
├─────────────────────────────────────────┤
│                                         │
│  Community members stake DREAM          │
│  (returned after verification)          │
│              │                          │
│              ▼                          │
│  Staking deadline: 60 epochs            │
│              │                          │
│              ▼                          │
│  Contributor reveals code to IPFS       │
│  Reveal deadline: 14 epochs             │
│              │                          │
│              ▼                          │
│  Verification period (14 epochs)        │
│  - Stakers review code                  │
│  - Vote: 60% threshold, min 3 votes    │
│              │                          │
│         ┌────┴────┐                     │
│         │         │                     │
│         ▼         ▼                     │
│     Verified   Disputed                 │
│         │         │                     │
│         ▼         ▼                     │
│   Payout minus  Council review          │
│   20% holdback  (ACCEPT/IMPROVE/REJECT) │
│         │       30 epochs deadline      │
│         ▼                               │
│   Tranche 2 unlocks                     │
│                                         │
└─────────────────────────────────────────┘
              │
              ▼
      [Repeat for each tranche]
              │
              ▼
┌─────────────────────────────────────────┐
│ All Tranches Complete                   │
├─────────────────────────────────────────┤
│ - Holdback (20%) released to contributor│
│ - Code fully Apache 2.0 licensed        │
│ - Transitions to normal x/rep Project   │
│ - Future work via standard initiatives  │
│ - Contributor has no special privileges │
└─────────────────────────────────────────┘
```

## Anonymous Voting (x/vote)

The x/vote module implements privacy-preserving voting using zero-knowledge proofs (ZK-SNARKs). This enables voters to cast ballots without revealing their identity while still proving eligibility and preventing double-voting.

### Why Anonymous Voting?

| Problem | Solution |
|---------|----------|
| Vote buying/coercion | Votes can't be proven to third parties |
| Social pressure | No one knows how you voted |
| Jury intimidation | Challenge jurors vote anonymously |
| Strategic voting | Can't coordinate based on others' votes |

### ZK Circuit Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                       ZK VOTING ARCHITECTURE                            │
└─────────────────────────────────────────────────────────────────────────┘

SETUP PHASE (One-time, before mainnet)
──────────────────────────────────────────────────────────────────────────
                    ┌─────────────────────┐
                    │   Trusted Setup     │
                    │   (MPC Ceremony)    │
                    └──────────┬──────────┘
                               │
               ┌───────────────┴───────────────┐
               ▼                               ▼
     ┌─────────────────────┐         ┌─────────────────────┐
     │   Proving Key       │         │   Verifying Key     │
     │   (~50-100 MB)      │         │   (~1-2 KB)         │
     │                     │         │                     │
     │   Distributed to    │         │   Embedded in       │
     │   voter clients     │         │   chain genesis     │
     └─────────────────────┘         └─────────────────────┘

REGISTRATION PHASE (Per-member, one-time)
──────────────────────────────────────────────────────────────────────────
     Client                              Chain
        │                                   │
        │  Generate secretKey (random)      │
        │  Compute publicKey = MiMC(sk)     │
        │  Store secretKey securely         │
        │                                   │
        │  ────MsgRegisterVoter────────▶    │
        │     (publicKey only)              │
        │                                   │
        │                          Store VoterRegistration
        │                          {publicKey, votingPower}
        │                                   │

VOTING PHASE
──────────────────────────────────────────────────────────────────────────
     Voter                               Chain
        │                                   │
        │  ◀────Query merkleProof────────   │
        │     (for my publicKey)            │
        │                                   │
        │  Compute:                         │
        │  - nullifier = MiMC(sk, proposalID)
        │                                   │
        │  Generate ZK proof proving:       │
        │  ┌─────────────────────────────┐  │
        │  │ PUBLIC (on-chain):         │  │
        │  │ - merkleRoot               │  │
        │  │ - nullifier                │  │
        │  │ - proposalID               │  │
        │  │ - voteOption (0/1/2)       │  │
        │  │ - votingPower              │  │
        │  ├─────────────────────────────┤  │
        │  │ PRIVATE (never revealed):  │  │
        │  │ - secretKey                │  │
        │  │ - merkleProof path         │  │
        │  └─────────────────────────────┘  │
        │                                   │
        │  ────MsgVote─────────────────▶    │
        │                                   │
        │                          Verify ZK proof (~2-5 ms)
        │                          Check nullifier not used
        │                          Record nullifier
        │                          Update tally
        │                                   │

VOTING MODES:
├── PUBLIC:  Immediate on-chain tally
├── SEALED:  TLE-encrypted, auto-revealed after voting period
└── PRIVATE: ZK anonymous, nullifier-based
```

### Threshold Timelock Encryption (TLE)

For SEALED and PRIVATE voting modes, x/vote uses validator-based threshold encryption:

- **Validator DKG:** Validators register TLE shares during setup
- **Threshold:** 2/3 of validators must submit decryption shares
- **Auto-reveal:** EndBlocker auto-decrypts sealed votes when decryption key becomes available
- **Liveness tracking:** Per-epoch validator participation monitoring (miss window: 100 blocks, tolerance: 10 misses)

### Privacy Guarantees

```
PUBLIC (visible on-chain):
├── Merkle root of eligible voters
├── Nullifier (hash of secretKey + proposalID)
├── Vote option and voting power claimed
├── ZK proof bytes (~200 bytes)
└── Aggregate tallies

PRIVATE (never revealed):
├── Voter's address and secret key
├── Voter's position in Merkle tree
├── Merkle proof path
└── Link between voter and their vote
```

### Vote Types

```
┌────────────────────────┬────────────────────────────────────────────────┐
│ Type                   │ Description                                    │
├────────────────────────┼────────────────────────────────────────────────┤
│ GENERAL                │ General governance proposals                   │
│ PARAMETER_CHANGE       │ Modify module parameters                       │
│ COUNCIL_ELECTION       │ Elect council/committee members                │
│ CHALLENGE_JURY         │ Anonymous jury vote on initiative challenges   │
│ SLASHING               │ Vote on slashing proposals                     │
│ BUDGET_APPROVAL        │ Large budget approvals                         │
└────────────────────────┴────────────────────────────────────────────────┘

Default thresholds:
├── Quorum: 33% of voting power must participate
├── Threshold: 50% + 1 to pass (simple majority)
├── Veto threshold: 33.4%
└── Jury votes: 67% to uphold challenge (supermajority)
```

## Key Parameters Reference

```
┌─────────────────────────────────────────────────────────────────────────┐
│                      KEY PARAMETERS                                     │
└─────────────────────────────────────────────────────────────────────────┘

TIME
├── Epoch duration:              17,280 blocks (~1 day at 5s blocks)
├── Season duration:             150 epochs (~5 months)
├── Season transition:           7 epochs (~1 week)
├── Review period:               7 epochs
├── Challenge period:            7 epochs
├── Challenge response deadline: 3 epochs
├── Invitation accountability:   150 epochs (1 season)
├── Conviction half-life:        7 epochs (initiatives)
├── Content conv. half-life:     14 epochs (author bonds)
├── Nomination conv. half-life:  3 epochs (retro funding)
├── Ephemeral blog TTL:          7 days
├── Ephemeral forum TTL:         24 hours
└── Archive threshold (forum):   30 days

DREAM ECONOMICS
├── Staking APY:                 10% annual
├── Unstaked decay:              1% per epoch
├── Transfer tax:                3% (burned)
├── Completer share:             90% of initiative budget
├── Treasury share:              10% of initiative budget
├── Min reputation multiplier:   0.10 (floor)
├── External conviction ratio:   50% minimum
├── Max conviction share/member: 33%
├── Rep decay:                   0.5% per epoch
├── Max rep gain/tag/epoch:      50
└── Max tags/initiative:         3

INITIATIVE TIERS
├── Apprentice: 0 rep min,   100 DREAM max,  0.5x mult, rep cap 25
├── Standard:   25 rep min,  500 DREAM max,  1.0x mult, rep cap 100
├── Expert:     100 rep min, 2000 DREAM max, 1.5x mult, rep cap 500
└── Epic:       250 rep min, 10000 DREAM max, 2.0x mult, rep cap 1000

INVITATIONS
├── Minimum stake:               100 micro-DREAM
├── Referral rate:               5% of invitee earnings
├── Referral duration:           1 season
├── Cost multiplier:             1.1x per invitation
└── Burn rate on acceptance:     10%

CHALLENGES
├── Minimum stake:               50 micro-DREAM
├── Anonymous multiplier:        2.5x fee + 1 SPARK escrow
├── Challenger reward:           20% of slashed amount
├── Jury size:                   5 members (odd)
├── Uphold threshold:            67% (supermajority)
└── Minimum juror reputation:    50

CONTENT CHALLENGES (Author Bonds)
├── Max author bond:             1,000 DREAM per content
├── Max stake per member:        10,000 DREAM per content item
├── Slash reward share:          50% to challenger
└── Challenge half-life:         14 epochs

INTERIM COMPENSATION (DREAM)
├── Simple complexity:           50
├── Standard complexity:         150
├── Complex complexity:          400
├── Expert complexity:           1,000
├── Solo expert bonus:           +50%
└── Deadline:                    7 epochs

TRUST LEVELS
├── Provisional: 50 rep + 3 initiatives
├── Established: 200 rep + 10 initiatives
├── Trusted: 500 rep + 1 season
└── Core: 1000 rep + 2 seasons

ANONYMOUS VOTING (x/vote)
├── Tree depth:                  20 (supports ~1M voters)
├── Min voting period:           3 epochs
├── Max voting period:           30 epochs
├── Default voting period:       7 epochs
├── Default quorum:              33%
├── Default threshold:           50% (simple majority)
├── Jury vote threshold:         67% (supermajority)
├── Proof generation:            2-5 seconds (client-side)
├── Proof verification:          2-5 ms (on-chain)
├── Proof size:                  ~200 bytes
├── Sealed/reveal period:        3 epochs
└── TLE threshold:               2/3 of validators

BLOG CONTENT
├── Max title:                   200 chars
├── Max body:                    10,000 chars
├── Max reply:                   2,000 chars
├── Max reply depth:             5 levels
├── Storage cost:                100 uspark/byte
├── Rate limits:                 10 posts, 50 replies, 100 reactions/day
├── Anonymous min trust:         ESTABLISHED (level 2)
└── Anonymous subsidy:           100 SPARK/epoch

FORUM CONTENT
├── Max content:                 10 KB
├── Max reply depth:             10 levels
├── Spam tax:                    1 SPARK
├── Sentinel bond:               100 DREAM
├── Bounty cancel fee:           10%
├── Appeal fee:                  5 SPARK
├── Rate limits:                 50 posts, 100 reactions, 20 flags/day
└── Edit grace period:           5 minutes

COLLECT CURATION
├── Max items/collection:        500
├── Max collaborators:           20
├── Collection deposit:          1 SPARK + 0.1 SPARK/item
├── Curator min bond:            500 DREAM (PROVISIONAL+ trust)
├── Challenge deposit:           250 DREAM
├── Downvote cost:               25 SPARK
├── Endorsement stake:           100 DREAM for 30 days
└── Anonymous max per key:       3 collections

SEASONAL GAMIFICATION
├── Guild creation:              100 DREAM
├── Guild size:                  3-100 members
├── Username cost:               10 DREAM
├── Display name appeal:         100 DREAM
├── Max XP/epoch:                200
├── Retro reward budget:         50,000 DREAM/season
├── Max retro recipients:        20
└── Max nominations/member:      3

REVEAL (Progressive Open-Source)
├── Max tranches:                10
├── Max total valuation:         50,000 DREAM
├── Bond rate:                   10% of valuation
├── Payout holdback:             20% per tranche
├── Min stake:                   100 DREAM
├── Verification threshold:      60%
├── Min verification votes:      3 (scales with stake)
├── Min proposer trust:          ESTABLISHED
├── Staking deadline:            60 epochs
├── Reveal deadline:             14 epochs
├── Verification period:         14 epochs
├── Dispute resolution:          30 epochs
└── Proposal cooldown:           14 epochs (after rejection)
```
