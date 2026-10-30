package types

import (
	errorsmod "cosmossdk.io/errors"
)

// x/shield module sentinel errors. Codes are not reused; gaps are codes
// retired because nothing returned them.
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
	ErrInvalidExecMode        = errorsmod.Register(ModuleName, 15, "invalid execution mode")
	ErrImmediateNotAllowed    = errorsmod.Register(ModuleName, 16, "this operation requires encrypted batch mode")
	ErrEncryptedBatchDisabled = errorsmod.Register(ModuleName, 17, "encrypted batch mode is disabled")

	// Encrypted batch
	ErrMissingEncryptedPayload = errorsmod.Register(ModuleName, 19, "encrypted_payload is required for encrypted batch mode")
	ErrPayloadTooLarge         = errorsmod.Register(ModuleName, 20, "encrypted_payload exceeds max_encrypted_payload_size")
	ErrInvalidTargetEpoch      = errorsmod.Register(ModuleName, 21, "target_epoch must be the current shield epoch")
	ErrPendingQueueFull        = errorsmod.Register(ModuleName, 22, "pending operation queue is at capacity")
	ErrInvalidMerkleRoot       = errorsmod.Register(ModuleName, 24, "merkle root is not current or previous")

	// Validator TLE
	ErrInvalidDecryptionShare    = errorsmod.Register(ModuleName, 25, "decryption share verification failed")
	ErrIncompatibleOperation     = errorsmod.Register(ModuleName, 32, "target module does not implement ShieldAware or rejects this message type")
	ErrDKGInProgress             = errorsmod.Register(ModuleName, 33, "DKG ceremony already in progress")
	ErrCleartextFieldInBatchMode = errorsmod.Register(ModuleName, 34, "inner_message must be empty in encrypted batch mode")

	// DKG ceremony
	ErrInsufficientValidators = errorsmod.Register(ModuleName, 41, "not enough bonded validators for DKG")
	ErrNoVerificationKey      = errorsmod.Register(ModuleName, 43, "no verification key registered; shielded execution requires a VK to be stored before use")

	// Input validation
	ErrInvalidNullifierLength = errorsmod.Register(ModuleName, 44, "nullifier must be exactly 32 bytes")
	ErrFeeTooHigh             = errorsmod.Register(ModuleName, 47, "shielded exec fee exceeds max_fee_per_exec")

	// Ownership mode
	ErrNotOwnable         = errorsmod.Register(ModuleName, 48, "target module cannot resolve ownership for this operation")
	ErrOwnershipMismatch  = errorsmod.Register(ModuleName, 49, "proof nullifier does not match the content's owner tag")
	ErrOwnershipBatchMode = errorsmod.Register(ModuleName, 50, "ownership operations are immediate-only")

	// Mempool guard (node-local, CheckTx only)
	ErrProofVerificationThrottled = errorsmod.Register(ModuleName, 51, "node is at its proof verification rate limit; retry shortly")

	// Encrypted batch submission proof
	ErrInvalidSubmissionNullifier = errorsmod.Register(ModuleName, 52, "submission nullifier already pending for this epoch")

	// DKG trigger
	ErrInvalidDKGThreshold = errorsmod.Register(ModuleName, 53, "DKG threshold must satisfy 0 < numerator <= denominator <= 100")

	// Public inputs: every nullifier, rate-limit nullifier and merkle root is
	// a BN254 scalar-field element and must be given in its one canonical
	// encoding (value < r). The verifier reduces inputs mod r, so n and n+r
	// verify alike while being different store keys.
	ErrNonCanonicalPublicInput = errorsmod.Register(ModuleName, 54, "public input is not a canonical BN254 scalar field element")

	// Chain-wide per-epoch budget of fees the module pays
	ErrEpochFeeBudgetExhausted = errorsmod.Register(ModuleName, 55, "shield epoch fee budget (max_fees_per_epoch) exhausted; retry next shield epoch")

	// Nullifier domain reuse across registrations
	ErrNullifierDomainConflict = errorsmod.Register(ModuleName, 56, "nullifier domain is already used by an operation with a different scope type, scope field or window")

	// The shared anonymous submitter's txs must be unordered
	ErrOrderedPublicSubmitterTx = errorsmod.Register(ModuleName, 57, "txs signed by the public anonymous submitter must be unordered")
)
