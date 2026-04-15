# Spark Dream Architecture

## System Overview

Spark Dream implements a hierarchical, reputation-based, and market-assisted governance architecture known as **"Three Pillars Governance"**.

This system moves beyond simple token-voting by delegating authority to specialized councils ("Pillars"), ensuring they have guaranteed funding, and holding them accountable via prediction markets ("Futarchy"). The architecture is extended with a reputation-based coordination layer, content platforms for community discourse, identity management, and privacy-preserving anonymous actions via zero-knowledge proofs.

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           SPARK DREAM                                   │
│                        Cosmos SDK Appchain                              │
│                                                                         │
│  15 custom modules · Dual tokens (SPARK/DREAM) · Shielded execution     │
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
│  Trust Tree, ZK Keys       Nominations                                  │
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
│  Pin/Hide System         Appeals, Archival        Endorsements          │
│  Shield-Aware            Shield-Aware             Shield-Aware          │
└─────────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                       IDENTITY & PRIVACY LAYER                          │
├─────────────────────────────────────────────────────────────────────────┤
│  x/name              x/session              x/shield                    │
│    │                   │                      │                         │
│  Human-Readable      Session Keys           Unified Privacy Layer       │
│  Names               Scoped Delegation      MsgShieldedExec (single     │
│  Dispute Resolution  Integrated Fee           entry for all anon ops)   │
│  Scavenging          Delegation             ZK Proof Verification       │
│                      Replaces authz/        TLE Threshold Encryption    │
│                        feegrant             Module-Paid Gas             │
│                                             Centralized Nullifiers      │
└─────────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                         FEDERATION LAYER                                │
├─────────────────────────────────────────────────────────────────────────┤
│  x/federation                                                           │
│    │                                                                    │
│  Cross-Chain Content Exchange    Identity Linking (IBC + bridges)        │
│  Content Verification (verifiers)Reputation Bridging (IBC)               │
│  ActivityPub / AT Protocol       Bridge + Verifier Accountability        │
│    Bridges (off-chain relayers)  Sovereignty-First (bilateral only)      │
│  x/split Compensation (SPARK)    No cross-chain tokens (SPARK/DREAM)     │
└─────────────────────────────────────────────────────────────────────────┘
```

## Module Inventory

| Module | Messages | Queries | EndBlocker | BeginBlocker | Purpose |
|--------|----------|---------|------------|--------------|---------|
| x/commons | 18 | 9 | 2 phases | — | Three Pillars governance, anonymous proposals |
| x/split | 1 | 3 | — | BeginBlock | Automated revenue distribution |
| x/futarchy | 7 | 4 | Yes | — | LMSR prediction markets, elastic tenure |
| x/ecosystem | 2 | 1 | — | — | Governance-gated ecosystem treasury |
| x/rep | 31 | 38 | 12 phases | — | Reputation, DREAM, trust tree, ZK keys |
| x/season | 43 | 72 | — | 3 phases | Seasons, XP, guilds, quests, retro funding |
| x/reveal | 10 | 11 | Yes | — | Progressive open-source, tranched funding |
| x/blog | 16 | 11 | 1 phase | — | Blog posts, replies, reactions |
| x/forum | 49 | 73 | 4 phases | — | Discussion threads, moderation, bounties |
| x/collect | 29 | 25 | 6 phases | — | Curated collections, curation, endorsements |
| x/name | 8 | 6 | — | Yes | Human-readable identity registry |
| x/shield | 5 | 17 | Yes | Yes | Shielded execution, ZK proofs, TLE, DKG |
| x/session | 4 | 4 | 1 phase | — | Session keys, scoped delegation, fee delegation |
| x/federation | 27 | 18 | 13 phases | — | Cross-chain content, reputation bridging, identity linking, verification |
| x/common | — | — | — | — | Shared types (tags, flags, moderation) |

## Core Module Architecture

### x/commons (The Orchestrator)

**Purpose:** Central engine for the "Three Pillars" governance, native proposal system, and anonymous governance.

**Mechanism:** Native `Group` structure with built-in proposal lifecycle (submit → vote → execute). Replaced x/group dependency.

**Key Logic (MsgRegisterGroup):**
- **Hierarchy:** Enforces parent-child trust chains (Gov → Council → Committee) and prevents cyclic dependencies.
- **Funding:** Automatically registers the group with `x/split` to receive `FundingWeight` share of revenue.
- **Accountability:** Initializes "Elastic Tenure" by triggering the first "Confidence Vote" market via `x/futarchy`.
- **Permissions:** Uses `AllowedMessages` to restrict council powers (e.g., only Technical Council can run `MsgSoftwareUpgrade`).

**Native Proposals:** `SubmitProposal` → `VoteProposal` → `ExecuteProposal` with early acceptance when threshold met, configurable `MinExecutionPeriod`.

**Anonymous Governance:** `SubmitAnonymousProposal` and `AnonymousVoteProposal` are shield-aware messages routed via x/shield's `MsgShieldedExec`, enabling anonymous council proposals and votes.

**Messages (18):** `register_group`, `renew_group`, `update_group_members`, `update_group_config`, `delete_group`, `spend_from_commons`, `policy_permissions` (create/update/delete), `force_upgrade`, `emergency_cancel_gov_proposal`, `veto_group_proposals`, `submit_proposal`, `vote_proposal`, `execute_proposal`, `submit_anonymous_proposal`, `anonymous_vote_proposal`, `update_params`

**Queries (9):** `params`, `get_policy_permissions`, `list_policy_permissions`, `get_group`, `list_groups`, `get_council_members`, `get_proposal`, `list_proposals`, `get_proposal_votes`

**EndBlocker (2 phases):**
1. Market trigger queue — schedules and fires futarchy confidence vote markets for groups
2. Proposal finalization — tallies votes for proposals past their voting deadline and sets status to ACCEPTED or REJECTED

### x/split (The Treasury)

**Purpose:** Automated revenue distribution engine.

**Mechanism:**
- **Shares:** Manages a registry of `Share` objects (Address + Weight).
- **Distribution Loop:** Runs in `BeginBlock` (via [x/split/module/module.go](x/split/module/module.go)). It iterates through the funds in the `x/distribution` module account (Community Pool).
- **Calculation:** Distributes funds to shareholders proportional to their weight: `(Balance × ShareWeight) / TotalWeight`.
- **Optimization:** Includes a "dust protection" check (in [x/split/keeper/split.go](x/split/keeper/split.go)) to skip insignificant transfers and save gas.

**Integration:** `x/commons` registers council policy addresses here, ensuring they automatically receive their allocated funding (e.g., 50% for Commons, 30% for Technical, 20% for Ecosystem).

**Messages (1):** `update_params`

**Queries (3):** `params`, `get_share`, `list_share`

### x/futarchy (The Accountability Engine)

**Purpose:** Implements "Elastic Tenure" via prediction markets.

**Mechanism:**
- **LMSR AMM:** Logarithmic Market Scoring Rule automated market maker with gas-metered exp/ln calculations.
- **Confidence Markets:** The system automatically creates "Confidence Vote" prediction markets for each council.
- **Elastic Tenure Hook:** When a market resolves, a hook in `x/commons` adjusts the council's term:
  - **High Confidence (YES):** Term extended by **20%**.
  - **Low Confidence (NO):** Term slashed by **50%** (potentially triggering immediate re-election).
- **Two-tier authorization:** Governance + ops committee for market management.

**Messages (7):** `create_market`, `trade`, `redeem`, `withdraw_liquidity`, `cancel_market`, `update_params`, `update_operational_params`

**Queries (4):** `params`, `get_market`, `list_market`, `get_market_price`

**EndBlocker:** Resolves expired prediction markets based on pool sizes (YES/NO/INVALID) and calls `AfterMarketResolved` hooks to adjust council tenure via x/commons.

### x/ecosystem (Ecosystem Treasury)

**Purpose:** Independent governance-gated spending from the ecosystem module account.

Separate from the `x/split` distribution pipeline, this module holds funds for ecosystem grants and partnerships, spending only via `x/gov` authority.

**Messages (2):** `spend`, `update_params`

## Coordination Layer

### x/rep (Reputation & Coordination)

**Purpose:** Primary module for the reputation-based task system, member lifecycle, and DREAM token economics.

**Key Features:**
- **Member lifecycle:** Invitation-based membership with 5 trust levels and "zeroing" instead of banning
- **DREAM token:** Minting via initiative completion, burning via slashing/decay/transfer tax, limited transfers (tips/gifts only)
- **Reputation:** Per-tag scores with seasonal resets, anti-gaming caps (0.5%/epoch decay, 33% max conviction share, 50 rep/tag/epoch cap)
- **Projects & initiatives:** Council-approved budgets, tiered initiatives (Apprentice/Standard/Expert/Epic), conviction-based completion
- **Conviction staking:** Time-weighted engagement (half-life: 7 epochs), 50% external conviction requirement
- **Challenges:** Named + anonymous (via x/shield, no DREAM stake), jury resolution (5 members, 67% supermajority)
- **Content challenges:** Cross-module author bonds, slashable on moderation (50% to challenger)
- **Interim compensation:** Fixed-rate delegated duties (Simple 50 → Expert 1,000 DREAM)
- **MasterChef staking rewards:** Epoch-based pool distribution (10% APY)
- **Trust tree:** Persistent KV-based sparse Merkle tree (depth 20) of member ZK public keys + trust levels, rebuilt incrementally via EndBlocker dirty-member tracking
- **ZK key registration:** Members register ZK public keys (`MsgRegisterZkPublicKey`) stored on Member proto (field 28: `bytes zk_public_key`), used as trust tree leaves via `MiMC(zk_public_key, trust_level)`

**Messages (31):** `invite_member`, `accept_invitation`, `transfer_dream`, `propose_project`, `approve_project_budget`, `cancel_project`, `create_initiative`, `assign_initiative`, `submit_initiative_work`, `approve_initiative`, `complete_initiative`, `abandon_initiative`, `stake`, `unstake`, `claim_staking_rewards`, `compound_staking_rewards`, `create_challenge`, `respond_to_challenge`, `submit_juror_vote`, `submit_expert_testimony`, `challenge_content`, `respond_to_content_challenge`, `create_interim`, `assign_interim`, `submit_interim_work`, `approve_interim`, `complete_interim`, `abandon_interim`, `register_zk_public_key`, `update_params`, `update_operational_params`

**Queries (38):** `params`, member CRUD (2), invitation CRUD (2), project CRUD (2), initiative CRUD (2), stake CRUD (2), challenge CRUD (2), jury_review CRUD (2), interim CRUD (2), interim_template CRUD (2), plus specialized: `members_by_trust_level`, `invitations_by_inviter`, `interims_by_assignee`, `interims_by_type`, `interims_by_reference`, `projects_by_council`, `initiatives_by_project`, `initiatives_by_assignee`, `available_initiatives`, `stakes_by_staker`, `stakes_by_target`, `initiative_conviction`, `challenges_by_initiative`, `reputation`, `pending_stake_rewards`, `get_member_stake_pool`, `get_tag_stake_pool`, `get_project_stake_info`, `content_conviction`, `author_bond`, content_challenge CRUD (2), `content_challenges_by_target`, `content_by_initiative`

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
- **Retroactive public goods funding:** Nomination window (~5 epochs before season end), conviction-weighted, budget = 25% of initiative minting (10K-75K range, treasury-funded first), max 20 recipients, min ESTABLISHED trust to nominate

**Messages (43):** Profile management (3), guild operations (16), quest operations (5), governance-created content (9), nominations (3), display name moderation (4), season management (5), `update_params`, `update_operational_params`

**Queries (72):** CRUD queries for seasons, season snapshots, member season snapshots, member profiles, member registrations, achievements, titles, season title eligibility, guilds, guild memberships, guild invites, quests, member quest progress, epoch XP trackers, vote XP records, forum XP cooldowns, display name moderations, display name report stakes, display name appeal stakes (42 CRUD). Plus specialized: `params`, `current_season`, `season_by_number`, `season_stats`, `member_by_display_name`, `member_season_history`, `member_xp_history`, `achievements`, `member_achievements`, `titles`, `member_titles`, `guild_by_id`, `guilds_list`, `guilds_by_founder`, `guild_members`, `member_guild`, `guild_invites`, `member_guild_invites`, `quests_list`, `quest_by_id`, `quest_chain`, `member_quest_status`, `available_quests`, `get_nomination`, `list_nominations`, `list_nominations_by_creator`, `list_nomination_stakes`, `list_retro_reward_history`, `get_season_transition_state`, `get_transition_recovery_state`, `get_next_season_info` (30 specialized).

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

**Messages (10):** `propose`, `approve`, `reject`, `cancel`, `stake`, `withdraw`, `reveal`, `verify`, `resolve_dispute`, `update_params`

**Queries (11):** `params`, `contribution`, `contributions`, `contributions_by_contributor`, `contributions_by_status`, `tranche`, `tranche_tally`, `tranche_stakes`, `stake_detail`, `stakes_by_staker`, `votes_by_voter`

**EndBlocker:** Processes deadlines for staking (cancels contribution), reveal (slashes bond), verification (auto-tallies), and dispute phases (auto-REJECT on council timeout).

## Content & Discussion Layer

### x/blog (Content Management)

**Purpose:** On-chain blog posts with threaded replies, reactions, and shield-aware anonymous posting via x/shield.

**Key Features:**
- **Posts & replies:** CRUD with configurable length constraints (title 200, body 10,000, reply 2,000 chars), max reply depth of 5
- **Reactions:** Like, Insightful, Disagree, Funny with per-reaction fees (50 uspark)
- **Rate limiting:** Max 10 posts/day, 50 replies/day, 100 reactions/day per address
- **Ephemeral content:** Non-member posts expire after TTL (7 days default); conviction renewal extends TTL if conviction ≥ threshold
- **Storage fees:** 100 uspark/byte, charged to submitter
- **Pin/hide system:** Trust-level-gated pinning (CORE trust for blog), owner/admin hiding
- **Shield-aware:** Implements `ShieldAware` interface; anonymous posts/replies/reactions routed via x/shield's `MsgShieldedExec`

**Messages (16):** Post CRUD (3), reply CRUD (3), hide/unhide post (2), hide/unhide reply (2), pin post (1), pin reply (1), react (1), remove reaction (1), `update_params`, `update_operational_params`

**Queries (11):** `params`, `show_post`, `list_post`, `show_reply`, `list_replies`, `list_posts_by_creator`, `reaction_counts`, `user_reaction`, `list_reactions`, `list_reactions_by_creator`, `list_expiring_content`

**EndBlocker (1 phase):**
1. TTL expiry — upgrades to permanent if creator becomes member, conviction renewal if conviction ≥ threshold, or tombstones expired content

### x/forum (Decentralized Discussion)

**Purpose:** Full-featured discussion platform with hierarchical content, dual-token sentinel moderation, bounties, and economic incentives.

**Key Features:**
- **Hierarchical content:** Governance-controlled categories, member-created tags with usage tracking and expiry
- **Sentinel moderation:** DREAM-bonded sentinels (100 DREAM commit) with hide/report authority, accountability via challenges
- **Content lifecycle:** Ephemeral TTL (24h) for non-members, permanent for members, conviction renewal for initiative-linked content
- **Bounties:** Thread-attached DREAM bounties with assignment, cancellation (10% fee), and expiry
- **Tag budgets:** Governance-funded tag-scoped budgets for rewarding quality contributions
- **Shield-aware:** Implements `ShieldAware` interface; anonymous threads/replies/reactions routed via x/shield's `MsgShieldedExec`
- **Appeals:** Jury-based appeals for hide, lock, move, and governance actions (5 SPARK fee, 14-day deadline)
- **Thread operations:** Lock, freeze, move, archive, pin/unpin, follow/unfollow
- **Member reports:** Multi-step reporting with cosigning, defense, and resolution
- **Rate limiting:** 50 posts/day, 100 reactions/day, 20 downvotes/day, 20 flags/day

**Messages (49):** Post operations (6), moderation (6), thread control (5), reactions (2), reply management (5), bounties (5), tag budgets (5), tags (2), appeals (4), thread ops (3), emergency (2), sentinel (2), member reports (4), governance (1), `update_params`, `update_operational_params`

**Queries (73):** CRUD queries for posts, categories, tags, reserved tags, rate limits, reaction limits, sentinel activity, hide records, thread lock/move records, post flags, bounties, tag budgets, tag budget awards, thread metadata, thread follows, thread follow counts, archive metadata, tag reports, member salvation status, jury participation, member reports, member warnings, gov action appeals (46 CRUD). Plus specialized: posts, thread, categories, user_posts, sentinel_status, sentinel_bond_commitment, archive_cooldown, tag_exists, tag_reports, forum_status, appeal_cooldown, member_reports, member_warnings, member_standing, pinned_posts, locked_threads, thread_lock_status, top_posts, thread_followers, user_followed_threads, is_following_thread, bounty_by_thread, active_bounties, user_bounties, bounty_expiring_soon, tag_budget_by_tag, tag_budgets, tag_budget_awards, post_flags, flag_review_queue, gov_action_appeals, params (27 specialized).

**EndBlocker (4 phases):**
1. Prune expired ephemeral posts (max 100/block, conviction renewal check for initiatives)
2. Expire hidden posts (max 50/block, soft-delete after configured hidden expiration)
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
- **Shield-aware:** Implements `ShieldAware` interface; anonymous collections/reactions routed via x/shield's `MsgShieldedExec`
- **Pinning:** ESTABLISHED+ trust, max 10 pins/day
- **On-chain references:** Validate content existence in x/blog and x/forum

**Messages (29):** Collection CRUD (3), items (5), collaborators (3), curation (4), sponsorship (3), endorsement (2), reactions (3), moderation (3), pin (1), `update_params`, `update_operational_params`

**Queries (25):** `params`, `collection`, `collections_by_owner`, `public_collections`, `public_collections_by_type`, `collections_by_collaborator`, `item`, `items`, `items_by_owner`, `collaborators`, `curator`, `active_curators`, `curation_summary`, `curation_reviews`, `curation_reviews_by_curator`, `sponsorship_request`, `sponsorship_requests`, `content_flag`, `flagged_content`, `hide_record`, `hide_records_by_target`, `pending_collections`, `endorsement`, `collections_by_content`, `collection_conviction`

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

**Messages (8):** `register_name`, `update_name`, `set_primary`, `file_dispute`, `contest_dispute`, `resolve_dispute`, `update_params`, `update_operational_params`

**Queries (6):** `params`, `resolve`, `reverse_resolve`, `names`, `get_dispute`, `list_dispute`

**BeginBlocker:** Auto-resolve expired disputes.

### x/shield (Unified Privacy Layer)

**Purpose:** Single entry point for all anonymous operations across all modules. Owns ZK proof verification, TLE threshold encryption, centralized nullifier management, and module-paid gas.

**Key Features:**
- **Single entry point:** `MsgShieldedExec` wraps any registered inner message for anonymous execution
- **Module-paid gas:** Shield module account pays tx fees; submitters need zero balance (auto-funded from community pool via BeginBlocker)
- **ZK proof verification:** PLONK over BN254, verification keys stored on-chain, proof of trust tree membership
- **Two execution modes:** Immediate (low latency, content visible) and Encrypted Batch (TLE + batching, maximum privacy)
- **Centralized nullifiers:** Per-domain, per-scope nullifier tracking replaces per-module stores
- **TLE infrastructure:** Distributed Key Generation (DKG) ceremony, master public key, Shamir secret sharing, epoch-based decryption
- **Rate limiting:** Per-identity rate limiting (based on rate-limit nullifier) prevents gas abuse
- **ShieldAware interface:** Modules opt-in via `IsShieldCompatible()` for double-gate security (governance whitelist + module acceptance)
- **DKG state machine:** Auto-triggers when sufficient validators bonded, IDLE → REGISTERING → CONTRIBUTING → ACTIVE lifecycle
- **Validator liveness:** TLE miss tracking per epoch, jailing for non-participation
- **Operation registration:** Governance-gated `MsgRegisterShieldedOp` / `MsgDeregisterShieldedOp` to whitelist anonymous message types

**Messages (5):** `shielded_exec`, `register_shielded_op`, `deregister_shielded_op`, `trigger_dkg`, `update_params`

**Queries (17):** `params`, `shielded_op`, `shielded_ops`, `module_balance`, `nullifier_used`, `day_funding`, `shield_epoch`, `pending_ops`, `pending_op_count`, `tle_master_public_key`, `tle_key_set`, `verification_key`, `tle_miss_count`, `decryption_shares`, `identity_rate_limit`, `dkg_state`, `dkg_contributions`

**BeginBlocker:**
- Auto-fund from community pool (up to daily cap `max_funding_per_day`)
- Advance DKG state machine (IDLE → REGISTERING → CONTRIBUTING → ACTIVE)
- Detect validator set drift and re-trigger DKG if needed

**EndBlocker:**
- Shield epoch advancement (when `shield_epoch_interval` blocks pass)
- Encrypted batch processing (decrypt, shuffle, verify, execute)
- Carry-over stale batches past `max_pending_epochs`
- TLE liveness checks (track validator miss counts, jail violators)
- Prune old nullifiers, decryption keys/shares, rate limits, day fundings

**ABCI Extensions:**
- `ExtendVote`: Validators include DKG contributions and decryption shares in vote extensions
- `PrepareProposal`: Aggregates DKG/decryption data into `InjectedDKGData` pseudo-transaction at block position 0
- `PreBlocker`: Processes injected DKG data before normal block execution

**Ante Handlers:**
- `ShieldGasDecorator`: Intercepts `MsgShieldedExec`, deducts gas from shield module account
- `SkipFeeDecorator`: Skips normal fee processing for shielded messages

### x/session (Session Keys)

**Purpose:** Scoped, time-limited transaction delegation with integrated fee delegation. Purpose-built replacement for `x/authz` + `x/feegrant`.

**Key Features:**
- **Session lifecycle:** Granter creates session for ephemeral grantee key → grantee signs `MsgExecSession` → granter pays gas → session expires or is revoked
- **Bounded allowlist:** Two-tier model — ceiling (`max_allowed_msg_types`, upgrade-only) and active list (governance can shrink, ops committee can restore within ceiling)
- **Integrated fee delegation:** `spend_limit` on each session, `SessionFeeDecorator` ante handler overrides fee payer
- **Non-recursive:** `MsgExecSession` cannot contain another `MsgExecSession` — eliminates recursion attacks
- **Leaf module:** Depends only on x/bank, x/auth, msg router. No cycle risk.

**Messages (4):** `create_session`, `revoke_session`, `exec_session`, `update_params`

**Queries (4):** `params`, `get_session`, `sessions_by_granter`, `sessions_by_grantee`

**EndBlocker (1 phase):**
1. Prune expired sessions (walk `SessionsByExpiration` index)

## Federation Layer

### x/federation (Cross-Chain Exchange)

**Purpose:** Enables Spark Dream chains to exchange content, verify reputation, and link identities with other Spark Dream chains (via IBC) and external social protocols (ActivityPub, AT Protocol) via off-chain bridges.

**Key Features:**
- **Three layers:** On-chain primitives (peer registry, policies) → IBC protocol (chain-to-chain, trustless) → off-chain bridges (ActivityPub/AT Protocol, staked operators)
- **Sovereignty first:** Bilateral relationships only, no supergovernment, no cross-chain tokens, no binding reputation, unilateral suspend/remove
- **Peer management:** Commons Council registers/removes peers; Operations Committee manages policies and bridge operators
- **Content federation:** Inbound content stored in x/federation (leaf module — content modules unaware); per-peer content type allowlists, inbound + outbound rate limits, moderation
- **Content verification:** Bridge content enters as PENDING_VERIFICATION; independent community verifiers (DREAM-bonded, ESTABLISHED+ trust) fetch source content and confirm hash match. Challenges use two-phase resolution: anonymous community members submit hashes via x/shield (ZK-proven membership, scoped nullifiers) for fast quorum-based auto-resolution; human jury via x/rep as fallback. IBC content verified by light client (no verifier needed).
- **Reputation bridging:** IBC attestation model with heavy discounting (default: 50% discount, capped at PROVISIONAL equivalent, 30-day TTL)
- **Identity linking:** Two-phase IBC challenge-response proving key ownership; bridge-verified links for external protocols
- **Bridge accountability:** SPARK-staked operators, 14-day unbonding period, slashable (burned, not redistributed), self-unbonding, stake top-up, session key support via x/session
- **Creator-signed outbound:** `MsgFederateContent` requires the content creator's signature (or x/session delegation), preventing relayers from fabricating content
- **Compensation:** Bridge operators and verifiers compensated via x/split (SPARK from Community Pool), weighted by verified submissions and verification accuracy respectively
- **Token separation:** Bridge operators stake SPARK only; verifiers bond DREAM only. DREAM is never transferred cross-chain.

**Messages (27):** `register_peer`, `remove_peer`, `suspend_peer`, `resume_peer`, `update_peer_policy`, `register_bridge`, `revoke_bridge`, `slash_bridge`, `update_bridge`, `unbond_bridge`, `top_up_bridge_stake`, `link_identity`, `unlink_identity`, `confirm_identity_link`, `submit_federated_content`, `federate_content`, `attest_outbound`, `moderate_content`, `request_reputation_attestation`, `bond_verifier`, `unbond_verifier`, `verify_content`, `challenge_verification`, `submit_arbiter_hash`, `escalate_challenge`, `update_params`, `update_operational_params` (arbiter hash also submittable anonymously via x/shield)

**Queries (18):** `params`, `get_peer`, `list_peers`, `get_peer_policy`, `get_bridge_operator`, `list_bridge_operators`, `get_federated_content`, `list_federated_content`, `get_identity_link`, `list_identity_links`, `resolve_remote_identity`, `get_pending_identity_challenge`, `list_pending_identity_challenges`, `get_reputation_attestation`, `list_outbound_attestations`, `get_verifier`, `list_verifiers`, `get_verification_record`

**EndBlocker (13 phases):**
1. Prune expired federated content
2. Prune expired reputation attestations
3. Prune expired unverified identity links
4. Prune expired identity challenges
5. Release unbonded bridge stakes
6. Expire unverified content (PENDING_VERIFICATION → HIDDEN after verification_window)
7. Release verifier bond commitments (challenge_window expired without challenge)
8. Expire arbiter resolution windows (no quorum → escalate to jury)
9. Finalize auto-resolutions (escalation window expired → verdict final)
10. Process peer removal queue (cursor-based)
11. Verifier epoch rewards (DREAM minting, auto-bonding, counter reset)
12. Bridge operator monitoring (inactivity + stake warnings)
13. Clean stale rate limit counters (inbound + outbound)

**IBC Application:**
- Port: `federation`, Channel: UNORDERED, Version: `federation-1`
- Packet types: `ReputationQueryPacket`, `ContentPacket`, `IdentityVerificationPacket`, `IdentityVerificationConfirmPacket`

## Shielded Execution System

x/shield provides a unified shielded execution layer that replaces per-module anonymous messaging. Any module can register operations for shielded execution, and x/shield handles all ZK proof verification, nullifier management, and gas payment.

### How It Works

```
CLIENT-SIDE                                    ON-CHAIN

1. Register ZK public key (one-time)
   MsgRegisterZkPublicKey(zk_pub_key)    ──▶ x/rep stores on Member proto
                                              Trust tree rebuilt in EndBlocker

2. Fetch trust tree root                  ◀── x/rep provides current root
   + Merkle proof for leaf                    (persistent KV sparse Merkle tree)

3. Compute nullifier
   null = MiMC(domain, secretKey, scope)

4. Generate ZK proof (~2-5s)              ── Proves:
   Public:  merkleRoot, nullifier,            - "I know a secretKey whose publicKey
            minTrustLevel, scope                 is in the tree at this trust level"
   Private: secretKey, trustLevel,            - "This nullifier is correctly derived"
            merklePath, pathIndices            - "My trust level ≥ minimum required"

5. Choose execution mode:

   IMMEDIATE MODE:
   ──────────────
   Submit MsgShieldedExec with:          ──▶ x/shield verifies proof (~2-5ms)
     inner_message (cleartext)                Checks nullifier not used
     proof, nullifier, merkle_root            Checks rate limit
     exec_mode = IMMEDIATE                    Dispatches inner msg via router
                                              Target module executes (shield = sender)

   ENCRYPTED BATCH MODE:
   ─────────────────────
   Encrypt (inner_msg + proof) with      ──▶ x/shield stores in pending queue
     TLE master public key                    Validates payload size
   Submit MsgShieldedExec with:               Checks pending nullifier dedup
     encrypted_payload (ciphertext)           At epoch boundary:
     nullifier (cleartext, for dedup)           Validators submit decryption shares
     target_epoch = current epoch               Keys reconstructed (2/3 threshold)
     exec_mode = ENCRYPTED_BATCH                Batch decrypted and shuffled
                                                Proofs verified, ops executed
```

### Nullifier Domains

Each shielded operation type registers a unique domain. x/shield manages all nullifiers centrally:

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
| 11 | x/commons | Anonymous proposal | Current epoch | 1 per epoch |
| 12 | x/commons | Anonymous vote | proposal_id | 1 per proposal |
| 13 | x/rep | Anonymous challenge | Current epoch | 1 per epoch |

### Module-Paid Gas System

x/shield eliminates the need for relay addresses and per-module subsidy budgets. Instead:

- **BeginBlocker** auto-funds the shield module account from the community pool (up to `max_funding_per_day`)
- **ShieldGasDecorator** intercepts `MsgShieldedExec` at the ante handler stage and pays gas from the shield module account
- **Submitters need zero balance** — no fee payment patterns to correlate with identity
- **Per-identity rate limiting** prevents gas abuse (configurable `max_execs_per_identity_per_epoch`)

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
┌────────┐┌──────────────┐
│x/season││  x/common    │
│        ││  (types)     │
│Seasons │└──────────────┘
│XP/Guild│       │
│Quests  │       │
│Retro $ │       ▼
└────────┘┌──────────────┐
          │  x/forum     │
          │  (tags,      │
          │   TagKeeper) │
          └──────────────┘

    ┌──────────────────────┐
    ▼          ▼           ▼
┌────────┐┌────────┐┌──────────┐
│x/blog  ││x/forum ││x/collect │
│        ││        ││          │
│Posts   ││Threads ││Collections│
│Replies ││Bounties││Curation  │
│Shield- ││Shield- ││Shield-   │
│Aware   ││Aware   ││Aware     │
└────────┘└────────┘└──────────┘

          ┌──────────────────────┐
          │      x/shield        │
          │  (Leaf Dependency)   │
          │                      │
          │ Shielded Execution   │
          │ ZK Proof Verification│
          │ TLE / DKG            │
          │ Centralized Nullifiers│
          │ Module-Paid Gas      │
          └──────────────────────┘
          Depends on: x/rep (trust tree),
          x/distribution (funding),
          x/staking, x/slashing (validators)

          ┌──────────────────────┐
          │     x/session        │
          │  (Leaf Dependency)   │
          │                      │
          │ Session Keys         │
          │ Scoped Delegation    │
          │ Fee Delegation       │
          │ Non-Recursive Exec   │
          └──────────────────────┘
          Depends on: x/bank, x/auth,
          msgServiceRouter

          ┌──────────────────────┐
          │    x/federation      │
          │  (Leaf Dependency)   │
          │                      │
          │ Cross-Chain Content  │
          │ Content Verification │
          │ Reputation Bridging  │
          │ Identity Linking     │
          │ Bridge Operators     │
          │ IBC Application      │
          └──────────────────────┘
          Depends on: x/commons (auth),
          x/rep (reputation, verifier
          DREAM bonds, jury), x/name,
          x/bank (bridge stakes),
          x/shield (anonymous arbiter
          resolution via ZK proofs),
          ibc-go
          Depended on by: x/split
          (read-only weight queries)
```

### Cross-Module Keeper Wiring (app.go)

Many keepers are wired manually after `depinject.Inject()` to break cyclic dependencies:

```
x/commons    ← SetGovKeeper(gov), SetRouter(msgServiceRouter)

x/futarchy   ← SetCommonsKeeper(commons), SetHooks(commons)

x/season     ← SetRepKeeper(rep), SetNameKeeper(name), SetCommonsKeeper(commons)
             ← SetBlogKeeper(blog), SetForumKeeper(forum), SetCollectKeeper(collect)

x/blog       ← SetRepKeeper(rep)

x/collect    ← SetRepKeeper(rep)

x/rep        ← SetSeasonKeeper(season)  [via shared lateKeepers pointer]
             ← SetTagKeeper(forum)      [via lateKeepers — forum implements TagKeeper]

x/split      ← SetDistrKeeper(distr)    [via adapter]

x/shield     ← SetRepKeeper(rep), SetDistrKeeper(distr)
             ← SetSlashingKeeper(slashing), SetStakingKeeper(staking)
             ← SetRouter(msgServiceRouter)
             ← RegisterShieldAwareModule(blog, forum, collect, rep, commons, federation)

x/session    ← (no late wiring needed — leaf module, all deps via depinject)

x/federation ← SetCommonsKeeper(commons), SetRepKeeper(rep)
             ← SetNameKeeper(name), SetShieldKeeper(shield)
```

The `lateKeepers` pattern in x/rep and x/commons uses a shared pointer struct so that `Set*Keeper()` mutations are visible to all keeper value copies (including the one inside AppModule's msgServer). x/shield, x/session, and x/federation are **leaf dependencies** — nothing depends on them, so they have no cycle risk and all keepers are wired via `Set*Keeper()` after depinject.

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
    │           0.2%/epoch decay
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
    ├── Seasonal staking pool (25,000 DREAM/season, pro-rata, self-adjusting APY)
    ├── Interim compensation (50-1000 DREAM, treasury-funded first)
    ├── Retroactive public goods funding (25% of initiative minting, 10K-75K, treasury-funded first)
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
(anonymous: no DREAM stake, module-paid gas via x/shield)
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
│ - Budget: 25% of initiative minting (10K-75K, treasury-funded first)     │
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

## Shielded Execution (x/shield)

x/shield is the unified privacy layer that replaces the former x/vote module and per-module anonymous messaging. It provides a single `MsgShieldedExec` entry point for all anonymous operations, with centralized ZK proof verification, nullifier management, TLE threshold encryption, and module-paid gas.

### Why Shielded Execution?

| Problem | Solution |
|---------|----------|
| Vote buying/coercion | Votes can't be proven to third parties |
| Social pressure | No one knows how you voted or posted |
| Jury intimidation | Challenge jurors act anonymously |
| Fee-based deanonymization | Module-paid gas eliminates fee payment patterns |
| Per-module complexity | Single entry point replaces per-module anonymous messages |
| Timing correlation | Encrypted batch mode hides submission timing |

### Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    SHIELDED EXECUTION ARCHITECTURE                      │
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
     │   Distributed to    │         │   Stored on-chain   │
     │   member clients    │         │   by circuit_id     │
     └─────────────────────┘         └─────────────────────┘

REGISTRATION PHASE (Per-member, one-time)
──────────────────────────────────────────────────────────────────────────
     Client                              Chain
        │                                   │
        │  Generate secretKey (random)      │
        │  Compute publicKey = MiMC(sk)     │
        │  Store secretKey securely         │
        │                                   │
        │  ─MsgRegisterZkPublicKey──────▶   │
        │     (zk_public_key bytes)         │
        │                                   │
        │                          x/rep stores on Member proto
        │                          Trust tree rebuilt in EndBlocker
        │                          Leaf = MiMC(zk_public_key, trust_level)
        │                                   │

DKG CEREMONY (Validator setup for encrypted batch mode)
──────────────────────────────────────────────────────────────────────────
     Validators                          Chain
        │                                   │
        │                          DKG auto-triggers when
        │                          min_tle_validators bonded
        │                                   │
        │  IDLE → REGISTERING               │
        │  Submit pub key via VoteExtension ▶│
        │                                   │
        │  REGISTERING → CONTRIBUTING       │
        │  Submit DKG shares via VoteExt  ──▶│
        │                                   │
        │  CONTRIBUTING → ACTIVE            │
        │                          Master public key derived
        │                          Shamir shares distributed
        │                          TLE ready for use
        │                                   │

SHIELDED EXECUTION
──────────────────────────────────────────────────────────────────────────
     Member                              Chain (x/shield)
        │                                   │
        │  ◀── Query trust tree root ────   │ (from x/rep)
        │  ◀── Query Merkle proof ──────    │
        │                                   │
        │  Compute nullifier:               │
        │  null = MiMC(domain, sk, scope)   │
        │                                   │
        │  Generate ZK proof proving:       │
        │  ┌─────────────────────────────┐  │
        │  │ PUBLIC (on-chain):         │  │
        │  │ - merkleRoot               │  │
        │  │ - nullifier                │  │
        │  │ - minTrustLevel            │  │
        │  │ - scope                    │  │
        │  ├─────────────────────────────┤  │
        │  │ PRIVATE (never revealed):  │  │
        │  │ - secretKey                │  │
        │  │ - trustLevel               │  │
        │  │ - merklePath + indices     │  │
        │  └─────────────────────────────┘  │
        │                                   │
        │  ── MsgShieldedExec ──────────▶   │
        │     (inner_message or             │
        │      encrypted_payload)           │
        │                                   │
        │                          ShieldGasDecorator pays gas
        │                          Verify ZK proof (~2-5 ms)
        │                          Check nullifier not used
        │                          Check rate limit
        │                          ShieldAware module check
        │                          Dispatch inner message
        │                                   │

EXECUTION MODES:
├── IMMEDIATE:        Low latency, content visible, identity hidden
└── ENCRYPTED_BATCH:  TLE-encrypted, batched, shuffled — maximum privacy
```

### Threshold Timelock Encryption (TLE)

For encrypted batch mode, x/shield uses validator-based threshold encryption:

- **DKG ceremony:** Automated state machine (IDLE → REGISTERING → CONTRIBUTING → ACTIVE) via ABCI vote extensions
- **Master public key:** Derived from validator DKG contributions, used by clients to encrypt payloads
- **Threshold:** 2/3 of validators must submit decryption shares per epoch
- **Batch execution:** EndBlocker decrypts, shuffles, verifies, and executes queued operations
- **Liveness tracking:** Per-epoch validator participation monitoring, violators jailed via x/slashing
- **Validator drift detection:** Auto-triggers new DKG round when validator set changes significantly

### Privacy Guarantees

```
IMMEDIATE MODE (visible on-chain):
├── Merkle root (current or previous epoch)
├── Nullifier (domain-scoped hash)
├── Inner message content (cleartext)
├── ZK proof bytes
└── Shield module as sender (not user address)

ENCRYPTED BATCH MODE (visible on-chain):
├── Merkle root
├── Nullifier (for dedup only)
├── Encrypted payload (ciphertext until epoch boundary)
├── Target epoch
└── After decryption: execution order shuffled

PRIVATE (never revealed in either mode):
├── User's address and secret key
├── User's position in trust tree
├── Merkle proof path
├── Link between user and their action
└── In batch mode: submission timing within epoch
```

### Registered Operation Types

Governance controls which message types can be executed via x/shield:

```
┌────────────────────────────┬────────────────────────────────────────────┐
│ Module                     │ Shielded Operations                        │
├────────────────────────────┼────────────────────────────────────────────┤
│ x/blog                     │ CreatePost, CreateReply, React             │
│ x/forum                    │ CreatePost, CreateReply, React             │
│ x/collect                  │ CreateCollection, React                    │
│ x/commons                  │ SubmitAnonymousProposal,                   │
│                            │ AnonymousVoteProposal                      │
│ x/rep                      │ CreateChallenge                            │
└────────────────────────────┴────────────────────────────────────────────┘

Each operation registers: domain, min_trust_level, nullifier_scope_type,
batch_mode_allowed, immediate_mode_allowed
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
├── Unstaked decay:              0.2% per epoch
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
├── Anonymous challenges:        No DREAM stake (module-paid gas via x/shield)
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

SHIELDED EXECUTION (x/shield)
├── Trust tree depth:            20 (supports ~1M members)
├── Proof generation:            2-5 seconds (client-side)
├── Proof verification:          2-5 ms (on-chain)
├── Max gas per exec:            configurable per operation
├── Max execs/identity/epoch:    configurable (rate limit)
├── Max funding per day:         governance-controlled daily cap
├── Shield epoch interval:       configurable (blocks per epoch)
├── TLE threshold:               2/3 of validators
├── TLE miss window:             100 blocks
├── TLE miss tolerance:          10 misses before jail
├── DKG window:                  configurable (blocks per phase)
├── Min TLE validators:          configurable minimum
├── Max pending queue size:      configurable
├── Max encrypted payload:       configurable
└── Max ops per batch:           configurable

BLOG CONTENT
├── Max title:                   200 chars
├── Max body:                    10,000 chars
├── Max reply:                   2,000 chars
├── Max reply depth:             5 levels
├── Storage cost:                100 uspark/byte
└── Rate limits:                 10 posts, 50 replies, 100 reactions/day

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
├── Retro reward budget:         25% of initiative minting (10K-75K)
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
