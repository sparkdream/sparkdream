package abci

import (
	"encoding/hex"
	"fmt"

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
//  3. For REGISTERING: stores pub keys as DKG registrations
//  4. For CONTRIBUTING: stores as DKG contributions + updates contribution count
//  5. Strips tx[0] from req.Txs so it doesn't go through normal tx delivery
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
		// Strip the bad injection so it doesn't pollute tx delivery
		req.Txs = req.Txs[1:]
		return
	}

	// Verify DKG state matches
	dkgState, found := k.GetDKGStateVal(ctx)
	if !found || injected.Round != dkgState.Round || injected.Phase != dkgState.Phase {
		ctx.Logger().With("module", "x/shield").Warn(
			"DKG injection round/phase mismatch, stripping",
			"injected_round", injected.Round,
			"injected_phase", injected.Phase,
		)
		req.Txs = req.Txs[1:]
		return
	}

	logger := ctx.Logger().With("module", "x/shield")

	switch injected.Phase {
	case types.DKGPhase_DKG_PHASE_REGISTERING:
		applied := processRegistrations(ctx, k, dkgState, injected.Extensions)
		logger.Info("processed DKG registrations from vote extensions",
			"round", injected.Round,
			"applied", applied,
			"total_extensions", len(injected.Extensions),
		)

	case types.DKGPhase_DKG_PHASE_CONTRIBUTING:
		applied := processContributions(ctx, k, dkgState, injected.Extensions)
		if applied > 0 {
			// Update contributions received count
			dkgState.ContributionsReceived += uint64(applied)
			_ = k.SetDKGStateVal(ctx, dkgState)
		}
		logger.Info("processed DKG contributions from vote extensions",
			"round", injected.Round,
			"applied", applied,
			"total_contributions", dkgState.ContributionsReceived,
		)

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

	// Strip tx[0] so it doesn't go through normal tx delivery
	req.Txs = req.Txs[1:]
}

// processRegistrations stores BN256 pub keys from REGISTERING phase extensions.
// Returns the number of new registrations applied.
func processRegistrations(ctx sdk.Context, k keeper.Keeper, dkgState types.DKGState, extensions []*types.ValidatorDKGExtension) int {
	applied := 0

	for _, ve := range extensions {
		// Map consensus address to operator address
		opAddr, err := consAddrToOperator(ctx, k, ve.ValidatorAddress)
		if err != nil {
			continue
		}

		// Must be in the DKG participant set
		if !isValInDKG(dkgState, opAddr) {
			continue
		}

		ext := &ve.Extension
		if len(ext.RegistrationPubKey) == 0 || len(ext.RegistrationPop) == 0 {
			continue
		}

		// Validate the pub key is a valid G1 point
		point := tleSuite.G1().Point()
		if err := point.UnmarshalBinary(ext.RegistrationPubKey); err != nil {
			continue
		}

		// Verify PoP (Schnorr signature over operator address)
		if err := verifyPoP(ext.RegistrationPubKey, ext.RegistrationPop, opAddr); err != nil {
			ctx.Logger().With("module", "x/shield").Debug(
				"DKG registration PoP verification failed",
				"validator", opAddr,
				"err", err,
			)
			continue
		}

		// Skip if already registered for this round
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
func processContributions(ctx sdk.Context, k keeper.Keeper, dkgState types.DKGState, extensions []*types.ValidatorDKGExtension) int {
	applied := 0
	threshold := computeThreshold(dkgState)

	for _, ve := range extensions {
		opAddr, err := consAddrToOperator(ctx, k, ve.ValidatorAddress)
		if err != nil {
			continue
		}

		if !isValInDKG(dkgState, opAddr) {
			continue
		}

		ext := &ve.Extension

		// Skip if already contributed for this round
		if _, exists := k.GetDKGContributionVal(ctx, opAddr); exists {
			continue
		}

		// Validate Feldman commitments count
		if uint64(len(ext.FeldmanCommitments)) != threshold {
			continue
		}

		// Verify all commitments are valid G1 points
		valid := true
		for _, c := range ext.FeldmanCommitments {
			point := tleSuite.G1().Point()
			if err := point.UnmarshalBinary(c); err != nil {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}

		// Validate G2 commitments and G1/G2 pairing consistency before storing.
		// A contribution that survives ProcessProposal but fails these checks here
		// would still be stored verbatim and cause honest validators to be blamed
		// during decryption-share verification.
		if err := keeper.ValidateFeldmanCommitmentsG2(ext.FeldmanCommitmentsG2, int(threshold)); err != nil {
			ctx.Logger().With("module", "x/shield").Debug(
				"DKG contribution G2 validation failed",
				"validator", opAddr,
				"err", err,
			)
			continue
		}
		if err := keeper.ValidateFeldmanCommitmentsConsistency(ext.FeldmanCommitments, ext.FeldmanCommitmentsG2); err != nil {
			ctx.Logger().With("module", "x/shield").Debug(
				"DKG contribution G1/G2 consistency check failed",
				"validator", opAddr,
				"err", err,
			)
			continue
		}

		// Verify PoP (Schnorr signature over operator address using a₀)
		if len(ext.ContributionPop) == 0 || len(ext.FeldmanCommitments) == 0 {
			continue
		}
		if err := verifyPoP(ext.FeldmanCommitments[0], ext.ContributionPop, opAddr); err != nil {
			ctx.Logger().With("module", "x/shield").Debug(
				"DKG contribution PoP verification failed",
				"validator", opAddr,
				"err", err,
			)
			continue
		}

		// Store the contribution (including G2 commitments for pairing-based verification)
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
			sdk.NewAttribute(types.AttributeKeyContributionsCount, fmt.Sprintf("%d", dkgState.ContributionsReceived+uint64(applied))),
		))
	}

	return applied
}

// processDecryptionShares stores validator decryption shares from ACTIVE phase vote extensions
// and attempts to reconstruct the epoch decryption key when the threshold is met.
// Returns the number of new shares applied.
func processDecryptionShares(ctx sdk.Context, k keeper.Keeper, dkgState types.DKGState, shares []*types.InjectedDecryptionShare) int {
	applied := 0

	ks, found := k.GetTLEKeySetVal(ctx)
	if !found || len(ks.MasterPublicKey) == 0 {
		return 0
	}

	for _, ds := range shares {
		// Map consensus address to operator address
		opAddr, err := consAddrToOperator(ctx, k, ds.ValidatorAddress)
		if err != nil {
			continue
		}

		// Must be in the DKG participant set
		if !isValInDKG(dkgState, opAddr) {
			continue
		}

		// Must have a registered TLE public key share
		hasShare := false
		for _, vs := range ks.ValidatorShares {
			if vs.ValidatorAddress == opAddr {
				hasShare = true
				break
			}
		}
		if !hasShare {
			continue
		}

		// Skip duplicate shares for same epoch+validator
		if _, found := k.GetDecryptionShare(ctx, ds.Epoch, opAddr); found {
			continue
		}

		// Validate the share is a valid G1 point (already done in ProcessProposal,
		// but belt-and-suspenders for safety)
		point := tleSuite.G1().Point()
		if err := point.UnmarshalBinary(ds.Share); err != nil {
			continue
		}
		if point.Equal(tleSuite.G1().Point().Null()) {
			continue
		}

		// Store the decryption share
		if err := k.SetDecryptionShare(ctx, types.ShieldDecryptionShare{
			Epoch:     ds.Epoch,
			Validator: opAddr,
			Share:     ds.Share,
		}); err != nil {
			continue
		}

		applied++
	}

	if applied == 0 {
		return 0
	}

	// Check if threshold is met for any epoch that received shares —
	// group by epoch and try reconstruction.
	epochSeen := make(map[uint64]bool)
	for _, ds := range shares {
		epochSeen[ds.Epoch] = true
	}

	threshold := computeThreshold(dkgState)
	for epoch := range epochSeen {
		// Skip if decryption key already exists
		if _, found := k.GetShieldEpochDecryptionKeyVal(ctx, epoch); found {
			continue
		}

		shareCount := k.CountDecryptionShares(ctx, epoch)
		if uint64(shareCount) < threshold {
			continue
		}

		// Reconstruct epoch decryption key via Lagrange interpolation
		allShares := k.GetDecryptionSharesForEpoch(ctx, epoch)
		reconstructedKey, err := keeper.ReconstructEpochDecryptionKey(allShares, ks)
		if err != nil {
			ctx.EventManager().EmitEvent(sdk.NewEvent(
				types.EventTypeShieldDecryptionKeyFailed,
				sdk.NewAttribute(types.AttributeKeyEpoch, fmt.Sprintf("%d", epoch)),
				sdk.NewAttribute(types.AttributeKeyError, err.Error()),
			))
			continue
		}

		sdkCtx := sdk.UnwrapSDKContext(ctx)
		if err := k.SetShieldEpochDecryptionKey(ctx, types.ShieldEpochDecryptionKey{
			Epoch:                 epoch,
			DecryptionKey:         reconstructedKey,
			ReconstructedAtHeight: sdkCtx.BlockHeight(),
		}); err != nil {
			continue
		}

		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeShieldDecryptionKeyAvailable,
			sdk.NewAttribute(types.AttributeKeyEpoch, fmt.Sprintf("%d", epoch)),
			sdk.NewAttribute(types.AttributeKeySharesSubmitted, fmt.Sprintf("%d", shareCount)),
			sdk.NewAttribute(types.AttributeKeyThresholdRequired, fmt.Sprintf("%d", threshold)),
		))
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

// verifyPoP verifies a Schnorr proof of possession.
func verifyPoP(pubKeyBytes, popBytes []byte, validatorAddr string) error {
	pubPoint := tleSuite.Point()
	if err := pubPoint.UnmarshalBinary(pubKeyBytes); err != nil {
		return fmt.Errorf("invalid pub key: %w", err)
	}

	msg := []byte(validatorAddr)
	return schnorrVerify(pubPoint, msg, popBytes)
}
