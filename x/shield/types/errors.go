package types

import (
	errorsmod "cosmossdk.io/errors"
)

// x/shield module sentinel errors
var (
	// General
	ErrInvalidSigner             = errorsmod.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")
	ErrShieldDisabled            = errorsmod.Register(ModuleName, 2, "shielded execution is disabled")
	ErrShieldGasDepleted         = errorsmod.Register(ModuleName, 3, "shield module gas reserve depleted")
	ErrUnregisteredOperation     = errorsmod.Register(ModuleName, 4, "inner message type not registered for shielded execution")
	ErrOperationInactive         = errorsmod.Register(ModuleName, 5, "shielded operation is currently inactive")
	ErrProofDomainMismatch       = errorsmod.Register(ModuleName, 6, "proof domain does not match registered requirement")
	ErrInsufficientTrustLevel    = errorsmod.Register(ModuleName, 7, "proven trust level below required minimum")
	ErrInvalidProof              = errorsmod.Register(ModuleName, 8, "ZK proof verification failed")
	ErrInvalidProofDomain        = errorsmod.Register(ModuleName, 9, "unknown proof domain")
	ErrNullifierUsed             = errorsmod.Register(ModuleName, 10, "nullifier already used in this domain and scope")
	ErrRateLimitExceeded         = errorsmod.Register(ModuleName, 11, "per-identity rate limit exceeded for this epoch")
	ErrInvalidInnerMessage       = errorsmod.Register(ModuleName, 12, "inner message is invalid or cannot be decoded")
	ErrInvalidInnerMessageSigner = errorsmod.Register(ModuleName, 13, "inner message signer must be shield module account")
	ErrMultiMsgNotAllowed        = errorsmod.Register(ModuleName, 14, "MsgShieldedExec must be the only message in the transaction")

	// Execution mode
	ErrInvalidExecMode          = errorsmod.Register(ModuleName, 15, "invalid execution mode")
	ErrImmediateNotAllowed      = errorsmod.Register(ModuleName, 16, "this operation requires encrypted batch mode")
	ErrEncryptedBatchDisabled   = errorsmod.Register(ModuleName, 17, "encrypted batch mode is disabled")
	ErrEncryptedBatchNotAllowed = errorsmod.Register(ModuleName, 18, "this operation does not support encrypted batch mode")

	// Encrypted batch
	ErrMissingEncryptedPayload = errorsmod.Register(ModuleName, 19, "encrypted_payload is required for encrypted batch mode")
	ErrPayloadTooLarge         = errorsmod.Register(ModuleName, 20, "encrypted_payload exceeds max_encrypted_payload_size")
	ErrInvalidTargetEpoch      = errorsmod.Register(ModuleName, 21, "target_epoch must be the current shield epoch")
	ErrPendingQueueFull        = errorsmod.Register(ModuleName, 22, "pending operation queue is at capacity")
	ErrDecryptionFailed        = errorsmod.Register(ModuleName, 23, "failed to decrypt encrypted payload")
	ErrInvalidMerkleRoot       = errorsmod.Register(ModuleName, 24, "merkle root is not current or previous")

	// Validator TLE
	ErrInvalidDecryptionShare    = errorsmod.Register(ModuleName, 25, "decryption share verification failed")
	ErrDuplicateShare            = errorsmod.Register(ModuleName, 26, "decryption share already submitted for this epoch")
	ErrNotTLEValidator           = errorsmod.Register(ModuleName, 27, "validator has no registered TLE public key share")
	ErrEpochTooOld               = errorsmod.Register(ModuleName, 28, "epoch is too old for late share submission")
	ErrDKGNotComplete            = errorsmod.Register(ModuleName, 29, "DKG ceremony not yet complete")
	ErrInvalidProofOfPossession  = errorsmod.Register(ModuleName, 30, "proof of possession for TLE key share is invalid")
	ErrRawMasterShareSubmitted   = errorsmod.Register(ModuleName, 31, "raw master share submitted instead of epoch-derived share")
	ErrIncompatibleOperation     = errorsmod.Register(ModuleName, 32, "target module does not implement ShieldAware or rejects this message type")
	ErrDKGInProgress             = errorsmod.Register(ModuleName, 33, "DKG ceremony already in progress")
	ErrCleartextFieldInBatchMode = errorsmod.Register(ModuleName, 34, "inner_message and proof must be empty in encrypted batch mode")

	// DKG ceremony
	ErrDKGNotOpen             = errorsmod.Register(ModuleName, 35, "DKG ceremony not in accepting phase")
	ErrNotBondedValidator     = errorsmod.Register(ModuleName, 36, "only bonded validators can participate in DKG")
	ErrDuplicateContribution  = errorsmod.Register(ModuleName, 37, "validator already contributed to this DKG round")
	ErrDKGRoundMismatch       = errorsmod.Register(ModuleName, 38, "DKG round does not match current round")
	ErrInvalidCommitments     = errorsmod.Register(ModuleName, 39, "feldman commitments are invalid")
	ErrInvalidEvaluations     = errorsmod.Register(ModuleName, 40, "encrypted evaluations are invalid or incomplete")
	ErrInsufficientValidators = errorsmod.Register(ModuleName, 41, "not enough bonded validators for DKG")
	ErrValidatorNotInDKG      = errorsmod.Register(ModuleName, 42, "validator is not in the expected DKG participant set")
	ErrNoVerificationKey      = errorsmod.Register(ModuleName, 43, "no verification key registered; shielded execution requires a VK to be stored before use")

	// Input validation
	ErrInvalidNullifierLength = errorsmod.Register(ModuleName, 44, "nullifier must be exactly 32 bytes")
	ErrInvalidMerkleRootLen   = errorsmod.Register(ModuleName, 45, "merkle root must be exactly 32 bytes")
	ErrProofTooLarge          = errorsmod.Register(ModuleName, 46, "proof exceeds maximum allowed size")
)
