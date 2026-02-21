package keeper

import (
	"context"

	zkcrypto "sparkdream/zkprivatevoting/crypto"

	"sparkdream/x/vote/types"
)

// DecryptTLEPayloadForTest exposes decryptTLEPayload for external tests.
func DecryptTLEPayloadForTest(encryptedReveal, decryptionKey []byte) (nullifier []byte, voteOption uint32, salt []byte, err error) {
	return decryptTLEPayload(encryptedReveal, decryptionKey)
}

// VerifyCorrectnessProofForTest exposes verifyCorrectnessProof for external tests.
func VerifyCorrectnessProofForTest(shareBytes, correctnessProof, registeredPublicKeyShare []byte) error {
	return verifyCorrectnessProof(context.Background(), shareBytes, correctnessProof, registeredPublicKeyShare)
}

// ComputeCommitmentHashForTest exposes computeCommitmentHash for external tests.
func ComputeCommitmentHashForTest(voteOption uint32, salt []byte) []byte {
	return computeCommitmentHash(voteOption, salt)
}

// ValidateProposalOptionsForTest exposes validateProposalOptions for external tests.
func ValidateProposalOptionsForTest(k Keeper, params types.Params, opts []*types.VoteOption) error {
	return k.validateProposalOptions(params, opts)
}

// InitTallyForTest exposes initTally for external tests.
func InitTallyForTest(opts []*types.VoteOption) []*types.VoteTally {
	return initTally(opts)
}

// NullifierKeyForTest exposes nullifierKey for external tests.
func NullifierKeyForTest(proposalID uint64, nullifier []byte) string {
	return nullifierKey(proposalID, nullifier)
}

// ProposalNullifierKeyForTest exposes proposalNullifierKey for external tests.
func ProposalNullifierKeyForTest(epoch uint64, nullifier []byte) string {
	return proposalNullifierKey(epoch, nullifier)
}

// TleShareKeyForTest exposes tleShareKey for external tests.
func TleShareKeyForTest(validator string, epoch uint64) string {
	return tleShareKey(validator, epoch)
}

// TrackTleLivenessForTest exposes trackTleLiveness for external tests.
func (k Keeper) TrackTleLivenessForTest(ctx context.Context) error {
	return k.trackTleLiveness(ctx)
}

// RecordEpochParticipationForTest exposes recordEpochParticipation for external tests.
func (k Keeper) RecordEpochParticipationForTest(ctx context.Context, epoch uint64, params types.Params) error {
	return k.recordEpochParticipation(ctx, epoch, params)
}

// UpdateValidatorLivenessFlagsForTest exposes updateValidatorLivenessFlags for external tests.
func (k Keeper) UpdateValidatorLivenessFlagsForTest(ctx context.Context, allValidators []string, params types.Params) error {
	return k.updateValidatorLivenessFlags(ctx, allValidators, params)
}

// PruneTleParticipationForTest exposes pruneTleParticipation for external tests.
func (k Keeper) PruneTleParticipationForTest(ctx context.Context, currentEpoch uint64, params types.Params) error {
	return k.pruneTleParticipation(ctx, currentEpoch, params)
}

// JailTleValidatorForTest exposes jailTleValidator for external tests.
func (k Keeper) JailTleValidatorForTest(ctx context.Context, validatorAddr string, missedCount uint32) error {
	return k.jailTleValidator(ctx, validatorAddr, missedCount)
}

// SetBuildMerkleTreeFunc overrides buildMerkleTree for testing.
// Returns a restore function to reset the original implementation.
func SetBuildMerkleTreeFunc(fn func([][]byte) ([]byte, uint64)) func() {
	old := buildMerkleTree
	buildMerkleTree = fn
	return func() { buildMerkleTree = old }
}

// SetBuildMerkleTreeFullFunc overrides buildMerkleTreeFull for testing.
// Returns a restore function to reset the original implementation.
func SetBuildMerkleTreeFullFunc(fn func([][]byte) *zkcrypto.MerkleTree) func() {
	old := buildMerkleTreeFull
	buildMerkleTreeFull = fn
	return func() { buildMerkleTreeFull = old }
}
