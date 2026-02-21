package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

// x/vote module sentinel errors
var (
	ErrInvalidSigner = errors.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")

	// Registration errors
	ErrNotAMember        = errors.Register(ModuleName, 2, "sender is not an active x/rep member")
	ErrAlreadyRegistered = errors.Register(ModuleName, 3, "voter already has an active registration")
	ErrNotRegistered     = errors.Register(ModuleName, 4, "voter is not registered")
	ErrAlreadyInactive   = errors.Register(ModuleName, 5, "voter registration is already inactive")
	ErrDuplicatePublicKey = errors.Register(ModuleName, 6, "zk public key is already registered to another address")
	ErrUseRotateKey      = errors.Register(ModuleName, 7, "active registration exists — use MsgRotateVoterKey to change keys")
	ErrRegistrationClosed = errors.Register(ModuleName, 8, "voter registration is closed (open_registration = false)")
	ErrInsufficientStake = errors.Register(ModuleName, 9, "voter does not meet min_registration_stake requirement")

	// Proposal errors
	ErrProposalNotFound       = errors.Register(ModuleName, 10, "proposal not found")
	ErrProposalNotActive      = errors.Register(ModuleName, 11, "proposal is not in ACTIVE status")
	ErrProposalNotTallying    = errors.Register(ModuleName, 12, "proposal is not in TALLYING status")
	ErrInvalidVisibility      = errors.Register(ModuleName, 13, "invalid visibility level for this message type")
	ErrPrivateNotAllowed      = errors.Register(ModuleName, 14, "private proposals are disabled")
	ErrSealedNotAllowed       = errors.Register(ModuleName, 15, "sealed proposals are disabled")
	ErrNoEligibleVoters       = errors.Register(ModuleName, 16, "voter tree is empty — no eligible voters")
	ErrTooManyEligibleVoters  = errors.Register(ModuleName, 17, "eligible voters exceed max_private_eligible_voters")
	ErrInvalidVoteOptions     = errors.Register(ModuleName, 18, "vote option IDs must be sequential starting from 0")
	ErrVoteOptionsOutOfRange  = errors.Register(ModuleName, 19, "vote options count outside [min, max] range")
	ErrCancelNotAuthorized    = errors.Register(ModuleName, 20, "not authorized to cancel this proposal")
	ErrVotingPeriodOutOfRange = errors.Register(ModuleName, 21, "voting period outside [min, max] range")
	ErrNoStandardOption       = errors.Register(ModuleName, 22, "at least one vote option must have OPTION_ROLE_STANDARD")
	ErrDuplicateAbstainRole   = errors.Register(ModuleName, 23, "at most one vote option may have OPTION_ROLE_ABSTAIN")
	ErrDuplicateVetoRole      = errors.Register(ModuleName, 24, "at most one vote option may have OPTION_ROLE_VETO")
	ErrInsufficientDeposit    = errors.Register(ModuleName, 25, "deposit is less than min_proposal_deposit")
	ErrInvalidThreshold       = errors.Register(ModuleName, 26, "quorum or threshold must be > 0 and <= 1")
	ErrProposalNotCancellable = errors.Register(ModuleName, 27, "proposal is not in ACTIVE or TALLYING status — cannot cancel")

	// Vote errors
	ErrNullifierUsed        = errors.Register(ModuleName, 30, "nullifier has already been used")
	ErrInvalidProof         = errors.Register(ModuleName, 31, "ZK proof verification failed")
	ErrMerkleRootMismatch   = errors.Register(ModuleName, 32, "Merkle root does not match proposal snapshot")
	ErrVoteOptionOutOfRange = errors.Register(ModuleName, 33, "vote option exceeds proposal option count")
	ErrInvalidCommitment    = errors.Register(ModuleName, 34, "vote commitment is empty or malformed")
	ErrRevealMismatch       = errors.Register(ModuleName, 35, "hash(option, salt) does not match stored commitment")
	ErrVoteNotFound         = errors.Register(ModuleName, 36, "sealed vote with this nullifier not found")
	ErrAlreadyRevealed      = errors.Register(ModuleName, 37, "sealed vote has already been revealed")

	// Epoch/proposal nullifier errors
	ErrEpochMismatch         = errors.Register(ModuleName, 40, "claimed epoch is not within ±1 of current epoch")
	ErrMaxNonceMismatch      = errors.Register(ModuleName, 41, "MaxNonce does not match params.max_proposals_per_epoch - 1")
	ErrProposalLimitReached  = errors.Register(ModuleName, 42, "proposal creation nullifier already used this epoch")

	// TLE errors
	ErrTLENotEnabled             = errors.Register(ModuleName, 50, "threshold timelock encryption is not enabled")
	ErrNotValidator              = errors.Register(ModuleName, 51, "sender is not an active bonded validator")
	ErrNoTLEShare                = errors.Register(ModuleName, 52, "validator has no registered TLE public key share")
	ErrDuplicateDecryptionShare  = errors.Register(ModuleName, 53, "validator already submitted a decryption share for this epoch")
	ErrInvalidCorrectnessProof   = errors.Register(ModuleName, 54, "decryption share correctness proof verification failed")
	ErrEncryptedRevealTooLarge   = errors.Register(ModuleName, 55, "encrypted reveal exceeds max_encrypted_reveal_bytes")
	ErrInvalidPublicKeyShare     = errors.Register(ModuleName, 56, "public key share is not a valid BN256 G1 point")
	ErrInvalidShareIndex         = errors.Register(ModuleName, 57, "share index must be positive (1-based)")
	ErrDuplicateShareIndex       = errors.Register(ModuleName, 58, "share index is already registered by another validator")

	// SRS errors
	ErrSRSNotStored    = errors.Register(ModuleName, 60, "SRS not found in state — must be stored before key derivation")
	ErrSRSHashMismatch = errors.Register(ModuleName, 61, "stored SRS hash does not match params.srs_hash")
)
