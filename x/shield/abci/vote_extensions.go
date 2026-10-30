package abci

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	commontypes "sparkdream/x/common/types"
	"sync"

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

	cleanupMu    sync.Mutex
	cleanedRound uint64 // last round whose predecessors' key files were removed
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
// Returns an empty extension outside an active DKG round, when this
// validator has nothing to say in the current phase, or on any error (never
// blocks consensus).
func (h *DKGVoteExtensionHandler) ExtendVoteHandler() sdk.ExtendVoteHandler {
	return func(ctx sdk.Context, req *abci.RequestExtendVote) (*abci.ResponseExtendVote, error) {
		ext, err := h.buildExtension(ctx)
		if err != nil {
			ctx.Logger().With("module", "x/shield").Error("building DKG vote extension failed", "err", err)
		}
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
// ACCEPT for empty extensions (validator not participating or no DKG active).
// REJECT for anything that fails the phase's acceptance rule (validate.go):
// an honest validator never produces it, and rejecting keeps it out of the
// commit entirely.
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

		if err := h.verifyExtension(ctx, &ext, req.ValidatorAddress); err != nil {
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
	if !found || ceremonyFrozen(ctx, h.keeper, dkgState.Phase) {
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
	case types.DKGPhase_DKG_PHASE_COMPLAINING:
		return h.buildComplaintExtension(ctx, dkgState, opAddr)
	case types.DKGPhase_DKG_PHASE_JUSTIFYING:
		return h.buildJustificationExtension(ctx, dkgState, opAddr)
	case types.DKGPhase_DKG_PHASE_ACTIVE:
		return h.buildDecryptionShareExtension(ctx, dkgState, opAddr)
	default:
		return nil, nil
	}
}

// buildRegistrationExtension creates a REGISTERING phase extension:
// BN256 G1 public key + Schnorr proof of possession over (operator, round).
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
	_, commitments, err := h.keyStore.GeneratePolynomial(dkgState.Round, commontypes.Saturate[int](threshold))
	if err != nil {
		return nil, fmt.Errorf("polynomial generation failed: %w", err)
	}

	// Marshal Feldman commitments (G1)
	feldmanCommitments := make([][]byte, len(commitments))
	for i, c := range commitments {
		bz, err := c.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("commitment marshal failed: %w", err)
		}
		feldmanCommitments[i] = bz
	}

	// Compute and marshal G2 Feldman commitments from the same polynomial coefficients.
	// Each G2 commitment = a_k * G2_gen (same scalar, different group generator).
	g2Suite := bn256.NewSuiteG2()
	feldmanCommitmentsG2 := make([][]byte, len(commitments))
	for i := range commitments {
		coeff, err := h.keyStore.GetPolynomialCoefficient(dkgState.Round, i)
		if err != nil {
			return nil, fmt.Errorf("failed to get polynomial coefficient %d: %w", i, err)
		}
		g2Point := g2Suite.G2().Point().Mul(coeff, nil)
		bz, err := g2Point.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("G2 commitment %d marshal failed: %w", i, err)
		}
		feldmanCommitmentsG2[i] = bz
	}

	// Build encrypted evaluations for each OTHER participant. Every
	// participant registered a key this round (the set was narrowed to
	// registrants when CONTRIBUTING opened), so a missing evaluation here is
	// a bug on our side, not theirs.
	selfIdx := valDKGIndex(dkgState, opAddr)
	var encryptedEvals []*types.EncryptedEvaluation

	for i, targetAddr := range dkgState.ExpectedValidators {
		targetIdx := commontypes.Saturate[uint32](i + 1)
		if targetIdx == selfIdx {
			continue // Don't encrypt for ourselves
		}

		reg, found := h.keeper.GetDKGRegistration(ctx, targetAddr)
		if !found || reg.Round != dkgState.Round {
			ctx.Logger().With("module", "x/shield").Error("DKG participant has no registration for this round", "validator", targetAddr, "round", dkgState.Round)
			continue
		}
		targetPubKeyBytes := keeper.GetDKGRegistrationPubKey(reg)
		targetPubKey := tleSuite.Point()
		if err := targetPubKey.UnmarshalBinary(targetPubKeyBytes); err != nil {
			continue // Skip malformed registrations
		}

		// Evaluate our polynomial at the target's index
		evalScalar, err := h.keyStore.EvaluatePolynomial(dkgState.Round, targetIdx)
		if err != nil {
			return nil, fmt.Errorf("evaluate polynomial at %d: %w", targetIdx, err)
		}
		evalBytes, err := evalScalar.MarshalBinary()
		if err != nil {
			return nil, err
		}

		// ECIES-encrypt the evaluation with the target's public key
		ciphertext, err := ecies.Encrypt(tleSuite, targetPubKey, evalBytes, nil)
		if err != nil {
			return nil, fmt.Errorf("encrypt evaluation for %d: %w", targetIdx, err)
		}

		encryptedEvals = append(encryptedEvals, &types.EncryptedEvaluation{
			TargetIndex: targetIdx,
			Ciphertext:  ciphertext,
		})
	}

	// Sign PoP over (operator, round) using our polynomial constant term
	pop, err := h.keyStore.SignPoP(dkgState.Round, opAddr)
	if err != nil {
		return nil, fmt.Errorf("contribution PoP failed: %w", err)
	}

	return &types.DKGVoteExtension{
		Round:                dkgState.Round,
		Phase:                types.DKGPhase_DKG_PHASE_CONTRIBUTING,
		FeldmanCommitments:   feldmanCommitments,
		FeldmanCommitmentsG2: feldmanCommitmentsG2,
		EncryptedEvaluations: encryptedEvals,
		ContributionPop:      pop,
	}, nil
}

// incomingEvaluations assembles, from on-chain DKG state, every other
// qualified dealer's evaluation for this validator: the dealer's commitments
// (to verify against) plus the ciphertext it published for our index or, if
// we complained and the dealer answered, the evaluation in clear.
func (h *DKGVoteExtensionHandler) incomingEvaluations(ctx sdk.Context, dkgState types.DKGState, opAddr string, selfIdx uint32) []keeper.IncomingEvaluation {
	var incoming []keeper.IncomingEvaluation
	for _, c := range h.keeper.QualifiedDKGContributions(ctx, dkgState) {
		if c.ValidatorAddress == opAddr {
			continue // covered by our local polynomial evaluation
		}
		ev := keeper.IncomingEvaluation{
			Dealer:      c.ValidatorAddress,
			DealerIndex: valDKGIndex(dkgState, c.ValidatorAddress),
			Commitments: c.FeldmanCommitments,
		}
		for _, e := range c.EncryptedEvaluations {
			if e.TargetIndex == selfIdx {
				ev.Ciphertext = e.Ciphertext
				break
			}
		}
		if complaint, found := h.keeper.GetDKGComplaint(ctx, c.ValidatorAddress, opAddr); found && complaint.Justified {
			ev.Plaintext = complaint.Evaluation
		}
		incoming = append(incoming, ev)
	}
	return incoming
}

// buildComplaintExtension creates a COMPLAINING phase extension listing the
// dealers whose evaluation for us is missing, does not open, or does not
// match their commitments. Nothing to complain about: no extension.
func (h *DKGVoteExtensionHandler) buildComplaintExtension(ctx sdk.Context, dkgState types.DKGState, opAddr string) (*types.DKGVoteExtension, error) {
	selfIdx := valDKGIndex(dkgState, opAddr)
	if selfIdx == 0 {
		return nil, nil
	}
	complaints, err := h.keyStore.FindComplaints(dkgState.Round, selfIdx, h.incomingEvaluations(ctx, dkgState, opAddr, selfIdx))
	if err != nil {
		return nil, fmt.Errorf("checking dealt evaluations: %w", err)
	}
	// Only complaints the chain does not have yet.
	var fresh []uint32
	for _, idx := range complaints {
		dealer := dkgState.ExpectedValidators[idx-1]
		if _, found := h.keeper.GetDKGComplaint(ctx, dealer, opAddr); !found {
			fresh = append(fresh, idx)
		}
	}
	if len(fresh) == 0 {
		return nil, nil
	}
	return &types.DKGVoteExtension{
		Round:      dkgState.Round,
		Phase:      types.DKGPhase_DKG_PHASE_COMPLAINING,
		Complaints: fresh,
	}, nil
}

// buildJustificationExtension creates a JUSTIFYING phase extension answering
// every open complaint against us with the disputed evaluation in clear.
func (h *DKGVoteExtensionHandler) buildJustificationExtension(ctx sdk.Context, dkgState types.DKGState, opAddr string) (*types.DKGVoteExtension, error) {
	var justifications []*types.DKGJustification
	for _, c := range h.keeper.ComplaintsAgainst(ctx, opAddr) {
		if c.Justified {
			continue
		}
		idx := valDKGIndex(dkgState, c.ComplainerAddress)
		if idx == 0 {
			continue
		}
		eval, err := h.keyStore.Justify(dkgState.Round, idx)
		if err != nil {
			return nil, fmt.Errorf("justify complaint from %s: %w", c.ComplainerAddress, err)
		}
		justifications = append(justifications, &types.DKGJustification{ComplainerIndex: idx, Evaluation: eval})
	}
	if len(justifications) == 0 {
		return nil, nil
	}
	return &types.DKGVoteExtension{
		Round:          dkgState.Round,
		Phase:          types.DKGPhase_DKG_PHASE_JUSTIFYING,
		Justifications: justifications,
	}, nil
}

// buildDecryptionShareExtension creates an ACTIVE phase extension: the
// decryption share for the oldest demanded closed epoch we have not served.
func (h *DKGVoteExtensionHandler) buildDecryptionShareExtension(ctx sdk.Context, dkgState types.DKGState, opAddr string) (*types.DKGVoteExtension, error) {
	// The round is active: key files of earlier rounds are no longer needed
	// and could open every payload ever sealed to their key sets.
	h.cleanupRetiredRounds(dkgState.Round)

	// Only produce shares when encrypted batch is enabled
	params, err := h.keeper.Params.Get(ctx)
	if err != nil || !params.EncryptedBatchEnabled {
		return nil, nil
	}

	// Shares are released for closed epochs only, and only once the
	// EndBlocker has demanded them (the batch is ready): a key reconstructed
	// mid-epoch would let anyone read every later submission to that epoch.
	epoch, needed := h.keeper.NextEpochNeedingShare(ctx, opAddr)
	if !needed {
		return nil, nil
	}

	selfIdx := valDKGIndex(dkgState, opAddr)
	if selfIdx == 0 {
		return nil, fmt.Errorf("operator %s not in expected_validators", opAddr)
	}
	share, err := h.keyStore.ComputeDecryptionShare(dkgState.Round, epoch, selfIdx, h.incomingEvaluations(ctx, dkgState, opAddr, selfIdx))
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

func (h *DKGVoteExtensionHandler) cleanupRetiredRounds(round uint64) {
	h.cleanupMu.Lock()
	defer h.cleanupMu.Unlock()
	if h.cleanedRound == round {
		return
	}
	h.keyStore.Cleanup(round)
	h.cleanedRound = round
}

// verifyExtension validates a DKG vote extension against the phase's
// acceptance rule. consAddr is the voting validator's consensus address
// (from the request), mapped to its operator address.
func (h *DKGVoteExtensionHandler) verifyExtension(ctx sdk.Context, ext *types.DKGVoteExtension, consAddr []byte) error {
	dkgState, found := h.keeper.GetDKGStateVal(ctx)
	if !found {
		return fmt.Errorf("no DKG state")
	}
	if ceremonyFrozen(ctx, h.keeper, dkgState.Phase) {
		return fmt.Errorf("shield disabled during DKG phase %s", dkgState.Phase)
	}
	opAddr, err := consAddrToOperator(ctx, h.keeper, consAddr)
	if err != nil {
		return err
	}
	return validateExtension(ctx, h.keeper, ext, dkgState, opAddr)
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
			return commontypes.Saturate[uint32](i + 1)
		}
	}
	return 0
}

func computeThreshold(state types.DKGState) uint64 {
	return types.ComputeThreshold(
		uint64(state.ThresholdNumerator),
		uint64(state.ThresholdDenominator),
		uint64(len(state.ExpectedValidators)),
	)
}
