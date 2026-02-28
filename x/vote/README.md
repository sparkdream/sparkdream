# `x/vote`

The `x/vote` module implements anonymous voting with zero-knowledge proofs (ZK-SNARKs) and threshold timelock encryption (TLE) for the Spark Dream governance system. It enables privacy-preserving proposals and votes while preventing double-voting via nullifiers.

## Overview

This module provides:

- **Anonymous proposals** — proposal creators prove eligibility without revealing identity
- **Anonymous votes** — voters cast votes without revealing identity via Groth16/BN254 ZK proofs
- **Sealed/private voting** — commit-reveal scheme with optional TLE-assisted auto-reveal
- **Equal voting weight** — one member = one vote (membership is the gate, not token balance)
- **Nullifier-based double-vote prevention** — deterministic nullifiers prevent duplicate actions
- **Cross-module anonymous actions** — generic ZK circuit for anonymous posting, reacting, and challenging in other modules
- **Voter key rotation** — atomic ZK key rotation without deactivation

## Concepts

### ZK Circuits

Three distinct circuits:

| Circuit | Purpose | Public Inputs |
|---------|---------|---------------|
| **Vote** | Anonymous voting | MerkleRoot, Nullifier, ProposalID, VoteOption, VotingPower |
| **Proposal** | Anonymous proposal creation | MerkleRoot, Nullifier, VotingPower |
| **AnonAction** | Cross-module anonymous actions | MerkleRoot, Nullifier, MinTrustLevel, Scope |

All use **Groth16 on BN254** with **MiMC hashing** (SNARK-friendly). In development mode (no verifying key configured), proof verification is skipped.

### Voter Registration

Voters register with ZK keys and optional encryption keys:

- `zk_public_key` = hash(secretKey) — stored in Merkle tree for eligibility proofs
- `encryption_public_key` — Babyjubjub point for PRIVATE mode proposals
- Requires active `x/rep` membership and minimum DREAM stake (`min_registration_stake`)
- Keys can be rotated atomically via `MsgRotateVoterKey`

### Nullifier System

```
vote_nullifier     = hash("vote", secretKey, proposalID)
proposal_nullifier = hash("propose", secretKey, epoch, nonce)
```

Same voter + same proposal = same nullifier (prevents double voting). Same voter + different proposal = different nullifier (unlinkable across proposals). Domain separation prevents correlation between vote and proposal nullifiers.

### Merkle Tree (Voter Eligibility)

- **Depth**: 20 (supports ~1 million voters)
- **Hash**: MiMC (SNARK-friendly)
- **Lazy rebuild**: only rebuilt when membership changes (dirty flag)
- **Snapshot isolation**: each proposal snapshots the tree at creation time, preventing eligibility manipulation during voting

### Voting Modes

| Mode | Commit | Reveal | Privacy Level |
|------|--------|--------|---------------|
| **PUBLIC** | ZK proof + vote option | N/A | Vote content hidden, but tally visible during voting |
| **SEALED** | ZK proof + commitment hash | Manual or TLE-assisted | Vote hidden until reveal phase |
| **PRIVATE** | ZK proof + encrypted commitment | TLE-assisted | Full content encryption until threshold decryption |

### Threshold Timelock Encryption (TLE)

- Validators register DKG public key shares via `MsgRegisterTLEShare`
- Clients encrypt vote reveals to a specific epoch
- After voting ends, validators submit decryption shares with correctness proofs
- When threshold (e.g., 2/3) shares are received, the decryption key is reconstructed on-chain
- EndBlocker applies decrypted reveals automatically
- Validator liveness tracked with configurable miss window and jail tolerance

### Proposal Outcomes

| Outcome | Condition |
|---------|-----------|
| `PASSED` | Quorum met AND threshold met AND veto below threshold |
| `REJECTED` | Quorum met AND threshold not met |
| `QUORUM_NOT_MET` | Insufficient participation |
| `VETOED` | Veto votes exceed veto threshold |

Passed proposals can include executable `google.protobuf.Any` messages, executed via the module account (governance automation pattern).

## State

### Objects

| Object | Key | Description |
|--------|-----|-------------|
| `VotingProposal` | `proposal/{id}` | Proposal with options, tally, status, visibility |
| `VoterRegistration` | `voter/{address}` | ZK keys, encryption key, active status |
| `AnonymousVote` | `vote/{proposalId}/{nullifierHex}` | PUBLIC mode votes |
| `SealedVote` | `sealed_vote/{proposalId}/{nullifierHex}` | SEALED/PRIVATE mode votes |
| `VoterTreeSnapshot` | `tree_snapshot/{proposal_id}` | Merkle root snapshot per proposal |
| `UsedNullifier` | `nullifier/{proposalId}/{nullifierHex}` | Double-vote prevention |
| `UsedProposalNullifier` | `proposal_nullifier/{epoch}/{nullifierHex}` | Proposal rate limiting |
| `TleValidatorShare` | `tle_share/{validator}` | Validator DKG public key share |
| `TleDecryptionShare` | `tle_decrypt/{epoch}/{validator}` | Submitted decryption shares |
| `EpochDecryptionKey` | `epoch_key/{epoch}` | Reconstructed decryption key |
| `TleValidatorLiveness` | `tle_liveness/{validator}` | Liveness tracking across miss window |
| `SrsState` | singleton | PLONK universal SRS and hash |

## Messages

### Voter Management

| Message | Description | Access |
|---------|-------------|--------|
| `MsgRegisterVoter` | Register voter with ZK and encryption keys | Active `x/rep` member |
| `MsgDeactivateVoter` | Voluntarily deactivate registration | Voter only |
| `MsgRotateVoterKey` | Atomically rotate ZK keys | Voter only |

### Proposals

| Message | Description | Access |
|---------|-------------|--------|
| `MsgCreateProposal` | Create proposal with public identity | Any registered voter |
| `MsgCreateAnonymousProposal` | Create proposal with ZK proof | Any registered voter (anonymous) |
| `MsgCancelProposal` | Cancel ACTIVE or TALLYING proposal | Proposer or authority |

### Voting

| Message | Description | Access |
|---------|-------------|--------|
| `MsgVote` | Submit anonymous vote with ZK proof (PUBLIC mode) | Registered voter |
| `MsgSealedVote` | Submit sealed vote with commitment (SEALED/PRIVATE) | Registered voter |
| `MsgRevealVote` | Reveal sealed vote after voting period | Sealed vote holder |

### TLE Management

| Message | Description | Access |
|---------|-------------|--------|
| `MsgRegisterTLEShare` | Register validator's DKG public key share | Active validator |
| `MsgSubmitDecryptionShare` | Submit decryption key share with correctness proof | Active validator |

### System

| Message | Description | Access |
|---------|-------------|--------|
| `MsgStoreSRS` | Store PLONK universal SRS on-chain | `x/gov` authority |
| `MsgUpdateParams` | Update module parameters | `x/gov` authority |

## Queries

| Query | Description |
|-------|-------------|
| `Params` | Module parameters |
| `Proposal` | Single proposal by ID |
| `Proposals` | All proposals (paginated) |
| `ProposalsByStatus` | Filter by ACTIVE, TALLYING, FINALIZED, CANCELLED |
| `ProposalsByType` | Filter by GENERAL, PARAMETER_CHANGE, COUNCIL_ELECTION, etc. |
| `ProposalTally` | Vote counts and eligible voter count |
| `ProposalVotes` | All votes for a proposal |
| `VoterRegistrationQuery` | Voter's ZK keys and status |
| `VoterRegistrations` | All registrations (paginated) |
| `VoterTreeSnapshotQuery` | Merkle snapshot for proposal |
| `VoterMerkleProof` | Merkle path for voter's public key |
| `NullifierUsed` | Check if vote nullifier used |
| `ProposalNullifierUsed` | Check if proposal nullifier used |
| `TleStatus` | TLE enabled/disabled, current epoch, master public key |
| `TleValidatorShares` | All validator DKG shares |
| `EpochDecryptionKeyQuery` | Decryption key and share status for epoch |
| `TleLiveness` | Liveness summary for all validators |
| `GetSrsState` | Stored SRS and hash |

## Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `vote_verifying_key` | bytes | (empty) | Verifying key for vote circuit |
| `proposal_verifying_key` | bytes | (empty) | Verifying key for proposal circuit |
| `anon_action_verifying_key` | bytes | (empty) | Verifying key for cross-module anonymous actions |
| `tree_depth` | uint32 | 20 | Merkle tree depth (~1M voters) |
| `min_voting_period_epochs` | int64 | | Min voting duration |
| `max_voting_period_epochs` | int64 | | Max voting duration |
| `default_voting_period_epochs` | int64 | | Default if not specified |
| `sealed_reveal_period_epochs` | int64 | | Additional time for sealed vote reveals |
| `default_quorum` | Dec | 0.333 | Minimum participation |
| `default_threshold` | Dec | 0.5 | Support required among votes |
| `default_veto_threshold` | Dec | | Veto trigger threshold |
| `open_registration` | bool | true | Allow new voter registrations |
| `min_registration_stake` | Int | | DREAM balance requirement |
| `max_proposals_per_epoch` | uint64 | 3 | Max proposals per member per epoch |
| `allow_private_proposals` | bool | | Enable PRIVATE visibility |
| `allow_sealed_proposals` | bool | | Enable SEALED visibility |
| `min_proposal_deposit` | Coins | | Deposit for public proposals |
| `tle_enabled` | bool | | Toggle threshold timelock encryption |
| `tle_threshold_numerator/denominator` | uint32 | 2/3 | Threshold for key reconstruction |
| `tle_master_public_key` | bytes | | Master public key for TLE |
| `tle_miss_window` | uint32 | 100 | Sliding window for tracking misses |
| `tle_miss_tolerance` | uint32 | 10 | Misses before flagging |
| `tle_jail_enabled` | bool | | Jail validators exceeding tolerance |

## Dependencies

| Module | Required | Purpose |
|--------|----------|---------|
| `x/auth` | Yes | Account codec, account lookups |
| `x/bank` | Yes | Check voter DREAM balance for registration |
| `x/rep` | Yes | Membership verification |
| `x/season` | No | Current epoch for nullifier scoping |
| `x/staking` | No | Validator verification and jailing for TLE liveness |

## EndBlocker

1. **Tree rebuild** — if dirty flag set, rebuild MiMC Merkle trees for main and encryption variants
2. **Sealed vote auto-reveal** — decrypt sealed votes using reconstructed TLE key
3. **Proposal finalization** — tally votes, determine outcome, execute messages if PASSED
4. **TLE validator liveness** — update participation tracking, flag/jail validators exceeding miss tolerance

## Client

### CLI

```bash
# Voter registration
sparkdreamd tx vote register-voter --from alice
sparkdreamd tx vote rotate-voter-key --from alice

# Proposals
sparkdreamd tx vote create-proposal "Title" "Description" GENERAL 0 5 0.33 0.5 0.33 PUBLIC --from alice
sparkdreamd tx vote create-anonymous-proposal "Title" "Description" GENERAL 0 5 0.33 0.5 0.33 PUBLIC 0 1 --from alice

# Voting
sparkdreamd tx vote vote 1 0 --from alice
sparkdreamd tx vote sealed-vote 1 --from alice
sparkdreamd tx vote reveal-vote 1 0 --from alice

# Queries
sparkdreamd q vote proposal 1
sparkdreamd q vote proposals
sparkdreamd q vote proposal-tally 1
sparkdreamd q vote voter-registration-query [address]
sparkdreamd q vote tle-status
sparkdreamd q vote params
```

### gRPC/REST

All queries are available via gRPC and REST (grpc-gateway). See `proto/sparkdream/vote/v1/query.proto` for the full API surface.
