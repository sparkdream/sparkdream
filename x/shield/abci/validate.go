package abci

import (
	"fmt"
	commontypes "sparkdream/x/common/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

// The checks below are the single acceptance rule for each kind of DKG vote
// extension, applied by VerifyVoteExtension, by PrepareProposal (so an honest
// proposer never injects something ProcessProposal would reject), by
// ProcessProposal and by the PreBlocker before anything is stored. They
// depend only on committed state, so every validator reaches the same verdict.

// ceremonyFrozen reports whether the DKG is in a ceremony phase (REGISTERING
// through JUSTIFYING) while shield is disabled. The BeginBlocker, which
// advances the phases, does not run while params.Enabled is false, so ceremony
// data gathered then could only pile up against deadlines that never move:
// every ABCI path refuses it. ACTIVE-phase decryption shares keep flowing,
// because the EndBlocker still judges the liveness of demanded epochs.
func ceremonyFrozen(ctx sdk.Context, k keeper.Keeper, phase types.DKGPhase) bool {
	switch phase {
	case types.DKGPhase_DKG_PHASE_REGISTERING, types.DKGPhase_DKG_PHASE_CONTRIBUTING,
		types.DKGPhase_DKG_PHASE_COMPLAINING, types.DKGPhase_DKG_PHASE_JUSTIFYING:
	default:
		return false
	}
	params, err := k.Params.Get(ctx)
	return err != nil || !params.Enabled
}

// validateRegistration checks a REGISTERING extension: a non-identity G1 key
// and a proof of possession over (operator, round).
func validateRegistration(ext *types.DKGVoteExtension, opAddr string, round uint64) error {
	if len(ext.RegistrationPubKey) == 0 {
		return fmt.Errorf("empty pub key")
	}
	point := tleSuite.G1().Point()
	if err := point.UnmarshalBinary(ext.RegistrationPubKey); err != nil {
		return fmt.Errorf("invalid pub key: %w", err)
	}
	if point.Equal(tleSuite.G1().Point().Null()) {
		return fmt.Errorf("pub key is identity element")
	}
	if err := keeper.VerifyProofOfPossession(ext.RegistrationPubKey, ext.RegistrationPop, opAddr, round); err != nil {
		return fmt.Errorf("registration PoP: %w", err)
	}
	return nil
}

// validateContribution checks a CONTRIBUTING extension: threshold-many
// non-identity G1 commitments, matching G2 commitments (pairing-consistent),
// in-range evaluation targets, and a PoP over (operator, round) under a_0.
func validateContribution(ext *types.DKGVoteExtension, dkgState types.DKGState, opAddr string) error {
	threshold := computeThreshold(dkgState)
	if err := keeper.ValidateFeldmanCommitments(ext.FeldmanCommitments, commontypes.Saturate[int](threshold)); err != nil {
		return fmt.Errorf("invalid G1 commitments: %w", err)
	}
	if err := keeper.ValidateFeldmanCommitmentsG2(ext.FeldmanCommitmentsG2, commontypes.Saturate[int](threshold)); err != nil {
		return fmt.Errorf("invalid G2 commitments: %w", err)
	}
	if err := keeper.ValidateFeldmanCommitmentsConsistency(ext.FeldmanCommitments, ext.FeldmanCommitmentsG2); err != nil {
		return fmt.Errorf("G1/G2 commitment mismatch: %w", err)
	}
	seen := map[uint32]bool{}
	for i, eval := range ext.EncryptedEvaluations {
		if eval.TargetIndex == 0 || int(eval.TargetIndex) > len(dkgState.ExpectedValidators) {
			return fmt.Errorf("eval %d: invalid target index %d", i, eval.TargetIndex)
		}
		if seen[eval.TargetIndex] {
			return fmt.Errorf("eval %d: duplicate target index %d", i, eval.TargetIndex)
		}
		seen[eval.TargetIndex] = true
		if len(eval.Ciphertext) == 0 {
			return fmt.Errorf("eval %d: empty ciphertext", i)
		}
	}
	if err := keeper.VerifyProofOfPossession(ext.FeldmanCommitments[0], ext.ContributionPop, opAddr, dkgState.Round); err != nil {
		return fmt.Errorf("contribution PoP: %w", err)
	}
	return nil
}

// validateComplaints checks a COMPLAINING extension: every accused index is a
// participant other than the complainer, listed once, and dealt a
// contribution this round (a dealer that never contributed is already out).
func validateComplaints(ctx sdk.Context, k keeper.Keeper, ext *types.DKGVoteExtension, dkgState types.DKGState, opAddr string) error {
	selfIdx := valDKGIndex(dkgState, opAddr)
	seen := map[uint32]bool{}
	for i, idx := range ext.Complaints {
		if idx == 0 || int(idx) > len(dkgState.ExpectedValidators) {
			return fmt.Errorf("complaint %d: invalid dealer index %d", i, idx)
		}
		if idx == selfIdx {
			return fmt.Errorf("complaint %d: validator complains about itself", i)
		}
		if seen[idx] {
			return fmt.Errorf("complaint %d: duplicate dealer index %d", i, idx)
		}
		seen[idx] = true
		dealer := dkgState.ExpectedValidators[idx-1]
		if _, found := k.GetDKGContributionVal(ctx, dealer); !found {
			return fmt.Errorf("complaint %d: dealer %s has no contribution", i, dealer)
		}
	}
	return nil
}

// validateJustifications checks a JUSTIFYING extension: each answer names a
// participant that complained about this dealer and carries the evaluation
// p_dealer(complainer) that matches the dealer's own commitments.
func validateJustifications(ctx sdk.Context, k keeper.Keeper, ext *types.DKGVoteExtension, dkgState types.DKGState, opAddr string) error {
	contribution, found := k.GetDKGContributionVal(ctx, opAddr)
	if !found {
		return fmt.Errorf("justifying dealer %s has no contribution", opAddr)
	}
	seen := map[uint32]bool{}
	for i, j := range ext.Justifications {
		if j == nil {
			return fmt.Errorf("justification %d: empty", i)
		}
		if j.ComplainerIndex == 0 || int(j.ComplainerIndex) > len(dkgState.ExpectedValidators) {
			return fmt.Errorf("justification %d: invalid complainer index %d", i, j.ComplainerIndex)
		}
		if seen[j.ComplainerIndex] {
			return fmt.Errorf("justification %d: duplicate complainer index %d", i, j.ComplainerIndex)
		}
		seen[j.ComplainerIndex] = true
		complainer := dkgState.ExpectedValidators[j.ComplainerIndex-1]
		if _, found := k.GetDKGComplaint(ctx, opAddr, complainer); !found {
			return fmt.Errorf("justification %d: %s did not complain about %s", i, complainer, opAddr)
		}
		if err := keeper.VerifyEvaluation(contribution.FeldmanCommitments, j.ComplainerIndex, j.Evaluation); err != nil {
			return fmt.Errorf("justification %d: %w", i, err)
		}
	}
	return nil
}

// validateExtension dispatches on the phase the extension claims, which must
// be the current one.
func validateExtension(ctx sdk.Context, k keeper.Keeper, ext *types.DKGVoteExtension, dkgState types.DKGState, opAddr string) error {
	if ext.Round != dkgState.Round {
		return fmt.Errorf("round mismatch: ext=%d, state=%d", ext.Round, dkgState.Round)
	}
	if ext.Phase != dkgState.Phase {
		return fmt.Errorf("phase mismatch: ext=%s, state=%s", ext.Phase, dkgState.Phase)
	}
	if !isValInDKG(dkgState, opAddr) {
		return fmt.Errorf("%s is not a DKG participant", opAddr)
	}
	switch ext.Phase {
	case types.DKGPhase_DKG_PHASE_REGISTERING:
		return validateRegistration(ext, opAddr, dkgState.Round)
	case types.DKGPhase_DKG_PHASE_CONTRIBUTING:
		return validateContribution(ext, dkgState, opAddr)
	case types.DKGPhase_DKG_PHASE_COMPLAINING:
		return validateComplaints(ctx, k, ext, dkgState, opAddr)
	case types.DKGPhase_DKG_PHASE_JUSTIFYING:
		return validateJustifications(ctx, k, ext, dkgState, opAddr)
	case types.DKGPhase_DKG_PHASE_ACTIVE:
		if len(ext.DecryptionShare) == 0 {
			return fmt.Errorf("empty decryption share")
		}
		return k.VerifyValidatorDecryptionShare(ctx, opAddr, ext.DecryptionEpoch, ext.DecryptionShare)
	default:
		return fmt.Errorf("unexpected phase: %s", ext.Phase)
	}
}
