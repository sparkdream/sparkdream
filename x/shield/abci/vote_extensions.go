package abci

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"go.dedis.ch/kyber/v4/encrypt/ecies"
	"go.dedis.ch/kyber/v4/pairing/bn256"

	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

var tleSuite = bn256.NewSuiteG1()

// DKGVoteExtensionHandler implements ExtendVote and VerifyVoteExtension
// for the DKG ceremony. Validators automatically participate in DKG rounds
// by embedding cryptographic material in their consensus votes.
type DKGVoteExtensionHandler struct {
	keeper   keeper.Keeper
	keyStore *keeper.DKGLocalKeyStore
	consAddr sdk.ConsAddress // This validator's CometBFT consensus address
}

// NewDKGVoteExtensionHandler creates a handler wired to the shield keeper.
// homeDir is the node's home directory (e.g., ~/.sparkdream).
func NewDKGVoteExtensionHandler(k keeper.Keeper, homeDir string) *DKGVoteExtensionHandler {
	h := &DKGVoteExtensionHandler{
		keeper:   k,
		keyStore: keeper.NewDKGLocalKeyStore(homeDir),
		consAddr: loadValidatorConsAddress(homeDir),
	}
	return h
}

// ExtendVoteHandler returns the sdk.ExtendVoteHandler for BaseApp.
// During REGISTERING: embeds BN256 public key + Schnorr PoP.
// During CONTRIBUTING: embeds Feldman commitments + ECIES-encrypted evaluations + PoP.
// Returns empty extension outside of active DKG or on any error (never blocks consensus).
func (h *DKGVoteExtensionHandler) ExtendVoteHandler() sdk.ExtendVoteHandler {
	return func(ctx sdk.Context, req *abci.RequestExtendVote) (*abci.ResponseExtendVote, error) {
		ext, err := h.buildExtension(ctx)
		if err != nil || ext == nil {
			return &abci.ResponseExtendVote{}, nil
		}

		bz, err := ext.Marshal()
		if err != nil {
			return &abci.ResponseExtendVote{}, nil
		}

		return &abci.ResponseExtendVote{VoteExtension: bz}, nil
	}
}

// VerifyVoteExtensionHandler returns the sdk.VerifyVoteExtensionHandler for BaseApp.
// Validates that the DKG data embedded in a vote extension is well-formed.
// ACCEPT for empty extensions (validator not participating or no DKG active).
// REJECT only for malformed data that indicates a Byzantine validator.
func (h *DKGVoteExtensionHandler) VerifyVoteExtensionHandler() sdk.VerifyVoteExtensionHandler {
	return func(ctx sdk.Context, req *abci.RequestVerifyVoteExtension) (*abci.ResponseVerifyVoteExtension, error) {
		// Empty extension is always valid (validator may not participate)
		if len(req.VoteExtension) == 0 {
			return &abci.ResponseVerifyVoteExtension{
				Status: abci.ResponseVerifyVoteExtension_ACCEPT,
			}, nil
		}

		var ext types.DKGVoteExtension
		if err := ext.Unmarshal(req.VoteExtension); err != nil {
			return &abci.ResponseVerifyVoteExtension{
				Status: abci.ResponseVerifyVoteExtension_REJECT,
			}, nil
		}

		if err := h.verifyExtension(ctx, &ext); err != nil {
			ctx.Logger().With("module", "x/shield").Debug(
				"rejected DKG vote extension",
				"validator", hex.EncodeToString(req.ValidatorAddress),
				"err", err,
			)
			return &abci.ResponseVerifyVoteExtension{
				Status: abci.ResponseVerifyVoteExtension_REJECT,
			}, nil
		}

		return &abci.ResponseVerifyVoteExtension{
			Status: abci.ResponseVerifyVoteExtension_ACCEPT,
		}, nil
	}
}

// buildExtension generates the DKG vote extension for this validator.
func (h *DKGVoteExtensionHandler) buildExtension(ctx sdk.Context) (*types.DKGVoteExtension, error) {
	if len(h.consAddr) == 0 {
		return nil, nil // Can't determine our identity (e.g., KMS signer)
	}

	dkgState, found := h.keeper.GetDKGStateVal(ctx)
	if !found {
		return nil, nil
	}

	// Get our operator address from consensus address
	opAddr, err := h.getOperatorAddr(ctx)
	if err != nil || opAddr == "" {
		return nil, nil // Not a bonded validator
	}

	// Check if we're in the DKG participant set
	if !isValInDKG(dkgState, opAddr) {
		return nil, nil
	}

	switch dkgState.Phase {
	case types.DKGPhase_DKG_PHASE_REGISTERING:
		return h.buildRegistrationExtension(dkgState, opAddr)
	case types.DKGPhase_DKG_PHASE_CONTRIBUTING:
		return h.buildContributionExtension(ctx, dkgState, opAddr)
	case types.DKGPhase_DKG_PHASE_ACTIVE:
		return h.buildDecryptionShareExtension(ctx, dkgState, opAddr)
	default:
		return nil, nil
	}
}

// buildRegistrationExtension creates a REGISTERING phase extension:
// BN256 G1 public key + Schnorr proof of possession.
func (h *DKGVoteExtensionHandler) buildRegistrationExtension(dkgState types.DKGState, opAddr string) (*types.DKGVoteExtension, error) {
	_, pubKey, err := h.keyStore.EnsureRegistrationKey(dkgState.Round)
	if err != nil {
		return nil, fmt.Errorf("key generation failed: %w", err)
	}

	pubBytes, err := pubKey.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("pub key marshal failed: %w", err)
	}

	pop, err := h.keyStore.SignPoP(dkgState.Round, opAddr)
	if err != nil {
		return nil, fmt.Errorf("PoP signing failed: %w", err)
	}

	return &types.DKGVoteExtension{
		Round:              dkgState.Round,
		Phase:              types.DKGPhase_DKG_PHASE_REGISTERING,
		RegistrationPubKey: pubBytes,
		RegistrationPop:    pop,
	}, nil
}

// buildContributionExtension creates a CONTRIBUTING phase extension:
// Feldman commitments + ECIES-encrypted polynomial evaluations + PoP.
func (h *DKGVoteExtensionHandler) buildContributionExtension(ctx sdk.Context, dkgState types.DKGState, opAddr string) (*types.DKGVoteExtension, error) {
	threshold := computeThreshold(dkgState)
	if threshold == 0 {
		return nil, fmt.Errorf("threshold is 0")
	}

	// Generate or reload our polynomial
	poly, commitments, err := h.keyStore.GeneratePolynomial(dkgState.Round, int(threshold))
	if err != nil {
		return nil, fmt.Errorf("polynomial generation failed: %w", err)
	}
	_ = poly // used below for evaluations

	// Marshal Feldman commitments
	feldmanCommitments := make([][]byte, len(commitments))
	for i, c := range commitments {
		bz, err := c.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("commitment marshal failed: %w", err)
		}
		feldmanCommitments[i] = bz
	}

	// Build encrypted evaluations for each OTHER validator
	selfIdx := valDKGIndex(dkgState, opAddr)
	var encryptedEvals []*types.EncryptedEvaluation

	for i, targetAddr := range dkgState.ExpectedValidators {
		targetIdx := uint32(i + 1)
		if targetIdx == selfIdx {
			continue // Don't encrypt for ourselves
		}

		// Look up the target's registered pub key
		reg, found := h.keeper.GetDKGRegistration(ctx, targetAddr)
		if !found {
			// Target hasn't registered yet — skip (they won't get our share)
			continue
		}
		targetPubKeyBytes := keeper.GetDKGRegistrationPubKey(reg)
		if len(targetPubKeyBytes) == 0 {
			continue
		}

		// Parse the target's BN256 G1 public key
		targetPubKey := tleSuite.Point()
		if err := targetPubKey.UnmarshalBinary(targetPubKeyBytes); err != nil {
			continue // Skip malformed registrations
		}

		// Evaluate our polynomial at the target's index
		evalScalar, err := h.keyStore.EvaluatePolynomial(dkgState.Round, targetIdx)
		if err != nil {
			continue
		}
		evalBytes, err := evalScalar.MarshalBinary()
		if err != nil {
			continue
		}

		// ECIES-encrypt the evaluation with the target's public key
		ciphertext, err := ecies.Encrypt(tleSuite, targetPubKey, evalBytes, nil)
		if err != nil {
			continue
		}

		encryptedEvals = append(encryptedEvals, &types.EncryptedEvaluation{
			TargetIndex: targetIdx,
			Ciphertext:  ciphertext,
		})
	}

	// Sign PoP over our operator address using our polynomial constant term
	pop, err := h.keyStore.SignPoP(dkgState.Round, opAddr)
	if err != nil {
		return nil, fmt.Errorf("contribution PoP failed: %w", err)
	}

	return &types.DKGVoteExtension{
		Round:                dkgState.Round,
		Phase:                types.DKGPhase_DKG_PHASE_CONTRIBUTING,
		FeldmanCommitments:   feldmanCommitments,
		EncryptedEvaluations: encryptedEvals,
		ContributionPop:      pop,
	}, nil
}

// buildDecryptionShareExtension creates an ACTIVE phase extension:
// epoch decryption share for the current shield epoch (when pending ops exist).
func (h *DKGVoteExtensionHandler) buildDecryptionShareExtension(ctx sdk.Context, dkgState types.DKGState, opAddr string) (*types.DKGVoteExtension, error) {
	// Only produce shares when encrypted batch is enabled
	params, err := h.keeper.Params.Get(ctx)
	if err != nil || !params.EncryptedBatchEnabled {
		return nil, nil
	}

	epochState, found := h.keeper.GetShieldEpochStateVal(ctx)
	if !found {
		return nil, nil
	}

	epoch := epochState.CurrentEpoch

	// Only produce shares when there are pending ops that need decryption
	pendingCount := h.keeper.GetPendingOpCountVal(ctx)
	if pendingCount == 0 {
		return nil, nil
	}

	// Check if decryption key already exists for this epoch (no need to share again)
	if _, found := h.keeper.GetShieldEpochDecryptionKeyVal(ctx, epoch); found {
		return nil, nil
	}

	// Check if we already submitted a share for this epoch
	if _, found := h.keeper.GetDecryptionShare(ctx, epoch, opAddr); found {
		return nil, nil
	}

	// Compute our decryption share
	share, err := h.keyStore.ComputeDecryptionShare(dkgState.Round, epoch)
	if err != nil {
		return nil, fmt.Errorf("decryption share computation failed: %w", err)
	}

	return &types.DKGVoteExtension{
		Round:           dkgState.Round,
		Phase:           types.DKGPhase_DKG_PHASE_ACTIVE,
		DecryptionEpoch: epoch,
		DecryptionShare: share,
	}, nil
}

// verifyExtension validates a DKG vote extension is well-formed.
func (h *DKGVoteExtensionHandler) verifyExtension(ctx sdk.Context, ext *types.DKGVoteExtension) error {
	dkgState, found := h.keeper.GetDKGStateVal(ctx)
	if !found {
		return fmt.Errorf("no DKG state")
	}

	// Round must match
	if ext.Round != dkgState.Round {
		return fmt.Errorf("round mismatch: ext=%d, state=%d", ext.Round, dkgState.Round)
	}

	// Phase must match
	if ext.Phase != dkgState.Phase {
		return fmt.Errorf("phase mismatch: ext=%s, state=%s", ext.Phase, dkgState.Phase)
	}

	switch ext.Phase {
	case types.DKGPhase_DKG_PHASE_REGISTERING:
		return h.verifyRegistrationExtension(ext)
	case types.DKGPhase_DKG_PHASE_CONTRIBUTING:
		return h.verifyContributionExtension(ext, dkgState)
	case types.DKGPhase_DKG_PHASE_ACTIVE:
		return h.verifyDecryptionShareExtension(ext)
	default:
		return fmt.Errorf("unexpected phase: %s", ext.Phase)
	}
}

// verifyRegistrationExtension validates a REGISTERING phase extension.
func (h *DKGVoteExtensionHandler) verifyRegistrationExtension(ext *types.DKGVoteExtension) error {
	if len(ext.RegistrationPubKey) == 0 {
		return fmt.Errorf("empty pub key")
	}

	// Validate it's a valid BN256 G1 point
	point := tleSuite.G1().Point()
	if err := point.UnmarshalBinary(ext.RegistrationPubKey); err != nil {
		return fmt.Errorf("invalid pub key: %w", err)
	}

	// Reject identity element
	if point.Equal(tleSuite.G1().Point().Null()) {
		return fmt.Errorf("pub key is identity element")
	}

	// PoP must be non-empty (full verification happens in PreBlocker when we know the operator address)
	if len(ext.RegistrationPop) == 0 {
		return fmt.Errorf("empty PoP")
	}

	return nil
}

// verifyContributionExtension validates a CONTRIBUTING phase extension.
func (h *DKGVoteExtensionHandler) verifyContributionExtension(ext *types.DKGVoteExtension, dkgState types.DKGState) error {
	threshold := computeThreshold(dkgState)

	// Validate Feldman commitments count
	if uint64(len(ext.FeldmanCommitments)) != threshold {
		return fmt.Errorf("expected %d commitments, got %d", threshold, len(ext.FeldmanCommitments))
	}

	// Validate each commitment is a valid G1 point
	for i, c := range ext.FeldmanCommitments {
		point := tleSuite.G1().Point()
		if err := point.UnmarshalBinary(c); err != nil {
			return fmt.Errorf("commitment %d invalid: %w", i, err)
		}
		if point.Equal(tleSuite.G1().Point().Null()) {
			return fmt.Errorf("commitment %d is identity", i)
		}
	}

	// Validate encrypted evaluations have non-empty ciphertexts
	for i, eval := range ext.EncryptedEvaluations {
		if eval.TargetIndex == 0 || eval.TargetIndex > uint32(len(dkgState.ExpectedValidators)) {
			return fmt.Errorf("eval %d: invalid target index %d", i, eval.TargetIndex)
		}
		if len(eval.Ciphertext) == 0 {
			return fmt.Errorf("eval %d: empty ciphertext", i)
		}
	}

	// PoP must be present
	if len(ext.ContributionPop) == 0 {
		return fmt.Errorf("empty contribution PoP")
	}

	return nil
}

// verifyDecryptionShareExtension validates an ACTIVE phase extension (decryption share).
func (h *DKGVoteExtensionHandler) verifyDecryptionShareExtension(ext *types.DKGVoteExtension) error {
	if len(ext.DecryptionShare) == 0 {
		return fmt.Errorf("empty decryption share")
	}

	// Validate decryption share is a valid BN256 G1 point
	point := tleSuite.G1().Point()
	if err := point.UnmarshalBinary(ext.DecryptionShare); err != nil {
		return fmt.Errorf("invalid decryption share: not a valid G1 point: %w", err)
	}

	// Reject identity element
	if point.Equal(tleSuite.G1().Point().Null()) {
		return fmt.Errorf("decryption share is the identity element")
	}

	return nil
}

// getOperatorAddr maps this validator's consensus address to their operator address.
func (h *DKGVoteExtensionHandler) getOperatorAddr(ctx sdk.Context) (string, error) {
	sk := h.keeper.GetStakingKeeper()
	if sk == nil {
		return "", fmt.Errorf("staking keeper not wired")
	}

	val, err := sk.GetValidatorByConsAddr(ctx, h.consAddr)
	if err != nil {
		return "", err
	}
	return val.GetOperator(), nil
}

// loadValidatorConsAddress reads the CometBFT consensus address from priv_validator_key.json.
// Returns nil if the file is not found (e.g., remote signer / KMS).
func loadValidatorConsAddress(homeDir string) sdk.ConsAddress {
	keyFile := filepath.Join(homeDir, "config", "priv_validator_key.json")
	data, err := os.ReadFile(keyFile)
	if err != nil {
		return nil
	}

	var key struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal(data, &key); err != nil {
		return nil
	}

	addrBytes, err := hex.DecodeString(key.Address)
	if err != nil {
		return nil
	}

	return sdk.ConsAddress(addrBytes)
}

// --- helpers (duplicated from keeper to avoid circular import) ---

func isValInDKG(state types.DKGState, valAddr string) bool {
	for _, v := range state.ExpectedValidators {
		if v == valAddr {
			return true
		}
	}
	return false
}

func valDKGIndex(state types.DKGState, valAddr string) uint32 {
	for i, v := range state.ExpectedValidators {
		if v == valAddr {
			return uint32(i + 1)
		}
	}
	return 0
}

func computeThreshold(state types.DKGState) uint64 {
	n := uint64(len(state.ExpectedValidators))
	num := uint64(state.ThresholdNumerator)
	den := uint64(state.ThresholdDenominator)
	if den == 0 || n == 0 {
		return 0
	}
	t := (n * num) / den
	if t == 0 {
		t = 1
	}
	return t
}
