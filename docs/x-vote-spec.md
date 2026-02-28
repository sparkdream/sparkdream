# x/vote Module Specification

## Overview

The `x/vote` module implements **anonymous voting** using zero-knowledge proofs (ZK-SNARKs). Both proposal creation and vote casting are anonymous — observers can see that an eligible member acted, but cannot determine which member.

**Core guarantees:**

1. **Anonymous proposals**: Proposal creators prove eligibility without revealing identity
2. **Anonymous votes**: Voters prove eligibility and cast votes without revealing identity
3. **No double voting**: Nullifier system prevents duplicate votes per proposal
4. **One member, one vote**: Every eligible voter has equal weight — membership is the gate, not token balance
5. **Configurable visibility**: Proposals can be public, sealed, or fully private

All proofs use PLONK over the BN254 curve with KZG commitments and MiMC hashing (SNARK-friendly). PLONK's universal trusted setup means a single ceremony covers all circuits — no per-circuit ceremonies required.

## Privacy Model

### Visibility Levels

Each proposal has a visibility level that controls what non-voters can see:

```
PUBLIC (default):
├── Proposal text:       Visible to all
├── Individual votes:    Vote option visible per-nullifier
├── Final tally:         Visible to all
├── Voter identity:      Hidden (ZK)
└── Proposer identity:   Hidden (ZK) or public (proposer's choice)

SEALED:
├── Proposal text:       Visible to all
├── Individual votes:    Hidden until voting ends (committed)
├── Final tally:         Revealed after voting period ends
├── Voter identity:      Hidden (ZK)
└── Proposer identity:   Hidden (ZK) or public (proposer's choice)

PRIVATE (anonymous proposals only):
├── Proposal text:       Encrypted (eligible voters only)
├── Individual votes:    Hidden until reveal (committed, same as SEALED)
├── Final tally:         Public (chain must know to enforce outcome)
├── Proposal outcome:    Public (PASSED/REJECTED/QUORUM_NOT_MET/VETOED)
├── Voter identity:      Hidden (ZK)
├── Proposer identity:   Hidden (ZK)
└── Non-voters see:      Proposal ID, status, timestamps, tally, outcome
                         (but NOT what was voted on)
```

**Note**: The `SEALED` and `PUBLIC` visibility levels can be used with either anonymous proposals (`MsgCreateAnonymousProposal`) or public-proposer proposals (`MsgCreateProposal`). `PRIVATE` visibility is restricted to anonymous proposals — since the proposal text is encrypted and only eligible voters can read it, a public proposer identity would be contradictory (the proposer's interest in the topic leaks information about the content). Public proposers who want hidden votes should use `SEALED`.

### What's Always Public (On-Chain)

```
├── Merkle root of eligible voters
├── Nullifiers (prevents double voting, unlinkable to identity)
├── Proposal ID and status
├── Number of votes cast
├── ZK proofs
└── Timestamps
```

### What's Always Private (Never Revealed)

```
├── Voter's secret key
├── Voter's position in Merkle tree
├── Merkle proof path
└── Link between voter address and nullifier
```

### Anonymity and Equal Voting

All votes carry equal weight — one member, one vote. This is a deliberate design choice:

- **Egalitarian governance**: Membership is the gate, not token wealth. The invitation system, trust levels, and reputation in x/rep already filter for engaged members. Once admitted, every voice counts equally.
- **Maximum anonymity**: Since all votes are identical in weight, there is no metadata to narrow down who cast a particular vote. Weighted voting would leak identity (a voter with a unique DREAM balance becomes trivially identifiable).
- **Simpler circuit**: No voting power in the Merkle leaf means fewer public inputs, fewer constraints, and faster proofs.

Weighted influence exists elsewhere in the system (conviction staking in x/rep, DREAM-based initiative thresholds, trust-level-gated actions). x/vote is specifically for "does the community support this?" decisions where equal voice is appropriate.

## Nullifier System

The nullifier uniquely identifies an action without revealing the actor:

```
vote_nullifier     = hash("vote", secretKey, proposalID)
proposal_nullifier = hash("propose", secretKey, epoch, nonce)
```

The domain-separated prefixes ("vote", "propose") prevent correlation between a voter's proposal-creation nullifier and their voting nullifier.

The proposal nullifier includes a `nonce` (0-indexed) to allow up to `max_proposals_per_epoch` proposals per member per epoch. With `max_proposals_per_epoch = 3`, a member can use nonces 0, 1, 2 — each producing a distinct nullifier. The ZK circuit constrains `nonce <= max_proposals_per_epoch - 1`.

Properties:
- Same voter + same proposal = same nullifier (prevents double voting)
- Same voter + different proposal = different nullifier (unlinkable across proposals)
- Proposal nullifier ≠ vote nullifier for same voter (unlinkable across actions)
- Same proposer + same epoch + same nonce = same nullifier (prevents reuse)
- Different voter = different nullifier (can't impersonate)

## Merkle Tree

Voters are stored in a Merkle tree where:

```
leaf = hash(secretKey)
```

The leaf IS the voter's registered public commitment. There is no extra hashing layer — the registered `zk_public_key = hash(secretKey)` is used directly as the Merkle leaf, saving ~320 constraints in the ZK circuit.

Tree depth of 20 supports ~1 million voters.

The tree is snapshotted when a proposal is created, fixing the eligible voter set for the lifetime of that proposal. This prevents manipulation of eligibility during voting.

The full Merkle tree (all leaves, all intermediate nodes) is maintained in on-chain state so that the `VoterMerkleProof` query can return Merkle paths without requiring an external indexer.

### Tree Variants

The module maintains two tree variants:

- **Main tree**: All active registered voters. Used for proposal creation proofs and for voting on PUBLIC/SEALED proposals.
- **Encryption tree**: Only active voters who registered an `encryption_public_key`. Used for voting on PRIVATE proposals. This prevents voters without encryption keys from voting blind on proposals they cannot read.

Both trees share the same depth and structure; the encryption tree is a subset. Since auto-registration always provides an encryption key (derived from the account key), the main tree and encryption tree are identical for all auto-registered voters in practice. The distinction is maintained for forward-compatibility and for restricted jury trees (which may include jurors without encryption keys). When a PRIVATE proposal is created, the chain snapshots the encryption tree as the proposal's voting tree. The `eligible_voters` count and quorum calculation use this restricted set.

### Tree Rebuild Timing

The Merkle trees are rebuilt lazily:

1. **Registration/deactivation/revocation** marks the trees as "dirty" (a boolean flag in state)
2. **EndBlocker** checks the dirty flag each block. If dirty, it rebuilds both trees from all active registrations and clears the flag
3. **Proposal creation** uses the most recently built tree root. If the trees are dirty at proposal creation time, the message handler forces an immediate rebuild before snapshotting

This ensures proposals always reference a consistent tree. The rebuild cost is O(N) where N = active voters, amortized across blocks since rebuilds only happen when membership changes.

## State

### Proposal (Voting Context)

```protobuf
message VotingProposal {
  uint64 id = 1;

  // Content (plaintext for PUBLIC/SEALED, encrypted for PRIVATE)
  string title = 2;
  string description = 3;
  bytes encrypted_content = 4;    // AES-GCM ciphertext for PRIVATE mode
  bytes content_nonce = 5;        // Encryption nonce for PRIVATE mode

  // Creator
  string proposer = 6;            // Address if public, empty if anonymous
  bytes proposer_nullifier = 7;   // Set when proposer is anonymous

  // Voter eligibility snapshot
  bytes merkle_root = 8;
  int64 snapshot_block = 9;
  uint64 eligible_voters = 10;

  // Vote options
  repeated VoteOption options = 11;

  // Timing (all block heights — converted from epochs via blocks_per_epoch at creation)
  int64 voting_start = 12;              // Block height when voting opens
  int64 voting_end = 13;                // Block height when voting closes

  // Thresholds
  string quorum = 14 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  string threshold = 15 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  string veto_threshold = 28 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"]; // Veto check: veto_votes / non_abstain_votes > veto_threshold → VETOED (0 = no veto check)

  // Results
  repeated VoteTally tally = 16;
  ProposalStatus status = 17;
  ProposalOutcome outcome = 18;

  // Metadata
  ProposalType proposal_type = 19;
  uint64 reference_id = 20;       // e.g., challenge_id for jury votes
  int64 created_at = 21;
  int64 finalized_at = 22;

  // Visibility
  VisibilityLevel visibility = 23;

  // Encryption (PRIVATE mode)
  repeated EncryptedKeyShare key_shares = 24;

  // Deposit (public proposals only)
  repeated cosmos.base.v1beta1.Coin deposit = 25;

  // Executable messages (x/gov pattern)
  // Executed by x/vote module account on PASS. Empty for advisory votes.
  // Always stored plaintext, even for PRIVATE proposals (chain must execute them).
  repeated google.protobuf.Any messages = 26;

  // TLE target epoch for auto-reveal (SEALED/PRIVATE only)
  // Computed at proposal creation time: epoch containing voting_end + 1
  // Clients read this field to encrypt their reveal payload to the correct epoch.
  uint64 reveal_epoch = 27;

  // Reveal deadline as block height (SEALED/PRIVATE only; 0 for PUBLIC)
  // Computed at proposal creation: voting_end + sealed_reveal_period_epochs * blocks_per_epoch.
  // Snapshotted so that governance changes to sealed_reveal_period_epochs do NOT
  // retroactively affect in-flight proposals.
  int64 reveal_end = 29;
}

// How a vote option is counted in the tally.
// Roles let x/vote understand abstain/veto semantics generically — the creating
// module picks which roles to include, and x/vote counts them accordingly.
enum OptionRole {
  OPTION_ROLE_STANDARD = 0;            // Normal option — counted in threshold
  OPTION_ROLE_ABSTAIN = 1;             // Counts toward quorum, excluded from threshold denominator
  OPTION_ROLE_VETO = 2;               // Counts toward quorum + triggers veto check (see Finalization)
}

// Vote option IDs MUST be sequential starting from 0 (0, 1, 2, ...).
// The chain enforces this at proposal creation time — proposals with
// non-sequential or non-zero-based option IDs are rejected.
// This enables efficient range validation: vote_option < len(options).
//
// Role constraints (enforced at proposal creation):
// - At least ONE option must have OPTION_ROLE_STANDARD
// - At most ONE option may have OPTION_ROLE_ABSTAIN
// - At most ONE option may have OPTION_ROLE_VETO
// - Both ABSTAIN and VETO are optional — simple votes need only STANDARD options
message VoteOption {
  uint32 id = 1;                       // Must equal the option's index (0-based, sequential)
  string label = 2;                    // "yes", "no", "abstain", "no with veto", or custom
  OptionRole role = 3;                 // How this option is counted (default STANDARD)
}

message VoteTally {
  uint32 option_id = 1;
  uint64 vote_count = 2;
}

// Per-voter encrypted copy of the proposal symmetric key
message EncryptedKeyShare {
  bytes voter_encryption_pubkey = 1; // Identifies which voter this is for
  bytes encrypted_key = 2;           // ECIES-encrypted symmetric key
}

enum VisibilityLevel {
  VISIBILITY_PUBLIC = 0;
  VISIBILITY_SEALED = 1;
  VISIBILITY_PRIVATE = 2;
}

enum ProposalStatus {
  PROPOSAL_STATUS_ACTIVE = 0;      // Voting is open
  PROPOSAL_STATUS_TALLYING = 1;    // SEALED/PRIVATE: reveal period (voting ended, reveals in progress)
  PROPOSAL_STATUS_FINALIZED = 2;   // Terminal: outcome determined, tally frozen
  PROPOSAL_STATUS_CANCELLED = 3;   // Terminal: cancelled by proposer or governance
}

enum ProposalOutcome {
  PROPOSAL_OUTCOME_UNSPECIFIED = 0;
  PROPOSAL_OUTCOME_PASSED = 1;
  PROPOSAL_OUTCOME_REJECTED = 2;
  PROPOSAL_OUTCOME_QUORUM_NOT_MET = 3;
  PROPOSAL_OUTCOME_VETOED = 4;           // Veto votes exceeded veto_threshold
}

enum ProposalType {
  PROPOSAL_TYPE_GENERAL = 0;
  PROPOSAL_TYPE_PARAMETER_CHANGE = 1;
  PROPOSAL_TYPE_COUNCIL_ELECTION = 2;
  PROPOSAL_TYPE_CHALLENGE_JURY = 3;
  PROPOSAL_TYPE_SLASHING = 4;
  PROPOSAL_TYPE_BUDGET_APPROVAL = 5;
}
```

### Voter Registration

```protobuf
message VoterRegistration {
  string address = 1;
  bytes zk_public_key = 2;             // hash(secretKey), 32 bytes
  bytes encryption_public_key = 3;     // Babyjubjub point, for PRIVATE mode
  int64 registered_at = 4;
  bool active = 5;
}
```

### Vote Record

```protobuf
// PUBLIC mode: vote option visible
message AnonymousVote {
  uint64 proposal_id = 1;
  bytes nullifier = 2;                  // 32 bytes, unique per voter+proposal
  uint32 vote_option = 3;              // Plaintext for PUBLIC
  bytes proof = 4;                      // ~500 bytes PLONK proof
  int64 submitted_at = 5;
}

// SEALED mode: vote option committed, revealed later
message SealedVote {
  uint64 proposal_id = 1;
  bytes nullifier = 2;
  bytes vote_commitment = 3;            // hash(vote_option, salt)
  bytes proof = 4;
  int64 submitted_at = 5;
  bytes encrypted_reveal = 6;           // TLE-encrypted reveal payload (for auto-reveal)

  // Populated during reveal phase (auto or manual)
  uint32 revealed_option = 7;
  bytes reveal_salt = 8;
  bool revealed = 9;
}
```

### Voter Tree Snapshot

```protobuf
message VoterTreeSnapshot {
  uint64 proposal_id = 1;
  bytes merkle_root = 2;
  int64 snapshot_block = 3;
  uint64 voter_count = 4;
}
```

### Used Nullifiers

```protobuf
// Vote nullifiers: keyed by (proposal_id, nullifier)
message UsedNullifier {
  uint64 proposal_id = 1;
  bytes nullifier = 2;
  int64 used_at = 3;
}

// Proposal creation nullifiers: keyed by (epoch, nullifier)
// Separate from vote nullifiers because proposal nullifiers are scoped by epoch, not proposal_id.
message UsedProposalNullifier {
  uint64 epoch = 1;
  bytes nullifier = 2;
  int64 used_at = 3;
}
```

### TLE State

```protobuf
// Validator's public key share from DKG ceremony (stored on-chain for correctness proof verification)
message TLEValidatorShare {
  string validator = 1;               // Validator operator address
  bytes public_key_share = 2;         // BLS public key share (from DKG)
  uint64 share_index = 3;             // Position in the sharing polynomial
  int64 registered_at = 4;            // Block height when registered
}

// Tracks decryption shares submitted by validators for a given epoch
message TLEDecryptionShare {
  string validator = 1;
  uint64 epoch = 2;
  bytes share = 3;
  int64 submitted_at = 4;             // Block height
}

// Aggregated epoch decryption key (available once threshold shares collected)
message EpochDecryptionKey {
  uint64 epoch = 1;
  bytes decryption_key = 2;
  int64 available_at = 3;             // Block height when key became available
}
```

### SRS Storage

```protobuf
// The PLONK universal structured reference string (SRS).
// Stored in a dedicated state key (NOT in Params) because the SRS is several MB.
// The chain only needs the verifying keys (in Params) for proof verification.
// The SRS is needed only during circuit key derivation (governance param change)
// and is distributed out-of-band (downloaded by validators during setup).
// Integrity is verified against params.srs_hash before any key derivation.
message SRSState {
  bytes srs = 1;                       // Full PLONK SRS bytes
  bytes hash = 2;                      // SHA-256 hash (must match params.srs_hash)
  int64 stored_at = 3;                 // Block height when stored
}
```

## Params

```protobuf
message Params {
  // ZK circuit configuration (PLONK with universal SRS)
  // NOTE: The full SRS (several MB) is NOT stored in params — it is distributed out-of-band
  // and stored in a dedicated state key (see SRSState below). Only the hash is in params
  // for integrity verification during circuit key derivation.
  bytes srs_hash = 1;                  // SHA-256 hash of the universal SRS (32 bytes, for integrity check)
  bytes vote_verifying_key = 2;        // Verifying key for vote circuit (derived from SRS, ~1-2 KB)
  bytes proposal_verifying_key = 3;    // Verifying key for proposal creation circuit (derived from SRS, ~1-2 KB)
  uint32 tree_depth = 4;              // default 20

  // Voting periods
  int64 min_voting_period_epochs = 5;
  int64 max_voting_period_epochs = 6;
  int64 default_voting_period_epochs = 7;

  // SEALED mode: reveal period after voting ends
  int64 sealed_reveal_period_epochs = 8;

  // Thresholds
  string default_quorum = 9 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  string default_threshold = 10 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  string default_veto_threshold = 29 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"]; // Default: 33.4% (matching x/gov)

  // Registration
  bool open_registration = 11;
  string min_registration_stake = 12 [(gogoproto.customtype) = "cosmossdk.io/math.Int"];

  // Rate limiting (anonymous proposals)
  uint64 max_proposals_per_epoch = 13; // default 1

  // Privacy
  bool allow_private_proposals = 14;
  bool allow_sealed_proposals = 15;
  uint64 max_private_eligible_voters = 16; // Caps key_shares size

  // Deposit (public proposals only; anonymous proposals use rate limiting instead)
  repeated cosmos.base.v1beta1.Coin min_proposal_deposit = 17;

  // Vote options constraints
  uint32 min_vote_options = 18;       // default 2 (at least yes/no)
  uint32 max_vote_options = 19;       // default 10 (prevent state bloat)

  // Threshold timelock encryption (TLE)
  bool tle_enabled = 20;                        // Feature flag (default true)
  uint32 tle_threshold_numerator = 21;          // Default 2 (of 3 → 2/3 threshold)
  uint32 tle_threshold_denominator = 22;        // Default 3 (validators needed for epoch key)
  bytes tle_master_public_key = 23;             // BLS public key for TLE encryption (from DKG ceremony)
  uint32 max_encrypted_reveal_bytes = 24;       // Max TLE payload size per vote (default 512)

  // TLE validator liveness (see Validator Obligations section)
  uint32 tle_miss_window = 25;                   // Rolling window in epochs (default 100)
  uint32 tle_miss_tolerance = 26;                // Max missed epochs before flagged TLE-inactive (default 10)
  bool tle_jail_enabled = 27;                    // Jail TLE-inactive validators (default false)

  // Epoch fallback (used when x/season is not available)
  uint64 blocks_per_epoch = 28;                  // Blocks per epoch for fallback derivation (default 17280, ~1 day at 5s blocks)
}
```

## Messages

### Governance Messages

```protobuf
// Update module parameters (governance only)
message MsgUpdateParams {
  string authority = 1;               // Must be x/gov module account
  Params params = 2;
}

// Store or update the PLONK SRS on-chain (governance only)
// The SRS is several MB and is stored in a dedicated state key (SRSState), not in Params.
//
// Upload mechanism:
// - At genesis: SRS bytes are included in genesis.json (GenesisState.srs_state).
//   This is the primary path — the SRS is generated during the pre-launch ceremony
//   and baked into genesis. No transaction needed.
// - Post-genesis: This message allows governance to upload a new SRS via a proposal
//   containing MsgStoreSRS. Transaction size limits (~1-2 MB on most Cosmos chains)
//   may require chunked upload or an increase to max_tx_bytes. Alternatively, a chain
//   upgrade handler can load the SRS from a file bundled with the binary, bypassing
//   transaction size limits entirely. This is the recommended path for SRS updates.
// - The stored SRS hash must match params.srs_hash (set via MsgUpdateParams).
//   Workflow: (1) store SRS via MsgStoreSRS or upgrade handler, (2) update params
//   with the new srs_hash and derived verifying keys via MsgUpdateParams.
message MsgStoreSRS {
  string authority = 1;               // Must be x/gov module account
  bytes srs = 2;                      // Full PLONK SRS bytes
}
```

### Registration Messages

```protobuf
// Register for anonymous voting
// Client derives secretKey from account signature over a fixed domain string,
// then computes publicKey = hash(secretKey). Only publicKey is submitted on-chain.
// The ZK key is deterministically recoverable from the account key — no separate backup needed.
//
// Re-registration: If the voter has an existing inactive registration (self-deactivated or
// revoked), MsgRegisterVoter reactivates it — setting active=true and updating the ZK/encryption
// keys to the newly submitted values. The old registration record is overwritten, not duplicated.
// This allows members who were suspended and later reinstated by x/rep to rejoin voting
// without any special flow. The chain checks x/rep membership at registration time regardless.
message MsgRegisterVoter {
  string voter = 1;
  bytes zk_public_key = 2;             // hash(secretKey), 32 bytes
  bytes encryption_public_key = 3;     // Babyjubjub point, required (auto-derived from account key)
}

// Deactivate registration (voluntary — can re-register later via MsgRegisterVoter)
message MsgDeactivateVoter {
  string voter = 1;
}

// Rotate ZK secret key (atomic re-registration)
// Replaces the old leaf in the Merkle tree with the new one.
// In-flight proposals still use old tree snapshots (old key works).
// New proposals after the next tree rebuild use the new key.
message MsgRotateVoterKey {
  string voter = 1;                    // Must match existing registration
  bytes new_zk_public_key = 2;         // hash(newSecretKey)
  bytes new_encryption_public_key = 3; // If empty: keep existing key. If provided: update. Cannot clear (empty bytes rejected if voter already has one).
}
```

### Anonymous Proposal Creation

```protobuf
// Create a proposal anonymously with ZK proof of eligibility
message MsgCreateAnonymousProposal {
  string submitter = 1;               // Relayer/submitter address (pays gas)

  // Content (plaintext for PUBLIC/SEALED)
  string title = 2;
  string description = 3;

  // Content (encrypted for PRIVATE — title/description left empty)
  bytes encrypted_content = 4;
  bytes content_nonce = 5;
  repeated EncryptedKeyShare key_shares = 6;

  // Proposal config
  ProposalType proposal_type = 7;
  uint64 reference_id = 8;
  repeated VoteOption options = 9;
  int64 voting_period_epochs = 10;
  string quorum = 11 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  string threshold = 12 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  VisibilityLevel visibility = 13;

  // ZK proof of eligibility
  bytes nullifier = 14;               // hash("propose", secretKey, epoch, nonce)
  uint64 nonce = 15;                  // Proposal index within this epoch (0-indexed)
  uint64 claimed_epoch = 16;          // Epoch used in proof (chain verifies it's within ±1 of current)
  bytes proof = 17;                   // PLONK proof

  // Executable messages (x/gov pattern)
  // Always plaintext, even for PRIVATE proposals. Non-voters can see WHAT will
  // happen if the vote passes, but not WHY (the encrypted title/description).
  repeated google.protobuf.Any messages = 18;

  // Veto threshold (added after initial field layout — field 19)
  string veto_threshold = 19 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"]; // 0 or omitted = use default_veto_threshold; ignored if no VETO-role option
}

// Create a proposal with public proposer identity (non-anonymous)
// Proposer must be a registered voter (active x/rep member).
// Public proposals require a deposit (returned if quorum is met, burned otherwise).
// This prevents spam since the proposer's identity is known and accountable.
//
// Module-account exemption: When the proposer is a module account address (e.g.,
// x/rep, x/commons, x/reveal creating proposals programmatically via
// CreateProposal/CreateProposalWithTree), the deposit requirement is waived and
// the x/rep membership check is skipped. Module accounts are trusted system actors,
// not spam vectors. The chain verifies the proposer is a registered module account.
//
// Visibility restricted to PUBLIC or SEALED. PRIVATE is not allowed because
// encrypted content + known proposer identity leaks information about the content.
// Public proposers who want hidden votes should use SEALED.
message MsgCreateProposal {
  string proposer = 1;
  string title = 2;
  string description = 3;
  ProposalType proposal_type = 4;
  uint64 reference_id = 5;
  repeated VoteOption options = 6;
  int64 voting_period_epochs = 7;
  string quorum = 8 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  string threshold = 9 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"];
  VisibilityLevel visibility = 10;    // PUBLIC or SEALED only (PRIVATE rejected)
  repeated cosmos.base.v1beta1.Coin deposit = 11; // Spam prevention deposit
  repeated google.protobuf.Any messages = 12;     // Executed on PASS (x/gov pattern)

  // Veto threshold (added after initial field layout — field 13)
  string veto_threshold = 13 [(gogoproto.customtype) = "cosmossdk.io/math.LegacyDec"]; // 0 or omitted = use default_veto_threshold; ignored if no VETO-role option
}

// Cancel a proposal
// - Public proposals: proposer or governance authority
// - Anonymous proposals: governance authority only (no cancel circuit exists;
//   adding one would require a third trusted setup for marginal benefit)
message MsgCancelProposal {
  string authority = 1;               // Proposer address (public) or module authority (gov)
  uint64 proposal_id = 2;
  string reason = 3;
}
```

### Voting Messages

```protobuf
// Submit an anonymous vote with ZK proof (PUBLIC mode)
message MsgVote {
  string submitter = 1;               // Can be anyone (relayer pattern)
  uint64 proposal_id = 2;
  bytes nullifier = 3;                // hash("vote", secretKey, proposalID)
  uint32 vote_option = 4;
  bytes proof = 5;                    // PLONK proof
}

// Submit a sealed vote (SEALED mode — option hidden until reveal)
message MsgSealedVote {
  string submitter = 1;
  uint64 proposal_id = 2;
  bytes nullifier = 3;
  bytes vote_commitment = 4;          // hash(vote_option, salt)
  bytes proof = 5;
  bytes encrypted_reveal = 6;        // TLE-encrypted reveal payload (for auto-reveal)
}

// Reveal a sealed vote (after voting period ends)
message MsgRevealVote {
  string submitter = 1;
  uint64 proposal_id = 2;
  bytes nullifier = 3;                // Must match a submitted sealed vote
  uint32 vote_option = 4;
  bytes reveal_salt = 5;
}
```

### TLE Messages

```protobuf
// Submit a decryption key share for an epoch (validators only)
// Validators submit their share each epoch to enable auto-reveal.
// Once threshold shares are collected, the epoch decryption key is aggregated.
// Duplicate submissions from the same validator for the same epoch are rejected.
message MsgSubmitDecryptionShare {
  string validator = 1;               // Must be an active bonded validator with a registered TLEValidatorShare
  uint64 epoch = 2;                   // Target epoch
  bytes decryption_share = 3;         // Validator's IBE key share for this epoch
  bytes correctness_proof = 4;        // Verified against the validator's on-chain TLEValidatorShare.public_key_share
}

// Register a validator's public key share from DKG ceremony (one-time)
// Submitted after DKG completes. The public key share is stored on-chain and used
// to verify correctness proofs on MsgSubmitDecryptionShare.
// Can be re-submitted after PSS key refresh (replaces old share).
message MsgRegisterTLEShare {
  string validator = 1;               // Must be an active bonded validator
  bytes public_key_share = 2;         // BLS public key share from DKG
  uint64 share_index = 3;             // Position in the sharing polynomial
}
```

## ZK Circuits

The module uses **two separate circuits** sharing a single universal trusted setup (PLONK). Each circuit has its own verifying key derived from the same structured reference string (SRS).

### Circuit 1: Vote Circuit

Proves: "I am an eligible voter for this proposal."

**Public Inputs:**

| Input | Type | Description |
|-------|------|-------------|
| `MerkleRoot` | field | Root of the voter eligibility tree |
| `Nullifier` | field | hash("vote", secretKey, proposalID) |
| `ProposalID` | uint64 | Which proposal is being voted on |
| `VoteOption` | field | Vote choice (PUBLIC: small option index; SEALED: commitment hash = `hash(option, salt)`) |

**Private Inputs:**

| Input | Type | Description |
|-------|------|-------------|
| `SecretKey` | field | Voter's secret key |
| `PathElements` | [depth]field | Merkle proof siblings |
| `PathIndices` | [depth]uint64 | Merkle proof positions (0 or 1) |

**Constraints:**

```
1. Leaf computation (leaf = registered public commitment):
   leaf = hash(secretKey)

2. Merkle proof verification:
   computed_root = merkle_verify(leaf, pathElements, pathIndices)
   assert(computed_root == MerkleRoot)

3. Nullifier verification (domain-separated):
   expected_nullifier = hash("vote", secretKey, proposalID)
   assert(expected_nullifier == Nullifier)

4. Path indices binary:
   for each pathIndex:
     assert(pathIndex * (pathIndex - 1) == 0)
```

Note: `VoteOption` is a public input but is NOT constrained within the circuit — it's bound to the proof via the public witness. The circuit proves the voter is eligible; the vote choice is simply attached. For SEALED mode, the public input is the vote commitment hash, not the option itself. **Vote option range validation** is performed chain-side: the message handler rejects any `vote_option >= len(proposal.options)` before proof verification.

**OptionRole note**: The `OptionRole` (STANDARD, ABSTAIN, VETO) assigned to each vote option does NOT affect the ZK circuit. The circuit is agnostic to roles — it only binds the voter's choice to the proof. Role interpretation happens entirely chain-side during tally calculation. No circuit changes are needed to support abstain/veto semantics.

### Circuit 2: Proposal Creation Circuit

Proves: "I am an eligible member and I haven't exceeded my proposal creation limit this epoch."

**Public Inputs:**

| Input | Type | Description |
|-------|------|-------------|
| `MerkleRoot` | field | Root of the eligible member tree |
| `Nullifier` | field | hash("propose", secretKey, epoch, nonce) |
| `Epoch` | uint64 | Current epoch (for rate limiting) |
| `Nonce` | uint64 | Proposal index within this epoch (0-indexed) |
| `MaxNonce` | uint64 | `max_proposals_per_epoch - 1` (for range check) |

**Private Inputs:**

| Input | Type | Description |
|-------|------|-------------|
| `SecretKey` | field | Proposer's secret key |
| `PathElements` | [depth]field | Merkle proof siblings |
| `PathIndices` | [depth]uint64 | Merkle proof positions |

**Constraints:**

```
1. Leaf computation (leaf = registered public commitment):
   leaf = hash(secretKey)

2. Merkle proof verification:
   computed_root = merkle_verify(leaf, pathElements, pathIndices)
   assert(computed_root == MerkleRoot)

3. Nullifier verification (domain-separated, nonce-bound):
   expected_nullifier = hash("propose", secretKey, epoch, nonce)
   assert(expected_nullifier == Nullifier)

4. Nonce range check:
   assert(Nonce <= MaxNonce)

5. Path indices binary:
   for each pathIndex:
     assert(pathIndex * (pathIndex - 1) == 0)
```

The nonce allows up to `max_proposals_per_epoch` proposals per member per epoch. Each (epoch, nonce) pair produces a unique nullifier. The chain rejects duplicate nullifiers, preventing a member from reusing the same nonce. `MaxNonce` is provided as a public input so the circuit can enforce the range constraint; the chain verifies that `MaxNonce == params.max_proposals_per_epoch - 1`.

### Performance Characteristics

| Metric | Vote Circuit | Proposal Circuit |
|--------|-------------|-----------------|
| Constraints | ~6,880 | ~6,900 |
| Proof generation | ~3-7 seconds | ~3-7 seconds |
| Proof verification | ~3-5 ms | ~3-5 ms |
| Proof size | ~500 bytes | ~500 bytes |
| Tree depth | 20 (configurable) | 20 (configurable) |

The two circuits are nearly identical — both prove Merkle tree membership with a domain-separated nullifier. The vote circuit uses proposalID in the nullifier; the proposal circuit uses epoch+nonce and includes a nonce range check (~20 extra constraints).

## Flows

### 1. Voter Registration (One-Time)

```
Client (automatic — triggered by app on first login or invitation acceptance):
1. Sign fixed domain string: sig = sign("sparkdream-vote-identity-v1", accountPrivKey)
2. Derive secretKey = hash(sig) (deterministic — same account always produces same key)
3. Compute zkPublicKey = hash(secretKey)
4. Derive Babyjubjub encryption keypair from hash("encryption", secretKey)
5. Submit MsgRegisterVoter with zkPublicKey and encryptionPubKey
   - For new members: bundled with MsgAcceptInvitation in a single transaction
   - For founding members: auto-submitted on first app login (post-launch)
6. Secret key stored in app secure storage. No separate backup needed —
   recoverable by re-deriving from the same account key.

Chain:
1. Verify sender is a member (via x/rep)
1b. If params.open_registration = false → reject (return ErrRegistrationClosed).
    When false, only governance or module accounts can register voters (e.g., batch
    registration during a controlled onboarding phase). Default is true (self-registration).
1c. If params.min_registration_stake > 0 → verify the voter has at least
    min_registration_stake bonded SPARK (delegated to any validator). This prevents
    sybil attacks where a single entity creates many x/rep members to inflate the
    voter set. Default is 0 (no stake requirement — x/rep membership is sufficient).
2. Check for existing registration:
   a. If active registration exists with same zkPublicKey → reject (already registered)
   b. If active registration exists with different zkPublicKey → reject (use MsgRotateVoterKey)
   c. If inactive registration exists → reactivate: set active=true, update keys to new values
   d. If no registration exists → create new registration
3. Verify zkPublicKey is not in use by any OTHER address (prevents key collision)
4. Store VoterRegistration{zkPublicKey, encryptionPubKey, active: true}
5. Mark Merkle tree as dirty (needs rebuild)
6. Emit EventVoterRegistered
```

### 2. Anonymous Proposal Creation

```
Client (Proposer):
1. Query current Merkle root and own Merkle proof
2. Query current epoch
3. Choose nonce (0 for first proposal this epoch, 1 for second, etc.)
4. Compute nullifier = hash("propose", secretKey, epoch, nonce)
5. Generate ZK proof (proposal creation circuit, with Nonce and MaxNonce as public inputs)
6. If PRIVATE mode:
   a. Generate random symmetric key K
   b. Encrypt proposal text with K (AES-GCM)
   c. For each eligible voter with an encryption key: encrypt K with their encryption public key
7. Submit MsgCreateAnonymousProposal (via any submitter)

Chain:
1. Verify claimed epoch is within ±1 of current epoch (1-epoch grace window in both directions)
2. Verify MaxNonce == params.max_proposals_per_epoch - 1
3. Check proposal-creation nullifier hasn't been used this epoch
4. Verify ZK proof against proposal verifying key and main tree root
5. Validate vote options: IDs must be sequential 0-indexed (options[i].id == i for all i),
   count within [min_vote_options, max_vote_options], labels non-empty and unique.
   Validate option roles: at least one STANDARD, at most one ABSTAIN, at most one VETO.
   Reject if any role constraints are violated (see VoteOption role constraints).
5b. Set veto_threshold: if any option has OPTION_ROLE_VETO:
    - Use submitted veto_threshold if > 0
    - Otherwise use params.default_veto_threshold
    If no option has OPTION_ROLE_VETO: set veto_threshold to 0 (skip veto check at finalization)
5c. Set quorum: use submitted value if > 0, otherwise use params.default_quorum.
    Set threshold: use submitted value if > 0, otherwise use params.default_threshold.
    Reject if either value is > 1 (100%) or < 0.
6. Validate voting_period_epochs within [min_voting_period_epochs, max_voting_period_epochs].
   If 0 or omitted, use default_voting_period_epochs. Reject if outside the allowed range.
6b. Check visibility restrictions:
    - PRIVATE: reject if params.allow_private_proposals = false (return ErrPrivateNotAllowed)
    - SEALED: reject if params.allow_sealed_proposals = false (return ErrSealedNotAllowed)
    - PUBLIC: always allowed
7. Rebuild Merkle trees if dirty
8. Snapshot the appropriate tree for voting:
   - PUBLIC/SEALED: snapshot the main tree. Reject if main tree has 0 voters (no one can vote)
   - PRIVATE: snapshot the encryption tree (voters with encryption keys only)
   Reject PRIVATE proposals if encryption tree has 0 or > max_private_eligible_voters voters
9. Record nullifier
10. Create proposal with appropriate visibility level
11. Convert epochs to block heights and set timing:
    voting_start = current block height
    voting_end = voting_start + voting_period_epochs * blocks_per_epoch
    For SEALED/PRIVATE:
      reveal_epoch = epoch containing voting_end + 1 (clients encrypt to this TLE epoch)
      reveal_end = voting_end + sealed_reveal_period_epochs * blocks_per_epoch
      (snapshotted — immune to future param changes; 0 for PUBLIC proposals)
    blocks_per_epoch is from params (fallback) or derived from x/season epoch boundaries.
12. Emit EventProposalCreated (with nullifier, not proposer address)

Note: The proposal creation proof is always verified against the main tree root
(any member can propose). The voting tree snapshot may differ for PRIVATE proposals.
If the main tree root changed between the client generating the proof and the tx
executing (e.g., due to a new registration), the proof fails and the client retries.
This is expected behavior — proof generation is fast (~3-7 seconds) and tree rebuilds
are infrequent.
```

### 2b. Public Proposal Creation (MsgCreateProposal)

Public proposals follow the same validation as anonymous proposals (steps 5-12 above) with these differences:

```
Client (Proposer):
1. Submit MsgCreateProposal directly (no ZK proof, no relayer needed)

Chain:
1. Verify proposer is a registered voter (active x/rep member), OR is a module account
   (module accounts skip membership check and deposit requirement)
2. Reject PRIVATE visibility (public proposer + encrypted content is contradictory)
3. Validate deposit >= params.min_proposal_deposit (skip for module accounts)
4. Validate vote options — same as anonymous flow step 5:
   IDs sequential 0-indexed, count within [min, max], labels non-empty and unique.
   Role constraints: at least one STANDARD, at most one ABSTAIN, at most one VETO.
5. Set veto_threshold — same as anonymous flow step 5b:
   Use submitted value if > 0 and a VETO-role option exists, else use default.
   Set to 0 if no VETO-role option.
6. Set quorum and threshold — same as anonymous flow step 5c:
   Use submitted values if > 0, else use defaults. Reject if > 1 or < 0.
7. Validate voting_period_epochs — same as anonymous flow step 6
8. Check visibility restrictions — same as anonymous flow step 6b
   (SEALED rejected if allow_sealed_proposals = false)
9-13. Same as anonymous flow steps 7-12 (tree rebuild, snapshot, create proposal,
      set timing, emit event) — except proposer address is stored instead of nullifier
```

### 3a. Voting (PUBLIC Mode)

```
Client (Voter):
1. Query VoterMerkleProof for the proposal's snapshot
2. Load secretKey from secure storage
3. Compute nullifier = hash("vote", secretKey, proposalID)
4. Generate ZK proof (vote circuit)
5. Submit MsgVote (via any submitter/relayer)

Chain:
1. Check proposal is ACTIVE
2. Check proposal visibility is PUBLIC (reject MsgVote for SEALED/PRIVATE proposals)
3. Validate vote_option < len(proposal.options)
4. Check vote nullifier not already used for this proposal
5. Verify Merkle root matches proposal snapshot
6. Verify ZK proof against vote verifying key
7. Record nullifier
8. Update vote tally (increment option count)
9. Emit EventVoteCast (with nullifier and vote option)
```

### 3b. Voting (SEALED Mode)

```
Client (Voter):
1-3. Same as PUBLIC mode
4. Derive salt deterministically: salt = hash("sealed-salt", secretKey, proposalID)
5. Compute vote_commitment = hash(vote_option, salt)
6. Create reveal payload: {nullifier, vote_option, salt}
7. Encrypt reveal payload with threshold timelock encryption for the reveal epoch:
   encrypted_reveal = TLE.Encrypt(reveal_payload, proposal.reveal_epoch)
8. Generate ZK proof with vote_commitment as the VoteOption public input
9. Submit MsgSealedVote with vote_commitment, proof, AND encrypted_reveal
10. Done — voter does not need to return for the reveal phase.

Chain:
1. Check proposal is ACTIVE
2. Check proposal visibility is SEALED or PRIVATE (reject MsgSealedVote for PUBLIC proposals)
3. Validate vote_commitment is non-empty (32 bytes). No option range check — the option is
   hidden in the commitment and validated at reveal time.
4-6. Same as PUBLIC steps 4-6 (nullifier dedup, Merkle root match, ZK proof verification)
7. Validate encrypted_reveal size <= params.max_encrypted_reveal_bytes (reject if too large)
8. If TLE disabled: encrypted_reveal is ignored (manual reveal required)
9. Store sealed vote with encrypted_reveal (commitment visible, option hidden)
10. Emit EventSealedVoteCast (with nullifier, no vote option)

--- After voting period ends (TALLYING status) ---

Chain (EndBlocker auto-reveal):
1. Validators release the epoch decryption key for the reveal epoch
2. EndBlocker iterates all sealed votes for proposals entering TALLYING status
3. For each sealed vote:
   a. Decrypt encrypted_reveal using the epoch decryption key
   b. Verify hash(vote_option, salt) == stored commitment
   c. Validate vote_option < len(proposal.options) (reject out-of-range options —
      a voter who committed to an invalid option is treated as a failed reveal)
   d. If all checks pass: mark as revealed, record option, update tally
   e. If decryption, commitment, or range check fails: treat as unrevealed
      (the sealed vote still counts toward quorum — submitting proves participation —
      but the vote is excluded from threshold/veto calculations since the option is unknown)
4. Emit EventSealedVoteRevealed for each successfully revealed vote

Manual fallback (MsgRevealVote):
Chain validates:
- Proposal is in TALLYING status (reject if ACTIVE or FINALIZED)
- Sealed vote exists for the given nullifier (reject if not found)
- Vote not already revealed (reject if already revealed)
- hash(vote_option, salt) == stored commitment (reject on mismatch)
- vote_option < len(proposal.options) (reject out-of-range — return ErrVoteOptionOutOfRange)
If auto-reveal fails for a specific vote (e.g., corrupted encrypted_reveal),
the voter can still manually reveal during the reveal period via a relayer.
IMPORTANT: The voter MUST use a relayer/different submitter address for the
reveal to maintain anonymity.
PRIVACY NOTE: The relayer submitting a manual MsgRevealVote sees the plaintext
vote_option and reveal_salt for that nullifier. The relayer learns the vote
choice but NOT the voter identity. This is a privacy downgrade compared to
auto-reveal (where the chain decrypts internally and no individual sees
individual vote mappings). Users should prefer auto-reveal (TLE) and only
use manual reveal as a last resort.

--- After reveal period ends ---

Chain (EndBlocker):
1. Finalize proposal
2. Any remaining unrevealed votes stay unrevealed (count toward quorum but
   excluded from threshold/veto — see Finalization step 5c)
```

### 3c. Voting (PRIVATE Mode)

PRIVATE mode hides the **proposal text** from non-voters. Voting mechanics are identical to SEALED mode (commit-reveal). The final tally and outcome are public on-chain because the chain must know them to enforce results. The value of PRIVATE over SEALED is that non-voters cannot read *what was voted on*.

```
Client (Voter):
1. Decrypt the per-voter key share using own encryption private key
2. Decrypt proposal content using the symmetric key K
3. Read and evaluate the proposal
4-10. Same as SEALED mode (commitment, TLE-encrypted reveal, proof, submit — fire-and-forget)

--- Auto-reveal + tally happens exactly as SEALED ---

Non-voters see:
- Proposal ID, timestamps, status, vote count
- Final tally and outcome (public — chain enforces results)
- CANNOT decrypt proposal text (only eligible voters can)
```

**Requirement**: Voters must have registered an `encryption_public_key` (Babyjubjub) during registration to participate in PRIVATE proposals. PRIVATE proposals use the encryption tree (not the main tree) as their voting snapshot, so only voters with encryption keys are in the Merkle tree. Voters without an encryption key cannot generate a valid ZK proof and are completely excluded — they cannot vote, even blind.

### 4. Finalization

```
Chain (EndBlocker):
1. Rebuild Merkle trees if dirty (both main and encryption trees)
2. Aggregate any pending TLE decryption shares for the current epoch
   (reject duplicate shares from same validator for same epoch; verify correctness_proof
   against the validator's on-chain TLEValidatorShare.public_key_share;
   if threshold met, store the epoch decryption key and emit EventEpochDecryptionKeyAvailable)
   Also track TLE participation: update liveness records for validators who did/did not
   submit shares. Flag validators exceeding tle_miss_tolerance as TLE-inactive.
3. For each ACTIVE proposal where voting period has ended:
   a. Transition to TALLYING status (SEALED/PRIVATE) or proceed to finalization (PUBLIC)
4. For each SEALED/PRIVATE proposal in TALLYING status:
   a. Check if epoch decryption key is available for the proposal's reveal_epoch
   b. If available and votes not yet auto-revealed:
      - Decrypt all encrypted_reveal payloads using the epoch decryption key
      - Verify hash(vote_option, salt) == stored commitment for each
      - Valid: mark as revealed, record option, update tally, clear encrypted_reveal from state
      - Invalid (corrupted payload): leave as unrevealed (counts toward quorum, excluded from tally)
      - Emit EventSealedVoteRevealed for each revealed vote
   c. If not available: skip auto-reveal this block (retry next block)
      Manual reveals via MsgRevealVote are accepted in parallel throughout TALLYING
5. For proposals ready for finalization (PUBLIC: voting_end passed;
   SEALED/PRIVATE: proposal.reveal_end passed):
   a. PUBLIC: tally already computed incrementally
   b. SEALED/PRIVATE: unrevealed votes remain unrevealed (they count toward quorum
      but are excluded from threshold/veto calculations — see tally step c below).
      If TLE key was never available, emit EventTLEKeyUnavailable
   c. Tally calculation (role-aware):
      - total_submitted = all submitted votes (PUBLIC) or all sealed votes (SEALED/PRIVATE)
      - quorum_check: total_submitted / eligible_voters (all roles count toward quorum;
        all submitted sealed votes count regardless of reveal status — submitting a sealed
        vote proves participation even if the reveal fails)
      - If quorum not met → outcome = QUORUM_NOT_MET, skip remaining checks
      - revealed_votes:
        * PUBLIC: revealed_votes = total_submitted (all PUBLIC votes are plaintext)
        * SEALED/PRIVATE: revealed_votes = count of sealed votes successfully revealed
          (auto-reveal or manual)
      - For revealed votes, partition by role:
        * abstain_votes = votes for ABSTAIN-role option
        * veto_votes = votes for VETO-role option
        * standard_votes = votes for all STANDARD-role options (the actual choices)
        * non_abstain = revealed_votes - abstain_votes (denominator for threshold + veto)
      - Edge case: if non_abstain == 0 (all revealed votes are abstain) → QUORUM_NOT_MET
        (a quorum of abstentions is not a decisive quorum)
      - Edge case: if revealed_votes == 0 (all reveals failed) → QUORUM_NOT_MET
        regardless of submitted count (a quorum of unreadable votes is not a quorum)
      - Veto check (if proposal.veto_threshold > 0):
        veto_votes / non_abstain > veto_threshold → outcome = VETOED
        Veto is checked BEFORE threshold — veto overrides pass.
      - Threshold check: winning_standard_votes / non_abstain > threshold → PASSED
        (winning option is the STANDARD-role option with the most votes;
        ABSTAIN and VETO votes are excluded from both numerator and denominator)
        Tie-breaking: if two or more STANDARD options share the highest vote count,
        the option with the lowest ID wins. This is deterministic and matches the
        option ordering chosen by the proposer (who places their preferred option first).
      - Otherwise → REJECTED
   d. Set outcome (PASSED / REJECTED / QUORUM_NOT_MET / VETOED)
   e. For public proposals with deposit (skip if proposer is a module account — no deposit):
      - PASSED or REJECTED with quorum met → return deposit to proposer
      - QUORUM_NOT_MET → burn deposit (spam penalty)
      - VETOED → burn deposit (same as x/gov — vetoed proposals are penalized)
   f. If PASSED and proposal has messages:
      Execute messages via message router (x/vote module account as authority)
      If any message fails, mark PASSED but emit EventProposalExecutionFailed
   g. Emit EventProposalFinalized
```

### 5. Cancellation

```
Trigger: MsgCancelProposal{authority, proposal_id, reason}

Chain:
1. Load proposal — reject if not found
2. Check cancellation authority:
   - Public proposals: authority must be the proposer address OR the governance module account
   - Anonymous proposals: authority must be the governance module account
     (anonymous proposers cannot cancel — no cancel circuit exists)
3. Check proposal is in ACTIVE or TALLYING status (reject if already FINALIZED or CANCELLED)
4. Set status to PROPOSAL_STATUS_CANCELLED
5. For public proposals with deposit: return deposit to proposer
   (cancellation is not spam — it's the proposer voluntarily withdrawing)
6. Nullifiers are NOT freed — they remain recorded. This prevents a proposer from
   cancelling and re-proposing to reset nullifiers and allow double-voting on a
   replacement proposal. Voters who voted on the cancelled proposal use a different
   nullifier for any new proposal (nullifier is hash of secretKey + proposalID).
7. Votes already cast are preserved in state for auditability but excluded from tally
8. Emit EventProposalCancelled
```

### 6. Voter Deactivation

```
--- Self-deactivation ---

Trigger: MsgDeactivateVoter{voter}

Chain:
1. Verify sender matches voter address
2. Load registration — reject if not found
3. Reject if already inactive (no-op prevention)
4. Set registration.active = false
5. Mark Merkle tree as dirty (both main and encryption trees need rebuild)
6. Emit EventVoterDeactivated{voter, zk_public_key, reason: "self_deactivated"}

Note: In-flight proposals use old tree snapshots, so the voter can still complete
votes on proposals created before deactivation. They are excluded from new proposals
after the next tree rebuild.

--- Revocation by x/rep (hook) ---

Trigger: x/rep calls OnMemberRevoked(ctx, member, reason) when a member is
removed, suspended, or zeroed.

Chain:
1. Load registration — if not found, return nil (member was never a voter)
2. Set registration.active = false
3. Mark Merkle tree as dirty
4. Emit EventVoterDeactivated{voter, zk_public_key, reason}
   Reason values: "member_revoked", "member_suspended", "member_zeroed"

Note: The voter can re-register later via MsgRegisterVoter if their x/rep membership
is reinstated (see Registration flow, re-registration path).
```

## Queries

```protobuf
service Query {
  // --- Scaffolded CRUD queries (Ignite-generated, Get/List per collection) ---
  rpc Params(QueryParamsRequest) returns (QueryParamsResponse);
  rpc GetVotingProposal(QueryGetVotingProposalRequest) returns (QueryGetVotingProposalResponse);
  rpc ListVotingProposal(QueryAllVotingProposalRequest) returns (QueryAllVotingProposalResponse);
  rpc GetVoterRegistration(QueryGetVoterRegistrationRequest) returns (QueryGetVoterRegistrationResponse);
  rpc ListVoterRegistration(QueryAllVoterRegistrationRequest) returns (QueryAllVoterRegistrationResponse);
  rpc GetAnonymousVote(QueryGetAnonymousVoteRequest) returns (QueryGetAnonymousVoteResponse);
  rpc ListAnonymousVote(QueryAllAnonymousVoteRequest) returns (QueryAllAnonymousVoteResponse);
  rpc GetSealedVote(QueryGetSealedVoteRequest) returns (QueryGetSealedVoteResponse);
  rpc ListSealedVote(QueryAllSealedVoteRequest) returns (QueryAllSealedVoteResponse);
  rpc GetVoterTreeSnapshot(QueryGetVoterTreeSnapshotRequest) returns (QueryGetVoterTreeSnapshotResponse);
  rpc ListVoterTreeSnapshot(QueryAllVoterTreeSnapshotRequest) returns (QueryAllVoterTreeSnapshotResponse);
  rpc GetUsedNullifier(QueryGetUsedNullifierRequest) returns (QueryGetUsedNullifierResponse);
  rpc ListUsedNullifier(QueryAllUsedNullifierRequest) returns (QueryAllUsedNullifierResponse);
  rpc GetUsedProposalNullifier(QueryGetUsedProposalNullifierRequest) returns (QueryGetUsedProposalNullifierResponse);
  rpc ListUsedProposalNullifier(QueryAllUsedProposalNullifierRequest) returns (QueryAllUsedProposalNullifierResponse);
  rpc GetTleValidatorShare(QueryGetTleValidatorShareRequest) returns (QueryGetTleValidatorShareResponse);
  rpc ListTleValidatorShare(QueryAllTleValidatorShareRequest) returns (QueryAllTleValidatorShareResponse);
  rpc GetTleDecryptionShare(QueryGetTleDecryptionShareRequest) returns (QueryGetTleDecryptionShareResponse);
  rpc ListTleDecryptionShare(QueryAllTleDecryptionShareRequest) returns (QueryAllTleDecryptionShareResponse);
  rpc GetEpochDecryptionKey(QueryGetEpochDecryptionKeyRequest) returns (QueryGetEpochDecryptionKeyResponse);
  rpc ListEpochDecryptionKey(QueryAllEpochDecryptionKeyRequest) returns (QueryAllEpochDecryptionKeyResponse);
  rpc GetSrsState(QueryGetSrsStateRequest) returns (QueryGetSrsStateResponse);

  // --- Custom domain queries (hand-written, application-level) ---

  // Proposals
  rpc Proposal(QueryProposalRequest) returns (QueryProposalResponse);
  rpc Proposals(QueryProposalsRequest) returns (QueryProposalsResponse);
  rpc ProposalsByStatus(QueryProposalsByStatusRequest) returns (QueryProposalsByStatusResponse);
  rpc ProposalsByType(QueryProposalsByTypeRequest) returns (QueryProposalsByTypeResponse);

  // Vote tallies
  // PUBLIC: returns current tally
  // SEALED/PRIVATE: returns tally only after reveal period
  rpc ProposalTally(QueryProposalTallyRequest) returns (QueryProposalTallyResponse);

  // Vote records
  // PUBLIC: returns votes with options
  // SEALED/PRIVATE: returns commitments (pre-reveal) or options (post-reveal)
  rpc ProposalVotes(QueryProposalVotesRequest) returns (QueryProposalVotesResponse);

  // Voter registration (public keys only)
  rpc VoterRegistrationQuery(QueryVoterRegistrationQueryRequest) returns (QueryVoterRegistrationQueryResponse);
  rpc VoterRegistrations(QueryVoterRegistrationsRequest) returns (QueryVoterRegistrationsResponse);

  // Merkle tree for proof generation
  rpc VoterTreeSnapshotQuery(QueryVoterTreeSnapshotQueryRequest) returns (QueryVoterTreeSnapshotQueryResponse);
  rpc VoterMerkleProof(QueryVoterMerkleProofRequest) returns (QueryVoterMerkleProofResponse);

  // Check if vote nullifier is used (for double-vote prevention)
  rpc NullifierUsed(QueryNullifierUsedRequest) returns (QueryNullifierUsedResponse);

  // Check if proposal creation nullifier is used (for client nonce recovery)
  // Clients on a new device can check which nonces they've already used this epoch
  // by computing hash("propose", secretKey, epoch, nonce) for each nonce 0..max
  rpc ProposalNullifierUsed(QueryProposalNullifierUsedRequest) returns (QueryProposalNullifierUsedResponse);

  // TLE epoch key status
  rpc TleStatus(QueryTleStatusRequest) returns (QueryTleStatusResponse);
  rpc EpochDecryptionKeyQuery(QueryEpochDecryptionKeyQueryRequest) returns (QueryEpochDecryptionKeyQueryResponse);
  rpc TleValidatorShares(QueryTleValidatorSharesRequest) returns (QueryTleValidatorSharesResponse);
}

message QueryTleStatusResponse {
  bool tle_enabled = 1;
  uint64 current_epoch = 2;
  uint64 latest_available_epoch = 3;  // Most recent epoch with aggregated decryption key
  bytes master_public_key = 4;
}

message QueryEpochDecryptionKeyQueryResponse {
  uint64 epoch = 1;
  bool available = 2;
  bytes decryption_key = 3;           // Empty if not yet available
  uint64 shares_received = 4;
  uint64 shares_needed = 5;
}

message QueryTleValidatorSharesResponse {
  repeated TleValidatorShare shares = 1;
  uint64 total_validators = 2;        // Total bonded validators
  uint64 registered_validators = 3;   // Validators with registered TLE shares
  uint64 threshold_needed = 4;        // Shares needed per epoch for aggregation
}

// Response for Merkle proof query (needed by voters to generate ZK proofs)
message QueryVoterMerkleProofResponse {
  bytes merkle_root = 1;
  bytes leaf = 2;
  int32 leaf_index = 3;
  repeated bytes path_elements = 4;
  repeated uint64 path_indices = 5;
}

message QueryProposalNullifierUsedRequest {
  uint64 epoch = 1;
  bytes nullifier = 2;
}

message QueryProposalNullifierUsedResponse {
  bool used = 1;
  int64 used_at = 2;                  // Block height when used (0 if not used)
}
```

## Keeper Interface

```go
type Keeper interface {
    // Registration
    RegisterVoter(ctx context.Context, voter sdk.AccAddress, zkPubKey []byte, encPubKey []byte) error
    DeactivateVoter(ctx context.Context, voter sdk.AccAddress) error
    RotateVoterKey(ctx context.Context, voter sdk.AccAddress, newZKPubKey []byte, newEncPubKey []byte) error
    GetVoterRegistration(ctx context.Context, voter sdk.AccAddress) (VoterRegistration, error)
    SetVoterRegistration(ctx context.Context, voter sdk.AccAddress, reg VoterRegistration) error

    // Membership revocation hook (called by x/rep)
    OnMemberRevoked(ctx context.Context, member sdk.AccAddress, reason string) error

    // Merkle tree management
    BuildVoterTree(ctx context.Context) (*MerkleTree, error)                               // Main tree: all active voters
    BuildEncryptionTree(ctx context.Context) (*MerkleTree, error)                           // Encryption tree: voters with encryption keys
    BuildRestrictedTree(ctx context.Context, voters []sdk.AccAddress) (*MerkleTree, error)  // Ad-hoc tree for jury/committee votes; skips addresses without an active voter registration (logs warning); returns ErrNoEligibleVoters if all are unregistered
    MarkTreeDirty(ctx context.Context)
    RebuildTreeIfDirty(ctx context.Context) error // Called in EndBlocker, rebuilds both trees
    CreateTreeSnapshot(ctx context.Context, proposalID uint64, visibility VisibilityLevel) error
    GetTreeSnapshot(ctx context.Context, proposalID uint64) (VoterTreeSnapshot, error)
    GetVoterMerkleProof(ctx context.Context, proposalID uint64, zkPubKey []byte) (*MerkleProof, error)

    // Proposal management
    CreateProposal(ctx context.Context, msg MsgCreateProposal) (uint64, error)
    CreateProposalWithTree(ctx context.Context, tree *MerkleTree, msg MsgCreateProposal) (uint64, error) // For jury/committee restricted voter sets
    CreateAnonymousProposal(ctx context.Context, msg MsgCreateAnonymousProposal) (uint64, error)
    CreateChallengeVote(ctx context.Context, challengeID uint64, jurors []sdk.AccAddress) (uint64, error) // Convenience: builds restricted tree + creates SEALED jury proposal
    GetProposal(ctx context.Context, proposalID uint64) (VotingProposal, error)
    CancelProposal(ctx context.Context, proposalID uint64, authority string, reason string) error
    FinalizeProposal(ctx context.Context, proposalID uint64) error

    // Voting
    SubmitVote(ctx context.Context, msg MsgVote) error
    SubmitSealedVote(ctx context.Context, msg MsgSealedVote) error
    RevealVote(ctx context.Context, msg MsgRevealVote) error
    VerifyVoteProof(ctx context.Context, proposalID uint64, nullifier []byte, proof []byte, publicInputs [][]byte) error
    VerifyProposalProof(ctx context.Context, nullifier []byte, proof []byte, publicInputs [][]byte) error
    IsNullifierUsed(ctx context.Context, proposalID uint64, nullifier []byte) bool
    IsProposalNullifierUsed(ctx context.Context, epoch uint64, nullifier []byte) bool
    UpdateTally(ctx context.Context, proposalID uint64, option uint32) error

    // Threshold timelock encryption (TLE)
    IsTLEEnabled(ctx context.Context) bool
    RegisterTLEShare(ctx context.Context, validator sdk.ValAddress, pubKeyShare []byte, shareIndex uint64) error
    GetTLEValidatorShare(ctx context.Context, validator sdk.ValAddress) (TLEValidatorShare, error)
    GetEpochDecryptionKey(ctx context.Context, epoch uint64) ([]byte, error)
    SubmitDecryptionShare(ctx context.Context, validator sdk.ValAddress, epoch uint64, share []byte, proof []byte) error
    HasSubmittedDecryptionShare(ctx context.Context, validator sdk.ValAddress, epoch uint64) bool
    AggregateDecryptionShares(ctx context.Context, epoch uint64) error // Called in EndBlocker when threshold met
    AutoRevealSealedVotes(ctx context.Context, proposalID uint64) error
    TrackTLEParticipation(ctx context.Context, epoch uint64) error     // Called in EndBlocker to update liveness tracking

    // SRS management (stored separately from Params due to size)
    StoreSRS(ctx context.Context, srs []byte) error            // Store SRS bytes; computes and verifies hash against params.srs_hash
    GetSRS(ctx context.Context) ([]byte, error)                // Load SRS (needed only during key derivation)

    // Params
    UpdateParams(ctx context.Context, authority string, params Params) error

    // Integration with x/rep (membership check only)
    IsMember(ctx context.Context, member sdk.AccAddress) bool
}
```

## Integration with x/rep

### Membership Check

Registration requires active x/rep membership. The only check x/vote needs from x/rep is whether an address is a current member:

```go
func (k Keeper) RegisterVoter(ctx context.Context, voter sdk.AccAddress, zkPubKey []byte, encPubKey []byte) error {
    // Must be an active member in x/rep
    if !k.repKeeper.IsMember(ctx, voter) {
        return ErrNotAMember
    }
    // ... store registration
}
```

No voting power lookup, no balance hooks, no tier computation. Membership is binary — you're in or you're not.

### Membership Revocation Hook

When x/rep removes or suspends a member, it calls this hook to deactivate their voter registration:

```go
// Called by x/rep when a member is removed, suspended, or zeroed
func (k Keeper) OnMemberRevoked(ctx context.Context, member sdk.AccAddress, reason string) error {
    reg, err := k.GetVoterRegistration(ctx, member)
    if err != nil {
        return nil // Not registered as voter — no-op
    }
    reg.Active = false
    k.SetVoterRegistration(ctx, member, reg)
    k.MarkTreeDirty(ctx) // Rebuild Merkle tree in next EndBlocker
    ctx.EventManager().EmitEvent(EventVoterDeactivated{Voter: member.String(), ZKPublicKey: reg.ZKPublicKey, Reason: reason})
    return nil
}
```

**Important**: Deactivating a registration marks the Merkle tree as dirty but does NOT invalidate in-flight proposals. Proposals snapshot the tree at creation time, so a member revoked mid-vote can still complete votes on proposals created before their revocation. They cannot vote on NEW proposals created after the next tree rebuild.

### Challenge Jury Integration

```go
// Create anonymous jury vote for a challenge (restricted voter set)
func (k Keeper) CreateChallengeVote(ctx context.Context, challengeID uint64, jurors []sdk.AccAddress) (uint64, error) {
    // Build Merkle tree from selected jurors only
    tree, err := k.BuildRestrictedTree(ctx, jurors)
    if err != nil {
        return 0, err
    }

    // CreateProposalWithTree uses the provided tree as the voting snapshot
    // instead of snapshotting from the main/encryption tree.
    return k.CreateProposalWithTree(ctx, tree, MsgCreateProposal{
        Title:         fmt.Sprintf("Challenge %d Jury Vote", challengeID),
        ProposalType:  PROPOSAL_TYPE_CHALLENGE_JURY,
        ReferenceID:   challengeID,
        Options:       []VoteOption{
            {Id: 0, Label: "uphold", Role: OPTION_ROLE_STANDARD},
            {Id: 1, Label: "reject", Role: OPTION_ROLE_STANDARD},
        },
        VotingPeriodEpochs:  7, // epochs
        Quorum:        sdk.NewDecWithPrec(67, 2), // 67%
        Threshold:     sdk.NewDecWithPrec(67, 2), // 67% to uphold
        Visibility:    VISIBILITY_SEALED,          // Hide votes until end
    })
}
```

## Client-Side Implementation

### Key Management (Account-Derived)

```go
// Derive voting identity from Cosmos account key (account abstraction)
// The ZK secret key is derived from a deterministic signature — the user
// only manages their Cosmos account key. No separate voting key backup needed.
func DeriveVoterIdentity(accountPrivKey crypto.PrivKey) (*VoterIdentity, error) {
    // Sign a fixed domain string to derive the ZK secret key
    // Deterministic (RFC 6979) — same account always produces the same identity
    sig, err := accountPrivKey.Sign([]byte("sparkdream-vote-identity-v1"))
    if err != nil {
        return nil, err
    }
    secretKey := HashMiMC(sig)
    zkPublicKey := HashMiMC(secretKey)

    // Derive encryption keypair (Babyjubjub, for PRIVATE proposals)
    encSeed := HashMiMC([]byte("encryption"), secretKey)
    encPrivKey, encPubKey, err := DeriveBabyjubjubKeypair(encSeed)
    if err != nil {
        return nil, err
    }

    return &VoterIdentity{
        SecretKey:          secretKey,
        ZKPublicKey:        zkPublicKey,
        EncryptionPrivKey:  encPrivKey,
        EncryptionPubKey:   encPubKey,
    }, nil
}
```

### Vote Proof Generation

```go
func GenerateVoteProof(input *VoteInput) (*VoteProof, error) {
    prover, err := LoadProver("vote_proving_key.bin")
    if err != nil {
        return nil, err
    }

    // Compute nullifier (domain-separated)
    nullifier := HashMiMC([]byte("vote"), input.SecretKey, input.ProposalID)

    // Generate PLONK proof (~3-7 seconds)
    proof, err := prover.Prove(&VoteCircuitAssignment{
        // Public
        MerkleRoot:  input.MerkleRoot,
        Nullifier:   nullifier,
        ProposalID:  input.ProposalID,
        VoteOption:  input.VoteOption,  // or commitment for SEALED
        // Private
        SecretKey:    input.SecretKey,
        PathElements: input.MerkleProof.PathElements,
        PathIndices:  input.MerkleProof.PathIndices,
    })
    if err != nil {
        return nil, err
    }

    return &VoteProof{
        Nullifier:   nullifier,
        VoteOption:  input.VoteOption,
        ProofBytes:  proof.Serialize(),
    }, nil
}
```

### SEALED Vote Commitment

The `seal-vote` CLI tool (`zkprivatevoting/cmd/seal-vote`) and the `tle` library (`zkprivatevoting/tle`) automate this process:

```go
import "sparkdream/zkprivatevoting/tle"

// SealVote computes commitment + ECIES-encrypted reveal in one call.
// masterPubKey is read from on-chain params (TleMasterPublicKey).
// Pass nil for salt to auto-generate a random 32-byte salt.
sealed, err := tle.SealVote(masterPubKey, voteOption, nil)
// sealed.Commitment      → MiMC(voteOption_4bytes, salt), submit as vote_commitment
// sealed.EncryptedReveal → ECIES ciphertext, submit as encrypted_reveal
// sealed.Salt            → SAVE THIS for manual reveal fallback

// Compute nullifier for double-vote prevention.
nullifier := tle.ComputeNullifier(secretKey, proposalID)
```

**Commitment scheme** (must match on-chain `computeCommitmentHash`):

```
commitment = MiMC(voteOption as 4 bytes big-endian, salt as 32 bytes)
```

**Encrypted reveal format** (must match on-chain `decryptTLEPayload`):

```
plaintext        = voteOption (4 bytes, big-endian uint32) || salt (32 bytes)
encrypted_reveal = ECIES.Encrypt(bn256.G1, masterPublicKey, plaintext, nil)
```

**Full sealed vote generation flow:**

```go
func GenerateSealedVote(input *SealedVoteInput) (*SealedVoteProof, error) {
    // 1. Seal the vote (commitment + encryption)
    sealed, err := tle.SealVote(input.MasterPublicKey, input.VoteOption, nil)
    if err != nil {
        return nil, err
    }

    // 2. Compute nullifier
    nullifier := tle.ComputeNullifier(input.SecretKey, input.ProposalID)

    // 3. Generate ZK proof (proves voter eligibility without revealing identity)
    voteProof, err := GenerateVoteProof(&VoteInput{
        SecretKey:   input.SecretKey,
        ProposalID:  input.ProposalID,
        VoteOption:  0, // Option is hidden in commitment
        MerkleRoot:  input.MerkleRoot,
        MerkleProof: input.MerkleProof,
    })
    if err != nil {
        return nil, err
    }

    // 4. Save salt locally for manual reveal fallback
    tle.SaveSealedVote(sealed, input.ProposalID, nullifier, "~/.sparkdream/sealed_votes/"+proposalID+".json")

    return &SealedVoteProof{
        Nullifier:       nullifier,
        Commitment:      sealed.Commitment,
        ProofBytes:      voteProof.ProofBytes,
        EncryptedReveal: sealed.EncryptedReveal,
    }, nil
}
```

### Anonymous Proposal Proof Generation

```go
func GenerateProposalProof(input *ProposalInput) (*ProposalProof, error) {
    prover, err := LoadProver("proposal_proving_key.bin")
    if err != nil {
        return nil, err
    }

    // Compute nullifier (domain-separated, epoch+nonce-bound)
    nullifier := HashMiMC([]byte("propose"), input.SecretKey, input.Epoch, input.Nonce)

    // Generate ZK proof
    proof, err := prover.Prove(&ProposalCircuitAssignment{
        // Public
        MerkleRoot: input.MerkleRoot,
        Nullifier:  nullifier,
        Epoch:      input.Epoch,
        Nonce:      input.Nonce,
        MaxNonce:   input.MaxProposalsPerEpoch - 1,
        // Private
        SecretKey:    input.SecretKey,
        PathElements: input.MerkleProof.PathElements,
        PathIndices:  input.MerkleProof.PathIndices,
    })
    if err != nil {
        return nil, err
    }

    return &ProposalProof{
        Nullifier:  nullifier,
        Nonce:      input.Nonce,
        ProofBytes: proof.Serialize(),
    }, nil
}
```

## Client Architecture

### Account-Derived Key Management

The ZK voting key is derived from the user's existing Cosmos account key, eliminating separate key management entirely. This is account abstraction for the privacy layer — the user manages ONE key (their wallet), and the voting identity is implicit.

**Derivation process:**

1. The user's wallet signs a fixed domain string: `sig = sign("sparkdream-vote-identity-v1", accountPrivKey)`
2. The ZK secret key is derived: `zkSecretKey = hash(sig)`
3. The ZK public key is computed: `zkPublicKey = hash(zkSecretKey)`
4. The encryption keypair is derived: `encSeed = hash("encryption", zkSecretKey)` → Babyjubjub keypair

**Properties:**

- **One key to manage**: The user's Cosmos account key (via Keplr, Ledger, or seed phrase) is the only key. The ZK voting identity is derived from it automatically.
- **Recoverable**: Same seed phrase → same account key → same signature → same ZK key. If the user recovers their wallet on a new device, their voting identity is restored automatically.
- **Works with existing wallets**: Keplr, Ledger, and other Cosmos wallets already support message signing. No wallet changes needed.
- **Deterministic**: RFC 6979 deterministic signing ensures the same account always produces the same ZK identity. No randomness, no variation across devices.
- **Secure**: An observer cannot derive the signature from the account's public key (that would break the signature scheme). The derivation is one-way and private.
- **Wallet compatibility**: The domain string signing MUST use a deterministic scheme (RFC 6979 for secp256k1). All standard Cosmos wallets (Keplr, Ledger, Cosmostation) are RFC 6979 compliant. The app should verify determinism on first derivation by signing twice and comparing — if signatures differ, the wallet is incompatible and the app must fall back to random key generation with explicit backup.

**Account key rotation**: If the user rotates their Cosmos account key (e.g., via migration), their ZK key must also rotate. The app detects the mismatch (stored ZK key doesn't match re-derived key) and auto-submits `MsgRotateVoterKey` with the new derivation.

### Automated Registration

Voter registration is invisible to the user — it happens automatically as part of existing flows.

**Founding members (post-launch):** Founding members are registered in x/rep at genesis but do NOT have voter registrations at genesis. When a founder first opens the web app after launch:

1. App connects their wallet and detects they are an x/rep member
2. App queries x/vote — no voter registration found
3. App derives ZK key from account signature (see above)
4. App submits `MsgRegisterVoter` automatically
5. Founder sees a brief "Setting up your voting identity..." message, then they're ready

This keeps the genesis ceremony simple (no ZK key generation required) and ensures all members — founders and future — follow the same onboarding path.

**New members (invitation acceptance):** When a user accepts an x/rep invitation:

1. App derives their ZK key from their account signature
2. App bundles `MsgAcceptInvitation` + `MsgRegisterVoter` in a single transaction
3. User sees "Welcome to Spark Dream!" — voter registration happened transparently

**Result:** No user ever manually "registers for voting." The app handles it invisibly. From the user's perspective, joining the community and being able to vote are the same event.

**Account key rotation:** When a user opens the app after rotating their Cosmos account key:

1. App derives ZK key from the new account signature
2. App queries x/vote for existing registration by address
3. If `zk_public_key` on-chain doesn't match the newly derived key → app submits `MsgRotateVoterKey`
4. If no registration exists → app submits `MsgRegisterVoter`

This check runs on every app login, ensuring the ZK key stays in sync with the account key automatically. The user sees nothing unless a rotation actually occurs (brief "Updating voting identity..." message).

### Automated Sealed Reveals (Threshold Timelock Encryption)

Sealed votes use a two-phase commit-reveal process. Without automation, voters must return during the reveal period to unveil their vote — a significant UX burden that would cause most sealed votes to expire as abstentions. x/vote solves this with **threshold timelock encryption (TLE)** to make sealed voting fire-and-forget.

**How threshold timelock encryption works:**

The validator set collectively maintains a threshold encryption scheme (identity-based encryption keyed to epoch numbers). Think of it as a time-locked safe: data encrypted for "epoch 42" can only be opened once epoch 42 arrives and the validators collectively release that epoch's decryption key. No single validator can decrypt early — it requires a threshold (e.g., 2/3) of validators cooperating.

**At vote time (user does this once, then they're done):**

1. Voter calls `tle.SealVote(masterPublicKey, voteOption, nil)` (or uses the `seal-vote` CLI tool)
2. This computes `commitment = MiMC(voteOption, salt)` and `encrypted_reveal = ECIES.Encrypt(masterPubKey, voteOption || salt)`
3. Voter generates ZK proof of eligibility (via `zkprivatevoting/prover`)
4. Voter submits `MsgSealedVote` with commitment, encrypted_reveal, nullifier, and proof
5. Voter saves salt locally (via `tle.SaveSealedVote()`) for manual reveal fallback
6. The voter never needs to come back. Their job is done.

**At reveal time (fully automatic, no user action):**

1. When the reveal epoch arrives, validators produce decryption key shares
2. Once threshold is met, the epoch decryption key is published on-chain
3. EndBlocker iterates all sealed votes for proposals entering the reveal phase
4. For each vote: decrypt the reveal payload, verify `hash(vote_option, salt) == stored commitment`, update tally
5. If decryption or verification fails for a specific vote, it is left as unrevealed (counts toward quorum but excluded from threshold/veto calculations)

**Trust assumption:** The same as the chain's consensus — if fewer than the threshold (e.g., 2/3) of validators collude, votes remain private until the scheduled reveal time. This is no weaker than the chain's existing security model. Validators who already secure the chain's state transitions also secure vote privacy.

**Privacy from the relayer:** The relayer submits the `MsgSealedVote` on behalf of the voter. The relayer sees the `encrypted_reveal` blob but **cannot decrypt it** — only the validator threshold can, and only at the designated epoch. The relayer learns nothing about the vote content.

**Manual fallback:** `MsgRevealVote` still exists. If a voter's encrypted reveal was corrupted, or they opted out of auto-reveal, they can manually reveal during the reveal period using a relayer. This is a safety net, not the primary path.

### Web Application

The primary user interface is a web application with client-side ZK proof generation.

**Proof generation (WASM):** The `gnark` ZK library (Go-based) compiles to WebAssembly. The WASM module runs in the browser, generating proofs locally — the secret key never leaves the user's device.

| Platform | Proof generation time |
|----------|----------------------|
| Desktop browser | ~3-7 seconds |
| Mobile browser | ~10-15 seconds |

**Key storage:** The derived ZK secret key is stored in the browser's IndexedDB, encrypted via the Web Crypto API. Since the key is deterministically derived from the account signature, it can always be re-derived by reconnecting the wallet — IndexedDB is a cache, not the source of truth.

**User experience:** The user never sees nullifiers, Merkle proofs, ZK circuits, or cryptographic details. The flow looks like:

```
┌──────────────────────────────────────────────┐
│  Proposal #42: Budget Allocation Q2          │
│  SEALED · Ends in 4 days · 73/156 voted      │
│                                              │
│  ┌────────────┐  ┌────────────┐              │
│  │  ✓  Yes    │  │     No     │              │
│  └────────────┘  └────────────┘              │
│                                              │
│  ⏳ Generating proof...  (4s)                │
│                                              │
│  Your vote is anonymous and will be          │
│  revealed automatically when voting ends.    │
│                                              │
│  [ Submit Vote ]                             │
└──────────────────────────────────────────────┘
```

The user clicks "Vote", waits a few seconds for a spinner, and they're done. For sealed votes, the auto-reveal happens without any further user interaction.

### Relayer Service

Relayers are stateless services that accept anonymous vote/proposal payloads and submit them on-chain.

**Interface:**

```
POST /submit-vote       — Accept proof + nullifier, wrap in MsgVote or MsgSealedVote, submit
POST /submit-proposal   — Accept anonymous proposal proof, wrap in MsgCreateAnonymousProposal, submit
```

**Economics:** Relaying is a community service. Gas costs are minimal (~50k gas per vote, comparable to a simple token transfer). Councils, validators, or community members may operate relayer services. No protocol-level relayer incentives at launch.

**Spam protection:** Invalid proofs waste the relayer's gas on failed transactions. The chain validates proofs before state changes, so invalid submissions are rejected but the relayer still pays gas. This provides natural economic protection. Relayers may implement their own rate limiting (e.g., IP-based, captcha).

**Privacy guarantees:**

**At vote submission time:**

| Vote type | What relayer sees |
|-----------|-------------------|
| PUBLIC | Vote option (plaintext), but NOT voter identity |
| SEALED | Encrypted reveal blob (cannot decrypt), NOT voter identity |
| PRIVATE | Encrypted reveal blob + encrypted proposal text, NOT voter identity |

**At manual reveal time** (fallback only — auto-reveal is the primary path):

| Vote type | What relayer sees |
|-----------|-------------------|
| SEALED (manual reveal) | Vote option + salt (plaintext), but NOT voter identity |
| PRIVATE (manual reveal) | Vote option + salt (plaintext), but NOT voter identity |

Manual reveal is a privacy downgrade: the relayer learns the specific vote choice for a specific nullifier. Auto-reveal via TLE avoids this — the chain decrypts internally and no individual party sees individual vote mappings. Manual reveal exists only as a safety net when TLE fails.

In all cases, the relayer knows that *someone* is acting but cannot determine *who*.

## Threshold Timelock Encryption (TLE)

### Overview

TLE is the cryptographic subsystem that enables fire-and-forget sealed voting. It ensures that `encrypted_reveal` payloads submitted at vote time can only be decrypted after the designated reveal epoch — not earlier, not by the relayer, and not by any single validator.

x/vote uses **ECIES encryption on the BN256 G1 curve** (via `go.dedis.ch/kyber/v4`) combined with **Shamir secret sharing** for threshold key management. The client encrypts sealed vote payloads using the master public key. Decryption requires the master private key, which is reconstructed via Lagrange interpolation once a threshold of validators submit their private key shares.

### Scheme

**Encryption** (client-side, at vote time):
```
plaintext       = voteOption (4 bytes, big-endian uint32) || salt (32 bytes)
encrypted_reveal = ECIES.Encrypt(bn256.G1, masterPubKey, plaintext, nil)
```

The `masterPubKey` is a BN256 G1 point stored in module params (`tle_master_public_key`). The client reads it from on-chain state. ECIES uses SHA256 as the default hash function.

**Decryption** (chain-side, at reveal time):
```
plaintext = ECIES.Decrypt(bn256.G1, epochDecryptionKey, encrypted_reveal, nil)
voteOption = uint32(plaintext[0:4])
salt       = plaintext[4:36]
```

The `epochDecryptionKey` is a BN256 G1 scalar reconstructed from validator shares via Lagrange interpolation.

### Validator Key Management

**Distributed Key Generation (DKG):** At chain launch (or when TLE is first enabled), validators participate in a DKG ceremony to produce:

1. A **master public key** (stored on-chain in params as `tle_master_public_key`) — the BN256 G1 point `masterSecret * G`
2. **Individual private key shares** for each validator (stored locally by each validator, NEVER on-chain) — Shamir polynomial evaluations
3. **Individual public key shares** for each validator (stored on-chain as `TLEValidatorShare`) — the BN256 G1 points `share_i * G`, used to verify correctness on `MsgSubmitDecryptionShare`

The DKG ceremony uses Shamir secret sharing over the BN256 scalar field:
- A trusted dealer (or distributed protocol) generates a master secret scalar and a polynomial of degree `threshold - 1`
- Each validator receives their share: the polynomial evaluated at their 1-based index
- Each validator's public key share is `share_i * G` (scalar multiplication with the base point)
- The master public key is `masterSecret * G` (also computable via Lagrange interpolation on the public key shares)
- Each validator registers their public key share on-chain via `MsgRegisterTLEShare` (submitted once after DKG completes)

A dealer-based DKG tool is provided at `zkprivatevoting/cmd/tle-dkg`. For production, this should be replaced with a proper distributed protocol (Pedersen DKG or FROST) where no single party knows the master secret.

**Epoch Decryption Key Production:** Each epoch, validators submit their raw private key share scalar via `MsgSubmitDecryptionShare`:

```
decryptionShare = validatorPrivateScalar  // the raw Shamir share bytes
```

The chain validates the share by computing `share * G` and comparing it against the validator's registered public key share. This scalar-to-point check prevents malicious validators from submitting garbage shares. The `correctness_proof` field is reserved for future use (e.g., DLEQ proofs) but is currently unused — the scalar verification is sufficient.

**Reconstruction:** After each `MsgSubmitDecryptionShare`, `tryReconstructEpochKey` checks whether enough shares have been collected. Once `ceil(tle_threshold_numerator / tle_threshold_denominator * registeredValidators)` valid shares are available:

```
epochDecryptionKey = share.RecoverSecret(bn256.G1, priShares, threshold, totalValidators)
```

The reconstructed scalar is stored as an `EpochDecryptionKey` and an `EventEpochDecryptionKeyAvailable` is emitted. The EndBlocker then uses this key to auto-reveal sealed votes via ECIES decryption.

### Timing

Validators should submit decryption shares for epoch N during the first few blocks after epoch N starts. Typical timeline:

```
Epoch N starts
  → Block N+0 to N+5: validators submit decryption shares
  → Block ~N+3: threshold met, epoch decryption key aggregated and stored
  → Same block or next: EndBlocker auto-reveals sealed votes for proposals with reveal_epoch = N
```

**If the threshold is NOT met** during the entire reveal period (e.g., too many validators offline):
- Auto-reveal fails for affected proposals
- Manual reveals via `MsgRevealVote` still work throughout the reveal period
- An `EventTLEKeyUnavailable` is emitted when the reveal period ends without an available key
- Unrevealed votes (neither auto-revealed nor manually revealed) count toward quorum but are excluded from threshold/veto calculations

This is a graceful degradation — TLE failure does not break voting, it only degrades UX back to the manual reveal model.

### Validator Set Changes

When the validator set changes (new validators join, old ones leave):

- **New validators**: Receive key shares via a **proactive secret sharing (PSS)** protocol. Existing validators collaborate to issue shares to the new validator without revealing the master secret. The new validator can immediately begin contributing decryption shares for future epochs.
- **Departing validators**: Their shares become stale after the next key refresh. They cannot contribute to future epoch decryptions. Old shares they submitted remain valid for their target epochs.
- **Key refresh**: Triggered automatically when the bonded validator set changes by more than 1/3 of its members since the last refresh, or periodically (e.g., every 100 epochs). The refresh redistributes key shares without changing the master public key — clients continue encrypting with the same key.
- **Master public key**: Does NOT change during refresh. Only changes if governance explicitly triggers a full DKG re-ceremony (e.g., due to a security concern).

### Genesis Bootstrapping

TLE is bootstrapped alongside chain genesis:

1. Founders participate in a DKG ceremony during the pre-launch setup phase
2. The master public key is set in genesis params (`tle_master_public_key`)
3. Initial validators receive their key shares out-of-band during the ceremony
4. After genesis, the first epoch's decryption key is produced normally (validators submit shares)

If TLE is not ready at genesis, set `tle_enabled = false` in params. Sealed votes will require manual reveal (the `encrypted_reveal` field is ignored). TLE can be enabled later via governance param change after a DKG ceremony completes. Enabling TLE is non-breaking — proposals created before TLE was enabled use manual reveal; proposals created after use auto-reveal.

### Security Properties

| Property | Guarantee |
|----------|-----------|
| **Confidentiality** | Encrypted reveals cannot be decrypted before the target epoch, assuming fewer than `threshold` validators collude |
| **Correctness** | Correctness proofs prevent malicious validators from submitting invalid shares that would corrupt the epoch key |
| **Liveness** | If threshold validators are online and honest, the epoch key is produced within ~5 blocks of epoch start |
| **Degradation** | If TLE fails (threshold not met), the system degrades gracefully to manual reveals — votes are not lost |
| **Forward secrecy** | Epoch keys are derived per-epoch. Compromising one epoch's key does not reveal other epochs' keys |

**Collusion threshold:** If `tle_threshold_numerator / tle_threshold_denominator` (default 2/3) of bonded validators collude, they can decrypt sealed votes before the reveal epoch. This is the same trust assumption as the chain's consensus (2/3 Byzantine fault tolerance). If the validator set is already compromised at that level, TLE privacy is the least of the chain's problems.

**State size:** Each `TLEDecryptionShare` is ~150 bytes. With 100 validators and threshold of 67, that's ~10KB per epoch of share data. Shares are pruned after the epoch key is aggregated. The aggregated `EpochDecryptionKey` is ~100 bytes per epoch, pruned after all dependent proposals are finalized.

### Validator Obligations

Validators who participate in the DKG ceremony (and thus hold key shares) are expected to submit decryption shares each epoch. The protocol enforces participation through two mechanisms:

**Liveness tracking:** The module tracks each validator's TLE participation rate over a rolling window (e.g., last 100 epochs). Validators who fail to submit shares for more than `tle_miss_tolerance` epochs within the window (default: 10) are flagged as TLE-inactive.

**Consequences of non-participation:**
- **Soft penalty (default)**: TLE-inactive validators are excluded from the participation denominator for threshold calculation. This prevents chronically absent validators from permanently degrading TLE liveness. Example: if 5 of 100 validators are TLE-inactive, the threshold is calculated from the remaining 95.
- **Jailing (optional, governance-configurable)**: If `tle_jail_enabled = true`, validators exceeding the miss tolerance are jailed (same mechanism as consensus liveness jailing). This is aggressive and disabled by default — it can be enabled via governance once TLE is proven stable.

**No additional rewards:** TLE share submission is part of a validator's duties, like signing blocks. The existing staking rewards compensate validators for all protocol responsibilities. Adding separate TLE rewards would complicate tokenomics without clear benefit.

**Params:** `tle_miss_window` (field 25), `tle_miss_tolerance` (field 26), and `tle_jail_enabled` (field 27) in the Params proto control liveness enforcement. See the Params section for definitions and defaults.

## Operational Runbook

This section documents every manual action required to operate the x/vote module end-to-end, organized by role.

### Prerequisites

Before any TLE or ZK voting operations, two one-time setup ceremonies must be completed:

**1. ZK Trusted Setup** (produces proving/verifying keys for vote circuits):

```bash
# Run the trusted setup ceremony (one-time, produces ~50-100 MB proving key)
go run ./zkprivatevoting/cmd/setup

# Output files:
#   proving_key.bin    — distribute to voters (for client-side proof generation)
#   verifying_key.bin  — embed in chain genesis params (VoteVerifyingKey)
#   verifying_key.hex  — hex-encoded for genesis JSON
#   circuit.r1cs       — compiled circuit (optional, speeds up proving)
```

The verifying key must be set in module params (`vote_verifying_key`) via governance or genesis. The proving key must be distributed to all voter clients (web app, CLI).

**2. TLE DKG Ceremony** (produces threshold encryption keys for validators):

```bash
# Run dealer-based DKG ceremony (one-time per validator set)
# threshold = minimum shares for reconstruction (typically ceil(2/3 * validators))
go run ./zkprivatevoting/cmd/tle-dkg \
  --threshold 2 \
  --validators 3 \
  --output ./tle-shares

# Output files:
#   tle-shares/master.json       — master public key (set as TleMasterPublicKey param)
#   tle-shares/validator_1.json  — share for validator 1 (distribute securely)
#   tle-shares/validator_2.json  — share for validator 2 (distribute securely)
#   tle-shares/validator_3.json  — share for validator 3 (distribute securely)
```

The master public key must be set in module params (`tle_master_public_key`) via governance. Each validator receives exactly one share file — securely, out-of-band. **Delete the originals after distribution.**

> **Production note:** The `tle-dkg` tool uses a dealer-based DKG where one party generates all shares. For production deployments, replace this with a proper distributed protocol (Pedersen DKG or FROST) where no single party ever knows the master secret.

### Validator Operations

#### 1. Register TLE Share (One-Time)

After receiving their share file from the DKG ceremony, each validator registers their public key share on-chain. This is done once and persists until the validator is tombstoned or a new DKG ceremony is held.

```bash
# Read the share file to get the public key share hex and share index
cat tle-shares/validator_1.json
# {
#   "share_index": 1,
#   "private_scalar_hex": "...",     ← KEEP SECRET, needed per-epoch
#   "public_key_share_hex": "..."    ← submitted on-chain
# }

# Register the public key share
sparkdreamd tx vote register-tle-share \
  <public_key_share_hex> \
  <share_index> \
  --from <validator_key>
```

**On-chain guards** (enforced by `MsgRegisterTLEShare` handler):
- Sender must be a bonded validator (checked via staking keeper)
- Share index must be positive (1-based)
- Public key share must be a valid BN256 G1 point
- Share index must not be already registered by another validator

**Re-registration:** A validator can re-register (e.g., after key rotation) by submitting a new `MsgRegisterTLEShare`. The old share is replaced.

#### 2. Submit Decryption Share (Per Epoch)

Each epoch that has sealed votes awaiting reveal, validators must submit their private key share so the chain can reconstruct the epoch decryption key.

```bash
# Submit decryption share for the current epoch
# The decryption_share is the private_scalar_hex from your validator_N.json file
sparkdreamd tx vote submit-decryption-share \
  <epoch_number> \
  <private_scalar_hex> \
  --from <validator_key>
```

**Timing:** Submit within the first few blocks after an epoch starts. Typical timeline:
- Epoch N starts
- Blocks N+0 to N+5: validators submit decryption shares
- Block ~N+3: threshold met, epoch key reconstructed and stored
- Same or next block: EndBlocker auto-reveals sealed votes

**Automation:** This should be automated via a validator sidecar process that:
1. Monitors for new epochs (subscribe to `EventEpochDecryptionKeyAvail` or poll block heights)
2. Checks if sealed votes exist for the epoch
3. Submits the decryption share automatically

> **No sidecar is currently provided.** Validators must either submit manually or build their own automation. A sidecar implementation is a future enhancement.

**What happens if validators don't submit:**
- If fewer than threshold validators submit → epoch key is never reconstructed
- Auto-reveal fails for affected proposals
- Voters must manually reveal via `MsgRevealVote` during the reveal period
- Unrevealed votes count toward quorum but are excluded from threshold/veto calculations
- Validators who chronically miss submissions are tracked via `tle_miss_window` / `tle_miss_tolerance` params

#### 3. Validator Set Changes

- **New validators:** Must receive a key share via a proactive secret sharing (PSS) protocol or a new DKG ceremony. Until they have a share, they cannot participate in TLE.
- **Departing validators:** Their shares become stale. Existing submitted shares remain valid for their target epochs.
- **Key refresh:** Triggered when the validator set changes significantly. Redistributes shares without changing the master public key.

### Voter Operations

#### 1. Register as Voter (One-Time, Automated)

Voter registration is handled automatically by the web app on first login. For CLI usage:

```bash
# Derive ZK key from account and register
# The web app does this automatically — CLI is for development/debugging
sparkdreamd tx vote register --from alice
```

The ZK secret key is derived deterministically from the Cosmos account key signature (see Account-Derived Key Management). No separate key backup is needed.

#### 2. Seal a Vote (Per Sealed Proposal)

For SEALED or PRIVATE visibility proposals, voters must encrypt their vote. The `seal-vote` CLI tool handles this:

```bash
# Get the master public key from chain params
sparkdreamd query vote params | grep tle_master_public_key

# Seal the vote
go run ./zkprivatevoting/cmd/seal-vote \
  --master-pubkey <tle_master_public_key_hex> \
  --vote-option 1 \
  --secret-key <your_zk_secret_key_hex> \
  --proposal-id 42 \
  --save ~/.sparkdream/sealed_votes/proposal_42.json

# Output includes:
#   commitment (hex)      — submit as vote_commitment
#   encrypted_reveal (hex) — submit as encrypted_reveal
#   nullifier (hex)       — submit as nullifier
#   salt (hex)            — SAVE for manual reveal fallback
```

The voter then submits the sealed vote on-chain (the ZK proof must be generated separately via the prover):

```bash
sparkdreamd tx vote cast-sealed <proposal_id> <vote_option> \
  --from alice \
  --submitter relayer
```

**Or, using raw message fields:**

```bash
sparkdreamd tx vote sealed-vote \
  --proposal-id 42 \
  --nullifier <nullifier_hex> \
  --vote-commitment <commitment_hex> \
  --proof <zk_proof_hex> \
  --encrypted-reveal <encrypted_hex> \
  --from <voter_key>
```

**What happens next:** The voter's job is done. The EndBlocker will auto-reveal the vote when the epoch decryption key becomes available.

#### 3. Generate ZK Proof (Per Vote)

The `seal-vote` tool outputs commitment and encrypted reveal but **not** the ZK proof. The ZK proof must be generated separately using the prover:

```go
import "sparkdream/zkprivatevoting/prover"

p, err := prover.NewVoteProver("proving_key.bin", "circuit.r1cs")
output, err := p.GenerateProof(&prover.VoteProofInput{
    SecretKey:   secretKey,        // 32-byte ZK secret key
    VotingPower: 1,                // always 1 (equal-weight voting)
    ProposalID:  proposalID,
    VoteOption:  voteOption,       // 0-based option index
    MerkleRoot:  merkleRoot,       // from proposal's voter tree snapshot
    MerkleProof: merkleProof,      // from query vote merkle-proof
})
// output.ProofBytes → submit as proof field
```

The proving key file (~50-100 MB) must be available locally. The web app downloads it once and caches it.

#### 4. Manual Vote Reveal (Fallback)

If TLE auto-reveal fails (not enough validators submitted decryption shares), voters can manually reveal during the reveal period:

```bash
# Load saved sealed vote data
cat ~/.sparkdream/sealed_votes/proposal_42.json
# {
#   "proposal_id": 42,
#   "vote_option": 1,
#   "salt_hex": "...",
#   "nullifier_hex": "..."
# }

# Submit manual reveal
sparkdreamd tx vote reveal-vote \
  --proposal-id 42 \
  --nullifier <nullifier_hex> \
  --vote-option 1 \
  --reveal-salt <salt_hex> \
  --from <voter_key>
```

**Without the salt, manual reveal is impossible.** Always save the sealed vote data file. If auto-reveal succeeds (the normal case), the saved file can be deleted.

### Chain Admin / Governance Operations

#### 1. Set TLE Master Public Key

After the DKG ceremony, the master public key must be set in module params:

```bash
# Read master public key from DKG output
cat tle-shares/master.json | jq -r .master_public_key_hex

# Submit governance proposal to update params (or use council-governed param update)
sparkdreamd tx vote update-params \
  --tle-master-public-key <master_public_key_hex> \
  --from <governance_authority>
```

#### 2. Enable/Disable TLE

TLE can be toggled via params without a chain upgrade:

```bash
# Enable TLE (after DKG ceremony + master public key is set)
sparkdreamd tx vote update-params --tle-enabled true --from <governance_authority>

# Disable TLE (sealed votes fall back to manual reveal)
sparkdreamd tx vote update-params --tle-enabled false --from <governance_authority>
```

#### 3. Store SRS (ZK Trusted Setup)

The Structured Reference String from the trusted setup ceremony must be stored on-chain:

```bash
sparkdreamd tx vote store-srs <srs_bytes_file> --from <governance_authority>
```

### Checklist: Launching x/vote from Genesis

| Step | Actor | Command / Action | One-Time? |
|------|-------|------------------|-----------|
| 1 | Admin | Run ZK trusted setup (`cmd/setup`) | Yes |
| 2 | Admin | Embed `verifying_key.hex` in genesis params | Yes |
| 3 | Admin | Run DKG ceremony (`cmd/tle-dkg`) | Yes |
| 4 | Admin | Set `tle_master_public_key` in genesis params | Yes |
| 5 | Admin | Distribute validator share files securely | Yes |
| 6 | Each validator | Register TLE share on-chain (`register-tle-share`) | Yes |
| 7 | Admin | Set `tle_enabled = true` in params | Yes |
| 8 | Each voter | Register (automatic on first web app login) | Yes |
| 9 | Each voter | Vote on proposals (automatic proof gen + sealing) | Per vote |
| 10 | Each validator | Submit decryption share per epoch (sidecar or manual) | Per epoch |

## Security Considerations

### Trusted Setup

PLONK uses a **universal structured reference string (SRS)** — a single trusted setup that works for all circuits. The "toxic waste" from setup must be destroyed, but only one ceremony is needed regardless of how many circuits are added or modified.

**Approach:**
1. **Testnet**: Local single-party setup (acceptable for development)
2. **Pre-mainnet**: MPC ceremony with founders (secure if at least 1 party is honest)
3. **Community SRS**: Can adopt an existing community-generated SRS (e.g., Ethereum's KZG ceremony) if the curve matches

Circuit upgrades (adding constraints, changing hash functions) only require recomputing verifying keys from the same SRS — no new ceremony.

### Hash Function

MiMC is used for SNARK efficiency (~320 constraints per hash vs ~25,000 for SHA-256). While it has less cryptanalysis than traditional hashes, it has been extensively studied and is considered secure for this application. Poseidon is an alternative with even fewer constraints (~8x faster) that could be adopted later.

### Key Security

The ZK voting key is derived from the user's Cosmos account key (see Client Architecture > Account-Derived Key Management). This simplifies the security model:

- **Account key loss**: Recover the Cosmos account from seed phrase → ZK key is re-derived automatically
- **Account key theft**: Attacker can derive the ZK key and vote on behalf of the user. Same severity as losing the account key itself — the ZK key adds no new attack surface.
- **Device loss**: Re-derive everything from the seed phrase on a new device. No separate backup needed.
- **Account key rotation**: App auto-rotates the ZK key via `MsgRotateVoterKey`. In-flight proposals use old snapshots (old key still works for those).

### Privacy Guarantees

| Property | PUBLIC | SEALED | PRIVATE |
|----------|--------|--------|---------|
| Voter identity hidden | Yes | Yes | Yes |
| Votes unlinkable across proposals | Yes | Yes | Yes |
| Individual vote choice hidden during voting | No | Yes | Yes |
| Individual vote choice hidden after reveal | No | No | No |
| Proposal text hidden from non-voters | No | No | Yes |
| Final tally visible | Yes | Yes (after reveal) | Yes |
| Proposal outcome visible | Yes | Yes | Yes |

PRIVATE mode's unique value: non-voters can see that a vote occurred and its outcome, but cannot read what was voted on. This is useful for committee decisions where the subject matter is sensitive but the result must be enforceable.

### Coercion Resistance

**Limitation**: A voter can prove their vote to a coercer by revealing their secret key and vote choice. This is inherent to most ZK voting schemes.

**Mitigations** (future enhancements):
- **Revote period**: Allow voters to change their vote within a window, making coercion proofs unreliable
- **Designated verifier proofs**: Proofs that only a specific verifier can check (can't be forwarded to coercer)

Note: The TLE system used for auto-reveals does NOT address coercion. TLE prevents *early* reveal (before the voting period ends), but a coerced voter can still prove their vote by sharing their secret key with the coercer, who can then derive the salt and verify the vote choice. Coercion resistance requires fundamentally different mechanisms (revoting, deniability).

### Anonymity Set Size

Privacy strength depends on the anonymity set size. Since all votes carry equal weight (one member, one vote), there is no power-based metadata to narrow down voters. Anonymity depends only on the number of eligible voters:

- **Large public votes** (100+ voters): Strong anonymity
- **Committee votes** (5-10 voters): Reasonable anonymity; SEALED mode recommended to hide choices during deliberation
- **Jury votes** (3-5 jurors): Minimal anonymity set — SEALED mode strongly recommended

### Timing Analysis

Vote submission timing can leak information. Mitigations:
- The relayer/submitter pattern (anyone can submit) helps — voters can use different submitters
- Batched submission: accumulate votes and submit in batches
- Random delay: client adds random delay before submission

### Relayer Economics

The submitter/relayer pays gas for anonymous votes and proposals. Since anyone can submit on behalf of an anonymous voter, this enables privacy but raises the question of who pays.

**Current design**: No protocol-level relayer incentives. Relaying is a community service:
- Council members or validators may run relayer services for their community
- Organizations can operate relayers for their members
- The gas cost per vote is minimal (~50k gas, comparable to a simple token transfer)

**Spam prevention**: Relayers submitting invalid proofs waste gas on failed transactions. The chain validates proofs before state changes, so invalid submissions are rejected but the relayer still pays gas. This provides natural economic protection — a spam relayer only hurts themselves.

**Future consideration**: An optional `relayer_fee` param could allow the module to compensate relayers from the community pool, but this adds complexity and is not needed for launch.

## Genesis State

Voter registrations are **empty at genesis**. Founding members register post-launch using the same auto-registration flow as all other members — the app derives their ZK key from their account signature on first login. This keeps the genesis ceremony simple (no ZK key generation required) and ensures all members follow the same onboarding path.

```protobuf
message GenesisState {
  Params params = 1;
  repeated VotingProposal voting_proposal_list = 2;          // All proposals
  uint64 voting_proposal_count = 3;                          // Next proposal ID counter
  repeated VoterRegistration voter_registration_map = 4;     // Typically empty at genesis
  repeated AnonymousVote anonymous_vote_map = 5;
  repeated SealedVote sealed_vote_map = 6;
  repeated VoterTreeSnapshot voter_tree_snapshot_map = 7;
  repeated UsedNullifier used_nullifier_map = 8;
  repeated UsedProposalNullifier used_proposal_nullifier_map = 9;
  repeated TleValidatorShare tle_validator_share_map = 10;   // Validator public key shares from DKG
  repeated TleDecryptionShare tle_decryption_share_map = 11; // In-progress epoch shares (preserved across genesis export/import)
  repeated EpochDecryptionKey epoch_decryption_key_map = 12; // TLE epoch keys (typically empty at genesis)
  SrsState srs_state = 13;                                   // Full SRS bytes (large — stored separately from Params)
}
```

## Default Parameters

```go
var DefaultParams = Params{
    // ZK circuits (PLONK)
    SRSHash:              nil, // SHA-256 hash of the SRS (set after SRS is stored via governance)
    VoteVerifyingKey:     nil, // Derived from SRS for vote circuit
    ProposalVerifyingKey: nil, // Derived from SRS for proposal circuit
    TreeDepth:            20,  // ~1M voters

    // Voting periods
    MinVotingPeriodEpochs:     3,   // ~3 days minimum
    MaxVotingPeriodEpochs:     30,  // ~1 month maximum
    DefaultVotingPeriodEpochs: 7,   // ~1 week default
    SealedRevealPeriodEpochs:  3,   // ~3 days for reveal

    // Thresholds
    DefaultQuorum:         sdk.NewDecWithPrec(33, 2),  // 33%
    DefaultThreshold:      sdk.NewDecWithPrec(50, 2),  // 50% — strict ">" means this requires a majority (>50%)
    DefaultVetoThreshold:  sdk.NewDecWithPrec(334, 3), // 33.4% (matching x/gov)

    // Registration
    OpenRegistration:     true,
    MinRegistrationStake: math.ZeroInt(),

    // Rate limiting
    MaxProposalsPerEpoch: 1,

    // Privacy
    AllowPrivateProposals:      true,
    AllowSealedProposals:       true,
    MaxPrivateEligibleVoters:   50, // Caps encrypted key share storage

    // Deposit
    MinProposalDeposit: sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_000_000)), // 1 SPARK

    // Vote options
    MinVoteOptions: 2,  // At least two choices (e.g., yes/no)
    MaxVoteOptions: 10, // Prevent state bloat

    // Threshold timelock encryption (TLE)
    TLEEnabled:                 true,
    TLEThresholdNumerator:      2,
    TLEThresholdDenominator:    3,   // 2/3 of bonded validators
    TLEMasterPublicKey:         nil, // Must be set from DKG ceremony
    MaxEncryptedRevealBytes:    512, // ~300 bytes legitimate, 512 with margin
    TLEMissWindow:              100, // Rolling window in epochs
    TLEMissTolerance:           10,  // Max missed epochs before flagged TLE-inactive
    TLEJailEnabled:             false, // Jail TLE-inactive validators (disabled by default)

    // Epoch fallback
    BlocksPerEpoch:             17280, // ~1 day at 5s blocks (used when x/season unavailable)
}
```

## Events

Events are emitted as flat string-attribute pairs (Cosmos SDK `sdk.Event` style), not as typed protobuf messages. Each event has a type string and a set of key-value attributes.

```go
// Event type constants
const (
    EventVoterRegistered         = "voter_registered"
    EventVoterDeactivated        = "voter_deactivated"
    EventVoterKeyRotated         = "voter_key_rotated"
    EventProposalCreated         = "proposal_created"
    EventVoteCast                = "vote_cast"
    EventSealedVoteCast          = "sealed_vote_cast"
    EventSealedVoteRevealed      = "sealed_vote_revealed"
    EventProposalCancelled       = "proposal_cancelled"
    EventProposalFinalized       = "proposal_finalized"
    EventProposalExecutionFailed = "proposal_execution_failed"
    EventEpochDecryptionKeyAvail = "epoch_decryption_key_available"
    EventTLEKeyUnavailable       = "tle_key_unavailable"
    EventTLEShareRegistered      = "tle_share_registered"
    EventDecryptionShareSubmit   = "decryption_share_submitted"
    EventSRSStored               = "srs_stored"
)

// Attribute key constants
const (
    AttributeProposalID  = "proposal_id"
    AttributeVoter       = "voter"
    AttributeValidator   = "validator"
    AttributeNullifier   = "nullifier"
    AttributeVoteOption  = "vote_option"
    AttributeOutcome     = "outcome"
    AttributeVisibility  = "visibility"
    AttributeReason      = "reason"
    AttributeEpoch       = "epoch"
    AttributeStatus      = "status"
    AttributeShareIndex  = "share_index"
    AttributeVoterCount  = "voter_count"
    AttributeMerkleRoot  = "merkle_root"
    AttributeTotalVotes  = "total_votes"
    AttributeProposer    = "proposer"
    AttributeDeposit     = "deposit"
    AttributeVotingStart = "voting_start"
    AttributeVotingEnd   = "voting_end"
)
```

**Event details:**

| Event | Attributes | Notes |
|-------|-----------|-------|
| `voter_registered` | `voter` | Emitted on `MsgRegisterVoter` |
| `voter_deactivated` | `voter` | Emitted on `MsgDeactivateVoter` |
| `voter_key_rotated` | `voter` | Emitted on `MsgRotateVoterKey` |
| `proposal_created` | `proposal_id`, `visibility`, `voter_count` | Emitted for both anonymous and public proposals |
| `vote_cast` | `proposal_id`, `nullifier`, `vote_option` | PUBLIC votes — option visible |
| `sealed_vote_cast` | `proposal_id`, `nullifier` | SEALED/PRIVATE votes — option hidden |
| `sealed_vote_revealed` | `proposal_id`, `nullifier`, `vote_option` | After manual or auto-reveal |
| `proposal_cancelled` | `proposal_id`, `reason` | Emitted on `MsgCancelProposal` |
| `proposal_finalized` | `proposal_id`, `outcome`, `total_votes` | EndBlocker finalization |
| `proposal_execution_failed` | `proposal_id`, `reason` | Message execution failure (vote result still stands) |
| `epoch_decryption_key_available` | `epoch` | TLE: epoch key reconstructed, auto-reveals can proceed |
| `tle_key_unavailable` | `epoch`, `proposal_id` | TLE: reveal period ended without epoch key |
| `tle_share_registered` | `validator`, `share_index` | Emitted on `MsgRegisterTLEShare` |
| `decryption_share_submitted` | `validator`, `epoch` | Emitted on `MsgSubmitDecryptionShare` |
| `srs_stored` | (none) | Emitted on `MsgStoreSRS` |

## Errors

```go
var (
    // Registration
    ErrNotAMember             = sdkerrors.Register(ModuleName, 2, "sender is not an active x/rep member")
    ErrAlreadyRegistered      = sdkerrors.Register(ModuleName, 3, "voter already has an active registration")
    ErrNotRegistered          = sdkerrors.Register(ModuleName, 4, "voter is not registered")
    ErrAlreadyInactive        = sdkerrors.Register(ModuleName, 5, "voter registration is already inactive")
    ErrDuplicatePublicKey     = sdkerrors.Register(ModuleName, 6, "zk public key is already registered to another address")
    ErrUseRotateKey           = sdkerrors.Register(ModuleName, 7, "active registration exists — use MsgRotateVoterKey to change keys")
    ErrRegistrationClosed     = sdkerrors.Register(ModuleName, 8, "voter registration is closed (open_registration = false)")
    ErrInsufficientStake      = sdkerrors.Register(ModuleName, 9, "voter does not meet min_registration_stake requirement")

    // Proposals
    ErrProposalNotFound       = sdkerrors.Register(ModuleName, 10, "proposal not found")
    ErrProposalNotActive      = sdkerrors.Register(ModuleName, 11, "proposal is not in ACTIVE status")
    ErrProposalNotTallying    = sdkerrors.Register(ModuleName, 12, "proposal is not in TALLYING status")
    ErrInvalidVisibility      = sdkerrors.Register(ModuleName, 13, "invalid visibility level for this message type")
    ErrPrivateNotAllowed      = sdkerrors.Register(ModuleName, 14, "private proposals are disabled")
    ErrSealedNotAllowed       = sdkerrors.Register(ModuleName, 15, "sealed proposals are disabled")
    ErrNoEligibleVoters       = sdkerrors.Register(ModuleName, 16, "voter tree is empty — no eligible voters")
    ErrTooManyEligibleVoters  = sdkerrors.Register(ModuleName, 17, "eligible voters exceed max_private_eligible_voters")
    ErrInvalidVoteOptions     = sdkerrors.Register(ModuleName, 18, "vote option IDs must be sequential starting from 0")
    ErrVoteOptionsOutOfRange  = sdkerrors.Register(ModuleName, 19, "vote options count outside [min, max] range")
    ErrCancelNotAuthorized    = sdkerrors.Register(ModuleName, 20, "not authorized to cancel this proposal")
    ErrProposalNotCancellable = sdkerrors.Register(ModuleName, 27, "proposal is not in ACTIVE or TALLYING status — cannot cancel")
    ErrVotingPeriodOutOfRange = sdkerrors.Register(ModuleName, 21, "voting period outside [min, max] range")
    ErrInsufficientDeposit    = sdkerrors.Register(ModuleName, 25, "deposit is less than min_proposal_deposit")
    ErrInvalidThreshold       = sdkerrors.Register(ModuleName, 26, "quorum or threshold must be > 0 and <= 1")
    ErrNoStandardOption       = sdkerrors.Register(ModuleName, 22, "at least one vote option must have OPTION_ROLE_STANDARD")
    ErrDuplicateAbstainRole   = sdkerrors.Register(ModuleName, 23, "at most one vote option may have OPTION_ROLE_ABSTAIN")
    ErrDuplicateVetoRole      = sdkerrors.Register(ModuleName, 24, "at most one vote option may have OPTION_ROLE_VETO")

    // Voting & proofs
    ErrNullifierUsed          = sdkerrors.Register(ModuleName, 30, "nullifier has already been used")
    ErrInvalidProof           = sdkerrors.Register(ModuleName, 31, "ZK proof verification failed")
    ErrMerkleRootMismatch     = sdkerrors.Register(ModuleName, 32, "Merkle root does not match proposal snapshot")
    ErrVoteOptionOutOfRange   = sdkerrors.Register(ModuleName, 33, "vote option exceeds proposal option count")
    ErrInvalidCommitment      = sdkerrors.Register(ModuleName, 34, "vote commitment is empty or malformed")
    ErrRevealMismatch         = sdkerrors.Register(ModuleName, 35, "hash(option, salt) does not match stored commitment")
    ErrVoteNotFound           = sdkerrors.Register(ModuleName, 36, "sealed vote with this nullifier not found")
    ErrAlreadyRevealed        = sdkerrors.Register(ModuleName, 37, "sealed vote has already been revealed")

    // Rate limiting
    ErrEpochMismatch          = sdkerrors.Register(ModuleName, 40, "claimed epoch is not within ±1 of current epoch")
    ErrMaxNonceMismatch       = sdkerrors.Register(ModuleName, 41, "MaxNonce does not match params.max_proposals_per_epoch - 1")
    ErrProposalLimitReached   = sdkerrors.Register(ModuleName, 42, "proposal creation nullifier already used this epoch")

    // TLE
    ErrTLENotEnabled          = sdkerrors.Register(ModuleName, 50, "threshold timelock encryption is not enabled")
    ErrNotValidator           = sdkerrors.Register(ModuleName, 51, "sender is not an active bonded validator")
    ErrNoTLEShare             = sdkerrors.Register(ModuleName, 52, "validator has no registered TLE public key share")
    ErrDuplicateDecryptionShare = sdkerrors.Register(ModuleName, 53, "validator already submitted a decryption share for this epoch")
    ErrInvalidCorrectnessProof = sdkerrors.Register(ModuleName, 54, "decryption share correctness proof verification failed")
    ErrEncryptedRevealTooLarge = sdkerrors.Register(ModuleName, 55, "encrypted reveal exceeds max_encrypted_reveal_bytes")
    ErrInvalidPublicKeyShare   = sdkerrors.Register(ModuleName, 56, "public key share is not a valid BN256 G1 point")
    ErrInvalidShareIndex       = sdkerrors.Register(ModuleName, 57, "share index must be positive (1-based)")
    ErrDuplicateShareIndex     = sdkerrors.Register(ModuleName, 58, "share index is already registered by another validator")

    // SRS
    ErrSRSNotStored           = sdkerrors.Register(ModuleName, 60, "SRS not found in state — must be stored before key derivation")
    ErrSRSHashMismatch        = sdkerrors.Register(ModuleName, 61, "stored SRS hash does not match params.srs_hash")
)
```

## CLI Commands

The CLI is primarily a developer/debugging tool. Most users interact through the web app, which handles key derivation, proof generation, and relayer submission automatically. The CLI derives the ZK key from the account key in the local keyring — no separate secret key file.

```bash
# === Registration ===

# Register for anonymous voting (derives ZK key from account signature)
# Normally done automatically by the web app on first login
sparkdreamd tx vote register --from alice

# Rotate ZK key (derives new key from current account key)
sparkdreamd tx vote rotate-key --from alice

# === Anonymous Proposal Creation ===

# Create a PUBLIC anonymous proposal
# CLI derives ZK key from --from account, generates proof, submits via --submitter
sparkdreamd tx vote propose \
  --title "Parameter Update" \
  --description "Change staking APY to 12%" \
  --type parameter-change \
  --voting-period 7 \
  --from alice \           # Account used to derive ZK key (never sent on-chain)
  --submitter relayer      # On-chain submitter (pays gas)

# Create a SEALED anonymous proposal (votes hidden until end, auto-revealed)
sparkdreamd tx vote propose \
  --title "Budget Allocation Q2" \
  --description "..." \
  --type budget-approval \
  --visibility sealed \
  --voting-period 14 \
  --from alice \
  --submitter relayer

# Create a PRIVATE anonymous proposal (proposal text hidden from non-voters)
# NOTE: The CLI encrypts --title and --description client-side before submission.
# On-chain, only encrypted_content is stored. The --title/--description flags
# are convenience inputs — they are NEVER sent as plaintext in the transaction.
sparkdreamd tx vote propose \
  --title "Committee Internal Decision" \
  --description "..." \
  --type general \
  --visibility private \
  --voting-period 7 \
  --from alice \
  --submitter relayer

# Create a non-anonymous (public proposer) proposal
sparkdreamd tx vote propose-public \
  --title "Routine Upgrade" \
  --description "..." \
  --type parameter-change \
  --voting-period 7 \
  --from alice

# === Voting ===

# Vote on a PUBLIC proposal
sparkdreamd tx vote cast [proposal-id] [vote-option] \
  --from alice \           # Account to derive ZK key
  --submitter relayer      # On-chain submitter

# Vote on a SEALED proposal (auto-reveal via TLE — no manual reveal needed)
sparkdreamd tx vote cast-sealed [proposal-id] [vote-option] \
  --from alice \
  --submitter relayer

# Manual reveal fallback (only needed if auto-reveal fails)
sparkdreamd tx vote reveal [proposal-id] \
  --from alice \
  --submitter relayer

# === Cancellation ===

# Cancel a proposal (public proposer or governance)
sparkdreamd tx vote cancel-proposal [proposal-id] \
  --reason "Superseded by proposal #45" \
  --from alice

# === Deactivation ===

# Deactivate voter registration (voluntary — can re-register later)
sparkdreamd tx vote deactivate --from alice

# === TLE (Validator Operations) ===

# Run DKG ceremony (one-time, generates master key + validator shares)
go run ./zkprivatevoting/cmd/tle-dkg --threshold 2 --validators 3 --output ./tle-shares

# Register TLE public key share after DKG ceremony (one-time per validator)
# public_key_share_hex and share_index come from validator_N.json
sparkdreamd tx vote register-tle-share \
  <public_key_share_hex> \
  <share_index> \
  --from validator1

# Submit decryption share for an epoch (per epoch, automate via sidecar)
# private_scalar_hex comes from validator_N.json (KEEP SECRET)
sparkdreamd tx vote submit-decryption-share \
  <epoch> \
  <private_scalar_hex> \
  --from validator1

# === Sealed Vote Preparation ===

# Seal a vote (computes commitment + ECIES-encrypted reveal)
go run ./zkprivatevoting/cmd/seal-vote \
  --master-pubkey <tle_master_public_key_hex> \
  --vote-option 1 \
  --secret-key <zk_secret_key_hex> \
  --proposal-id 42 \
  --save ~/.sparkdream/sealed_votes/proposal_42.json

# === Queries ===

# Get your Merkle proof (needed for voting)
sparkdreamd query vote merkle-proof [proposal-id] [your-zk-public-key]

# Query proposal
sparkdreamd query vote proposal [proposal-id]

# Query tally (respects visibility)
sparkdreamd query vote tally [proposal-id]

# TLE status
sparkdreamd query vote tle-status

# TLE validator shares (all registered validators)
sparkdreamd query vote tle-validator-shares

# TLE epoch decryption key status
sparkdreamd query vote epoch-key [epoch]

```

## State Pruning

To prevent unbounded state growth:

- **Vote nullifiers**: Pruned after proposal finalization + grace period (default: 10 epochs after finalization). The proposal's final status prevents re-voting even without nullifiers.
- **Proposal creation nullifiers**: Pruned after the epoch they belong to is no longer within ±1 of the current epoch (i.e., 3 epochs after creation). The ±1-epoch grace window for proposal creation means nullifiers are only needed for epochs `current-1` through `current+1`.
- **Merkle snapshots**: Pruned when the associated proposal is finalized and the grace period expires.
- **Sealed vote data**: `encrypted_reveal` payloads are cleared from state immediately after successful auto-reveal (before finalization), reclaiming ~300 bytes per vote. The remaining sealed vote fields (commitment, revealed option, salt) are pruned after tally is finalized.
- **Proposal archival**: Finalized proposals older than a configurable age are archived (tally preserved, individual votes pruned).
- **TLE decryption shares**: Pruned immediately after the epoch decryption key is successfully aggregated (shares are no longer needed once the key exists). If aggregation never succeeds (threshold not met), shares are pruned when the epoch exits the rolling miss window.
- **TLE epoch keys**: Pruned after all proposals referencing that `reveal_epoch` are finalized and past the grace period.
- **TLE validator shares** (`TLEValidatorShare`): Persistent — only removed when a validator is tombstoned or explicitly removed from TLE via key refresh.
- **Inactive voter registrations**: Intentionally retained indefinitely. Inactive registrations must persist because (1) the `zk_public_key` uniqueness check across all addresses needs them to prevent key collision, and (2) members reinstated by x/rep can re-register via `MsgRegisterVoter`, which reactivates the existing record. These are small (~100 bytes each) and bounded by total historical membership.

## File References

### ZK Circuit Implementation

```
zkprivatevoting/                    # Client-side cryptographic tooling
├── circuit/
│   └── vote_circuit.go            # ZK circuit definition (Groth16/BN254)
├── crypto/
│   └── crypto.go                  # MiMC hashing, Merkle trees, key management
├── prover/
│   └── prover.go                  # Client-side ZK proof generation
├── tle/                           # Threshold Timelock Encryption tooling
│   ├── dkg.go                     # Dealer-based DKG ceremony (RunDKG, AggregateMasterPublicKey)
│   ├── voter.go                   # Voter-side: SealVote, EncryptVotePayload, ComputeVoteCommitment
│   ├── fileio.go                  # JSON I/O for DKG shares, master key, sealed vote data
│   └── tle_test.go                # End-to-end tests (12 tests)
├── cmd/
│   ├── setup/main.go              # ZK trusted setup ceremony (proving/verifying keys)
│   ├── demo/main.go               # End-to-end voting demo
│   ├── tle-dkg/main.go            # DKG ceremony CLI (generates master key + validator shares)
│   └── seal-vote/main.go          # Seal vote CLI (computes commitment + ECIES-encrypted reveal)
└── README.md

x/vote/keeper/                      # On-chain TLE implementation
├── tle.go                          # ECIES decrypt, correctness verification, epoch key reconstruction
├── tle_test.go                     # TLE crypto tests (7 tests)
├── export_test.go                  # Test helpers exposing unexported functions
├── endblock.go                     # EndBlocker: auto-reveal via decryptTLEPayload
├── msg_server_register_tle_share.go     # MsgRegisterTLEShare handler (with validator/point/index guards)
├── msg_server_submit_decryption_share.go # MsgSubmitDecryptionShare handler (with tryReconstructEpochKey)
└── crypto_stubs.go                 # ZK proof stubs, MiMC commitment hash
```

### Planned File Structure (To Be Created)

```
x/vote/
├── circuits/
│   ├── common/
│   │   ├── merkle.go             # Merkle verification (in-circuit)
│   │   └── utils.go              # Field element helpers
│   ├── setup/
│   │   ├── srs.go                # Universal SRS generation (PLONK)
│   │   └── keys.go               # Per-circuit verifying key derivation
│   ├── vote/
│   │   ├── circuit.go            # Vote circuit definition
│   │   └── circuit_test.go       # Circuit tests
│   └── proposal/
│       ├── circuit.go            # Proposal creation circuit definition
│       └── circuit_test.go       # Circuit tests
├── crypto/
│   ├── merkle.go                 # Off-chain Merkle tree (for building/querying)
│   ├── mimc.go                   # MiMC hash wrapper
│   └── babyjubjub.go            # Encryption key management (PRIVATE mode)
├── client/
│   └── prover/
│       ├── vote_prover.go        # Client-side vote proof generation
│       ├── proposal_prover.go    # Client-side proposal proof generation
│       └── key_manager.go        # Account-derived key management
└── keeper/
    ├── msg_server.go             # Message handlers
    ├── grpc_query.go             # Query handlers
    ├── merkle.go                 # Merkle tree management
    ├── verification.go           # On-chain ZK proof verification
    ├── tally.go                  # Vote tallying logic
    ├── auto_reveal.go            # TLE-based automatic sealed vote reveals
    └── abci.go                   # EndBlocker (finalization + auto-reveal)

web/                               # Web application (separate repo or monorepo)
├── src/
│   ├── wasm/
│   │   └── prover.wasm           # gnark compiled to WebAssembly
│   ├── lib/
│   │   ├── identity.ts           # Account-derived ZK key management
│   │   ├── prover.ts             # WASM prover wrapper
│   │   ├── relayer.ts            # Relayer API client
│   │   └── tle.ts                # TLE encryption (client-side)
│   └── components/
│       ├── VoteButton.tsx         # One-click voting UI
│       ├── ProposalForm.tsx       # Proposal creation (anonymous/public)
│       └── ProofSpinner.tsx       # "Generating proof..." UI
└── public/
    └── vote_proving_key.bin       # Proving key (downloaded once, cached)
```

### Specification References

- `docs/zkproof.md` — ZK circuit tutorial and design discussion
- `docs/x-rep-spec.md` — x/rep module (membership checks)
- `docs/architecture.md` — System overview

## Design Decisions

### 1. PLONK (not Groth16)

**Decision**: Use PLONK with KZG commitments over BN254.

**Rationale**: PLONK's universal trusted setup requires only **one ceremony** for all circuits. Circuit upgrades (adding constraints, changing hash functions, new circuits) only need new verifying keys derived from the same SRS — no additional ceremonies. This minimizes coordination overhead and ceremony fatigue.

**Tradeoffs accepted**:
- Larger proofs (~500 bytes vs ~192 bytes for Groth16) — acceptable for voting use case
- Slightly slower verification (~3-5 ms vs ~1-2 ms) — still fast enough for on-chain verification
- gnark supports both; the circuit code is identical. Only setup/prover/verifier initialization differs

**Migration path**: If a more efficient proving system emerges, migration is a governance param update (new SRS + verifying keys). In-flight proposals keep their old proofs. No hard fork required.

### 2. MiMC (not Poseidon)

**Decision**: Use MiMC for all in-circuit hashing.

**Rationale**: Security over performance. MiMC has extensive cryptanalysis and years of production use (Tornado Cash). The ~320 constraints per hash yield acceptable proof generation times (~3-7 seconds) for a voting use case where speed is not critical.

**Future option**: Poseidon (~40 constraints per hash, ~8x fewer) can be adopted later if mobile proof generation becomes a priority. Migration = same circuit structure, swap hash function, derive new verifying keys from existing SRS (no new ceremony needed thanks to PLONK).

### 3. Atomic Key Rotation via MsgRotateVoterKey

**Decision**: Voters rotate their ZK secret key by submitting `MsgRotateVoterKey` from their registered Cosmos address.

**How it works**:
1. Voter generates a new secret key and computes `newZKPublicKey = hash(newSecretKey)`
2. Submits `MsgRotateVoterKey{voter, newZKPublicKey, newEncPubKey}`
3. Chain replaces old leaf with new leaf in a single atomic operation
4. Tree marked dirty → rebuilt in next EndBlocker

**Why this works**:
- **No voting gap**: In-flight proposals use old tree snapshots (old key still works for those). New proposals after the rebuild use the new key.
- **Compromise handling**: If the ZK key is compromised but the Cosmos address is not, the voter rotates immediately. The attacker can only vote on in-flight proposals (using old snapshots).
- **No dual-leaf complexity**: Tree always has exactly one leaf per voter.

### 4. x/gov-Style Message Execution

**Decision**: Proposals contain `repeated google.protobuf.Any messages`, executed by x/vote's module account on PASS. This follows the same pattern as `x/gov` and `x/group`.

**How it works**:
- Proposals embed typed SDK messages (e.g., `MsgUpdateParams`, `MsgResolveChallenge`)
- On PASS: x/vote iterates messages and dispatches them via the message router, with x/vote's module account as the authority/signer
- On REJECT/QUORUM_NOT_MET/VETOED: no messages executed
- GENERAL (advisory) proposals: no messages, just a recorded result
- If message execution fails, the vote result still stands (PASSED) but an `EventProposalExecutionFailed` is emitted

**PRIVATE mode**: Messages are always stored **plaintext**, even for PRIVATE proposals. Non-voters can see *what will happen* if the vote passes (the executable effect) but not *why* (the encrypted title/description). The chain must be able to execute messages, so they cannot be encrypted.

**Typed proposal handling**: For proposals created programmatically by other modules (e.g., `CHALLENGE_JURY` created by x/rep), the originating module embeds the appropriate messages at creation time. For jury verdicts where the "on reject" action differs from "on pass", the originating module polls x/vote for the finalized result in its own EndBlocker:

```go
// In x/rep's EndBlocker: check for finalized jury votes
func (k Keeper) CheckJuryVerdicts(ctx context.Context) error {
    pendingJuries := k.GetPendingJuryVotes(ctx)
    for _, jury := range pendingJuries {
        proposal, err := k.voteKeeper.GetProposal(ctx, jury.ProposalID)
        if err != nil || proposal.Status != PROPOSAL_STATUS_FINALIZED {
            continue
        }
        switch proposal.Outcome {
        case PROPOSAL_OUTCOME_PASSED:
            // "Uphold" won — embedded messages already executed (slash)
            k.MarkJuryResolved(ctx, jury.ChallengeID)
        case PROPOSAL_OUTCOME_REJECTED:
            // "Reject" won — penalize challenger for frivolous challenge
            k.PenalizeChallenger(ctx, jury.ChallengeID)
            k.MarkJuryResolved(ctx, jury.ChallengeID)
        case PROPOSAL_OUTCOME_QUORUM_NOT_MET:
            // Extend voting period or escalate
            k.HandleJuryQuorumFailure(ctx, jury.ChallengeID)
        }
    }
    return nil
}
```

## Cross-Module Integration

### Required Module Changes

For x/vote to work with existing modules, the following changes are needed:

### x/rep → x/vote

x/rep is the primary consumer of x/vote. Changes needed:

```go
// 1. Call OnMemberRevoked when a member is removed/suspended/zeroed
//    (in x/rep's member management handlers)
func (k Keeper) RemoveMember(ctx context.Context, member sdk.AccAddress) error {
    // ... existing removal logic ...
    // Notify x/vote to deactivate voter registration
    return k.voteKeeper.OnMemberRevoked(ctx, member, "member_revoked")
}

// 2. Create jury votes using x/vote (replaces any ad-hoc jury system)
func (k Keeper) CreateChallengeJury(ctx context.Context, challengeID uint64, jurors []sdk.AccAddress) error {
    proposalID, err := k.voteKeeper.CreateChallengeVote(ctx, challengeID, jurors)
    if err != nil {
        return err
    }
    // Store mapping: challengeID → proposalID for EndBlocker polling
    return k.SetPendingJuryVote(ctx, challengeID, proposalID)
}

// 3. Poll for jury verdicts in EndBlocker (see Design Decision #4 above)
```

**Keeper dependency**: x/rep needs `voteKeeper` in its keeper struct, wired in `app.go`.

### x/commons → x/vote

x/commons may use x/vote for council elections and confidence votes:

```go
// Council elections: create a vote with candidates as options
func (k Keeper) CreateCouncilElection(ctx context.Context, councilID uint64, candidates []string) (uint64, error) {
    options := make([]VoteOption, 0, len(candidates)+1)
    for i, c := range candidates {
        options = append(options, VoteOption{Id: uint32(i), Label: c, Role: OPTION_ROLE_STANDARD})
    }
    // Add abstain option after all candidates
    options = append(options, VoteOption{Id: uint32(len(candidates)), Label: "abstain", Role: OPTION_ROLE_ABSTAIN})

    return k.voteKeeper.CreateProposal(ctx, MsgCreateProposal{
        Proposer:     k.GetModuleAddress().String(),
        Title:        fmt.Sprintf("Council %d Election", councilID),
        ProposalType: PROPOSAL_TYPE_COUNCIL_ELECTION,
        ReferenceID:  councilID,
        Options:      options,
        Visibility:   VISIBILITY_SEALED,
        // Messages: embed MsgUpdateCouncilMembers for the winning candidate
        // (resolved after tally — see note below)
    })
}
```

**Note**: For elections where the winning option determines the message (e.g., which candidate to appoint), the messages cannot be embedded at proposal creation time since the winner isn't known yet. x/commons handles this the same way as x/rep's jury polling: check the finalized tally in its EndBlocker and execute the appropriate action.

**Keeper dependency**: x/commons needs `voteKeeper` in its keeper struct.

### x/season → x/vote

x/season provides epoch information that x/vote uses for proposal nullifier rate limiting:

```go
// x/vote calls this to get the current epoch for nullifier verification
type SeasonKeeper interface {
    GetCurrentEpoch(ctx context.Context) uint64
}
```

**Keeper dependency**: x/vote needs `seasonKeeper` in its keeper struct. If x/season is not yet implemented, x/vote can derive epochs from block height as a fallback: `epoch = blockHeight / params.blocks_per_epoch` (default 17280 blocks ≈ 1 day at 5s blocks).

### x/reveal → x/vote

x/reveal may use x/vote for verification voting on code tranches:

```go
// Create verification vote for a revealed tranche
func (k Keeper) CreateVerificationVote(ctx context.Context, trancheID uint64) (uint64, error) {
    return k.voteKeeper.CreateProposal(ctx, MsgCreateProposal{
        Proposer:     k.GetModuleAddress().String(),
        Title:        fmt.Sprintf("Tranche %d Verification", trancheID),
        ProposalType: PROPOSAL_TYPE_GENERAL,
        ReferenceID:  trancheID,
        Options:      []VoteOption{
            {Id: 0, Label: "accept", Role: OPTION_ROLE_STANDARD},
            {Id: 1, Label: "improve", Role: OPTION_ROLE_STANDARD},
            {Id: 2, Label: "reject", Role: OPTION_ROLE_STANDARD},
        },
        Visibility:   VISIBILITY_PUBLIC,
    })
}
```

**Keeper dependency**: x/reveal needs `voteKeeper` in its keeper struct.

### x/forum → x/vote

x/forum may use x/vote for sentinel appeal votes (jury system for moderation appeals):

```go
// Create appeal vote for a moderation action
func (k Keeper) CreateAppealVote(ctx context.Context, appealID uint64, jurors []sdk.AccAddress) (uint64, error) {
    return k.voteKeeper.CreateChallengeVote(ctx, appealID, jurors)
}
```

**Keeper dependency**: x/forum needs `voteKeeper` in its keeper struct.

### App-Level Wiring (app.go)

```go
// In app.go, wire x/vote's module account as authorized for relevant messages
// The module account address is derived from the module name: "vote"

// x/vote keeper needs:
app.VoteKeeper = votekeeper.NewKeeper(
    appCodec,
    keys[votetypes.StoreKey],
    app.RepKeeper,       // IsMember checks
    app.SeasonKeeper,    // Epoch queries (or nil for block-height fallback)
    app.MsgServiceRouter(), // For executing proposal messages
    authtypes.NewModuleAddress(votetypes.ModuleName).String(), // Module authority
)

// Other modules that use x/vote:
app.RepKeeper.SetVoteKeeper(app.VoteKeeper)
app.CommonsKeeper.SetVoteKeeper(app.VoteKeeper)
// ... etc
```

### Message Authorization

The x/vote module account must be authorized to execute messages embedded in proposals. Each target module should accept the x/vote module account as a valid authority for the messages it handles:

```go
// In x/rep's MsgServer:
func (ms msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
    // Accept x/gov authority OR x/vote module account
    if msg.Authority != ms.keeper.GetGovAuthority() && msg.Authority != ms.keeper.GetVoteAuthority() {
        return nil, sdkerrors.ErrUnauthorized
    }
    // ... update params
}
```

This pattern is already standard in Cosmos SDK — modules that accept governance messages check the authority against the gov module account. Adding x/vote as an additional authorized caller is a one-line change per message handler.
