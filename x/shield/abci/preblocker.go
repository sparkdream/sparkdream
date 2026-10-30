package abci

import (
	"encoding/hex"
	"fmt"
	"slices"
	commontypes "sparkdream/x/common/types"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

// ProcessDKGInjection extracts and processes DKG vote extension data from tx[0]
// of the FinalizeBlock request. This runs in the app-level PreBlocker, before
// BeginBlock and normal tx delivery.
//
// If tx[0] is a DKG injection (identified by the magic prefix), it:
//  1. Decodes the InjectedDKGData
//  2. Maps each validator's consensus address to their operator address
//  3. Re-applies the phase's acceptance rule (validate.go) and stores what
//     passes: registrations, contributions, complaints, justifications, or
//     verified decryption shares (reconstructing epoch keys at threshold)
//
// req.Txs is left untouched: CometBFT requires one tx result per tx in the
// block, so the pseudo-tx must stay and fall through normal delivery, where
// it fails to decode (the magic prefix is not a valid tx) and is recorded as a
// failed tx. Removing it here halts consensus with "expected tx results
// length to match size of transactions in block".
func ProcessDKGInjection(ctx sdk.Context, k keeper.Keeper, req *abci.RequestFinalizeBlock) {
	if len(req.Txs) == 0 {
		return
	}

	if !HasDKGInjectionPrefix(req.Txs[0]) {
		return
	}

	injected, err := DecodeDKGInjection(req.Txs[0])
	if err != nil || injected == nil {
		ctx.Logger().With("module", "x/shield").Error(
			"failed to decode DKG injection in PreBlocker", "err", err)
		return
	}

	// Verify DKG state matches
	dkgState, found := k.GetDKGStateVal(ctx)
	if !found || injected.Round != dkgState.Round || injected.Phase != dkgState.Phase {
		ctx.Logger().With("module", "x/shield").Warn(
			"DKG injection round/phase mismatch, ignoring",
			"injected_round", injected.Round,
			"injected_phase", injected.Phase,
		)
		return
	}
	if ceremonyFrozen(ctx, k, dkgState.Phase) {
		return
	}

	logger := ctx.Logger().With("module", "x/shield")

	// Ceremony extensions must carry their validator's vote-extension
	// signature from the last commit; re-checked here, not trusted from
	// ProcessProposal (a node replaying blocks never ran it).
	sig := signedAt{height: req.Height - 1, round: req.DecidedLastCommit.Round}

	switch injected.Phase {
	case types.DKGPhase_DKG_PHASE_REGISTERING:
		applied := processRegistrations(ctx, k, dkgState, sig, injected.Extensions)
		logger.Info("processed DKG registrations from vote extensions",
			"round", injected.Round,
			"applied", applied,
			"total_extensions", len(injected.Extensions),
		)

	case types.DKGPhase_DKG_PHASE_CONTRIBUTING:
		applied := processContributions(ctx, k, dkgState, sig, injected.Extensions)
		if applied > 0 {
			// Update contributions received count
			dkgState.ContributionsReceived += uint64(applied)
			if err := k.SetDKGStateVal(ctx, dkgState); err != nil {
				ctx.Logger().Error("ProcessDKGInjection: k.SetDKGStateVal failed", "err", err)
			}
		}
		logger.Info("processed DKG contributions from vote extensions",
			"round", injected.Round,
			"applied", applied,
			"total_contributions", dkgState.ContributionsReceived,
		)

	case types.DKGPhase_DKG_PHASE_COMPLAINING:
		applied := processComplaints(ctx, k, dkgState, sig, injected.Extensions)
		if applied > 0 {
			logger.Info("processed DKG complaints from vote extensions", "round", injected.Round, "applied", applied)
		}

	case types.DKGPhase_DKG_PHASE_JUSTIFYING:
		applied := processJustifications(ctx, k, dkgState, sig, injected.Extensions)
		if applied > 0 {
			logger.Info("processed DKG justifications from vote extensions", "round", injected.Round, "applied", applied)
		}

	case types.DKGPhase_DKG_PHASE_ACTIVE:
		applied := processDecryptionShares(ctx, k, dkgState, injected.DecryptionShares)
		if applied > 0 {
			logger.Info("processed decryption shares from vote extensions",
				"round", injected.Round,
				"applied", applied,
				"total_shares", len(injected.DecryptionShares),
			)
		}
	}
}

// signedAt is where the injected extensions were signed: the previous
// height and the last commit's round.
type signedAt struct {
	height int64
	round  int32
}

// operatorFor maps an injected extension's consensus address to its operator,
// checks the validator's vote-extension signature and re-runs the acceptance
// rule; false means skip.
func operatorFor(ctx sdk.Context, k keeper.Keeper, dkgState types.DKGState, sig signedAt, ve *types.ValidatorDKGExtension) (string, bool) {
	opAddr, err := consAddrToOperator(ctx, k, ve.ValidatorAddress)
	if err != nil {
		return "", false
	}
	if err := verifyExtensionSignature(ctx, k, ve.ValidatorAddress, &ve.Extension, ve.ExtensionSignature, sig.height, sig.round); err != nil {
		ctx.Logger().With("module", "x/shield").Warn("unsigned or misattributed DKG extension rejected in PreBlocker", "validator", opAddr, "err", err)
		return "", false
	}
	if err := validateExtension(ctx, k, &ve.Extension, dkgState, opAddr); err != nil {
		ctx.Logger().With("module", "x/shield").Debug("DKG extension rejected in PreBlocker", "validator", opAddr, "err", err)
		return "", false
	}
	return opAddr, true
}

// processRegistrations stores BN256 pub keys from REGISTERING phase extensions.
// Returns the number of new registrations applied.
func processRegistrations(ctx sdk.Context, k keeper.Keeper, dkgState types.DKGState, sig signedAt, extensions []*types.ValidatorDKGExtension) int {
	applied := 0

	for _, ve := range extensions {
		opAddr, ok := operatorFor(ctx, k, dkgState, sig, ve)
		if !ok {
			continue
		}
		ext := &ve.Extension

		// Skip if already registered for this round (first key wins)
		if existing, found := k.GetDKGRegistration(ctx, opAddr); found && existing.Round == dkgState.Round {
			continue
		}

		// Store the registration (reuse DKGContribution proto)
		reg := types.DKGContribution{
			ValidatorAddress:   opAddr,
			Round:              dkgState.Round,
			FeldmanCommitments: [][]byte{ext.RegistrationPubKey}, // [0] = pub key
			ProofOfPossession:  ext.RegistrationPop,
		}
		if err := k.SetDKGRegistration(ctx, reg); err != nil {
			continue
		}

		applied++

		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeShieldDKGRegistration,
			sdk.NewAttribute(types.AttributeKeyValidator, opAddr),
			sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", dkgState.Round)),
		))
	}

	return applied
}

// processContributions stores Feldman DKG contributions from CONTRIBUTING phase extensions.
// Returns the number of new contributions applied.
func processContributions(ctx sdk.Context, k keeper.Keeper, dkgState types.DKGState, sig signedAt, extensions []*types.ValidatorDKGExtension) int {
	applied := 0

	for _, ve := range extensions {
		opAddr, ok := operatorFor(ctx, k, dkgState, sig, ve)
		if !ok {
			continue
		}
		ext := &ve.Extension

		// Skip if already contributed for this round (first contribution wins)
		if _, exists := k.GetDKGContributionVal(ctx, opAddr); exists {
			continue
		}

		contribution := types.DKGContribution{
			ValidatorAddress:     opAddr,
			Round:                dkgState.Round,
			FeldmanCommitments:   ext.FeldmanCommitments,
			FeldmanCommitmentsG2: ext.FeldmanCommitmentsG2,
			EncryptedEvaluations: ext.EncryptedEvaluations,
			ProofOfPossession:    ext.ContributionPop,
		}
		if err := k.SetDKGContributionVal(ctx, contribution); err != nil {
			continue
		}

		applied++

		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeShieldDKGContribution,
			sdk.NewAttribute(types.AttributeKeyValidator, opAddr),
			sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", dkgState.Round)),
			sdk.NewAttribute(types.AttributeKeyContributionsCount, fmt.Sprintf("%d", dkgState.ContributionsReceived+commontypes.Saturate[uint64](applied))),
		))
	}

	return applied
}

// processComplaints records complaints from COMPLAINING phase extensions.
// Returns the number of new complaints recorded.
func processComplaints(ctx sdk.Context, k keeper.Keeper, dkgState types.DKGState, sig signedAt, extensions []*types.ValidatorDKGExtension) int {
	applied := 0
	for _, ve := range extensions {
		opAddr, ok := operatorFor(ctx, k, dkgState, sig, ve)
		if !ok {
			continue
		}
		for _, idx := range ve.Extension.Complaints {
			dealer := dkgState.ExpectedValidators[idx-1]
			if _, found := k.GetDKGComplaint(ctx, dealer, opAddr); found {
				continue
			}
			if err := k.SetDKGComplaint(ctx, types.DKGComplaint{DealerAddress: dealer, ComplainerAddress: opAddr}); err != nil {
				continue
			}
			applied++
			ctx.EventManager().EmitEvent(sdk.NewEvent(
				types.EventTypeShieldDKGComplaint,
				sdk.NewAttribute(types.AttributeKeyDealer, dealer),
				sdk.NewAttribute(types.AttributeKeyComplainer, opAddr),
				sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", dkgState.Round)),
			))
		}
	}
	return applied
}

// processJustifications records valid answers to complaints from JUSTIFYING
// phase extensions (the evaluation was verified against the dealer's
// commitments by validateExtension). Returns the number applied.
func processJustifications(ctx sdk.Context, k keeper.Keeper, dkgState types.DKGState, sig signedAt, extensions []*types.ValidatorDKGExtension) int {
	applied := 0
	for _, ve := range extensions {
		opAddr, ok := operatorFor(ctx, k, dkgState, sig, ve)
		if !ok {
			continue
		}
		for _, j := range ve.Extension.Justifications {
			complainer := dkgState.ExpectedValidators[j.ComplainerIndex-1]
			complaint, found := k.GetDKGComplaint(ctx, opAddr, complainer)
			if !found || complaint.Justified {
				continue
			}
			complaint.Justified = true
			complaint.Evaluation = j.Evaluation
			if err := k.SetDKGComplaint(ctx, complaint); err != nil {
				continue
			}
			applied++
			ctx.EventManager().EmitEvent(sdk.NewEvent(
				types.EventTypeShieldDKGJustification,
				sdk.NewAttribute(types.AttributeKeyDealer, opAddr),
				sdk.NewAttribute(types.AttributeKeyComplainer, complainer),
				sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", dkgState.Round)),
			))
		}
	}
	return applied
}

// processDecryptionShares stores validator decryption shares from ACTIVE phase vote extensions
// and attempts to reconstruct the epoch decryption key when the threshold is met.
// Returns the number of new shares applied.
func processDecryptionShares(ctx sdk.Context, k keeper.Keeper, _ types.DKGState, shares []*types.InjectedDecryptionShare) int {
	applied := 0
	epochSeen := map[uint64]bool{}

	for _, ds := range shares {
		opAddr, err := consAddrToOperator(ctx, k, ds.ValidatorAddress)
		if err != nil {
			continue
		}
		// Re-verify before storing. VerifyVoteExtension and ProcessProposal
		// already applied this rule, but reconstruction interpolates whatever
		// is in state, and one bad share would silently corrupt the epoch key
		// for everyone: never store an unverified share.
		if err := k.VerifyValidatorDecryptionShare(ctx, opAddr, ds.Epoch, ds.Share); err != nil {
			ctx.EventManager().EmitEvent(sdk.NewEvent(
				types.EventTypeTLEInvalidShare,
				sdk.NewAttribute(types.AttributeKeyValidator, opAddr),
				sdk.NewAttribute(types.AttributeKeyEpoch, fmt.Sprintf("%d", ds.Epoch)),
				sdk.NewAttribute(types.AttributeKeyError, err.Error()),
			))
			continue
		}
		stored, err := k.StoreVerifiedDecryptionShare(ctx, opAddr, ds.Epoch, ds.Share)
		if err != nil || !stored {
			continue
		}
		applied++
		epochSeen[ds.Epoch] = true
	}

	// Sorted, so the reconstruction events come out in the same order on
	// every node (map order is random).
	epochs := make([]uint64, 0, len(epochSeen))
	for epoch := range epochSeen {
		epochs = append(epochs, epoch)
	}
	slices.Sort(epochs)
	for _, epoch := range epochs {
		k.TryReconstructEpochKey(ctx, epoch)
	}
	return applied
}

// consAddrToOperator maps a CometBFT consensus address to a Cosmos SDK operator address.
func consAddrToOperator(ctx sdk.Context, k keeper.Keeper, consAddrBytes []byte) (string, error) {
	sk := k.GetStakingKeeper()
	if sk == nil {
		return "", fmt.Errorf("staking keeper not wired")
	}

	consAddr := sdk.ConsAddress(consAddrBytes)
	val, err := sk.GetValidatorByConsAddr(ctx, consAddr)
	if err != nil {
		return "", fmt.Errorf("validator not found for cons addr %s: %w",
			hex.EncodeToString(consAddrBytes), err)
	}

	return val.GetOperator(), nil
}
