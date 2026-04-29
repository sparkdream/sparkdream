package abci

import (
	"fmt"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

// PrepareProposalHandler returns a PrepareProposalHandler that aggregates
// DKG vote extensions from the previous block's commit into an InjectedDKGData
// pseudo-transaction at position 0 of the block.
//
// If no DKG is active or no extensions are present, the block is unmodified.
func PrepareProposalHandler(k keeper.Keeper) sdk.PrepareProposalHandler {
	return func(ctx sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
		txs := req.Txs

		// Check if DKG is in an active phase that needs vote extension processing
		dkgState, found := k.GetDKGStateVal(ctx)
		if !found || dkgState.Phase == types.DKGPhase_DKG_PHASE_INACTIVE {
			return &abci.ResponsePrepareProposal{Txs: txs}, nil
		}

		// Collect DKG vote extensions from the previous block's commit
		var extensions []*types.ValidatorDKGExtension
		var decryptionShares []*types.InjectedDecryptionShare

		for _, vote := range req.LocalLastCommit.Votes {
			if len(vote.VoteExtension) == 0 {
				continue
			}

			var ext types.DKGVoteExtension
			if err := ext.Unmarshal(vote.VoteExtension); err != nil {
				continue // Skip malformed extensions
			}

			// Must match current DKG round
			if ext.Round != dkgState.Round {
				continue
			}

			switch ext.Phase {
			case types.DKGPhase_DKG_PHASE_REGISTERING, types.DKGPhase_DKG_PHASE_CONTRIBUTING:
				// Only include registration/contribution extensions if phase matches
				if ext.Phase != dkgState.Phase {
					continue
				}
				extensions = append(extensions, &types.ValidatorDKGExtension{
					ValidatorAddress: vote.Validator.Address,
					Extension:        ext,
				})

			case types.DKGPhase_DKG_PHASE_ACTIVE:
				// Collect decryption shares during ACTIVE phase
				if dkgState.Phase != types.DKGPhase_DKG_PHASE_ACTIVE {
					continue
				}
				if len(ext.DecryptionShare) == 0 {
					continue
				}
				decryptionShares = append(decryptionShares, &types.InjectedDecryptionShare{
					ValidatorAddress: vote.Validator.Address,
					Epoch:            ext.DecryptionEpoch,
					Share:            ext.DecryptionShare,
				})
			}
		}

		// No DKG extensions or decryption shares to inject
		if len(extensions) == 0 && len(decryptionShares) == 0 {
			return &abci.ResponsePrepareProposal{Txs: txs}, nil
		}

		// Build the injection
		injected := &types.InjectedDKGData{
			Round:            dkgState.Round,
			Phase:            dkgState.Phase,
			Extensions:       extensions,
			DecryptionShares: decryptionShares,
		}

		injectionBz, err := EncodeDKGInjection(injected)
		if err != nil {
			ctx.Logger().With("module", "x/shield").Error(
				"failed to encode DKG injection", "err", err)
			return &abci.ResponsePrepareProposal{Txs: txs}, nil
		}

		// Check size: injection must fit within MaxTxBytes
		injectionSize := int64(len(injectionBz))
		if injectionSize > req.MaxTxBytes {
			ctx.Logger().With("module", "x/shield").Warn(
				"DKG injection exceeds MaxTxBytes, skipping",
				"injection_size", injectionSize,
				"max_tx_bytes", req.MaxTxBytes,
			)
			return &abci.ResponsePrepareProposal{Txs: txs}, nil
		}

		// Prepend injection as tx[0], trimming normal txs if needed
		result := make([][]byte, 0, 1+len(txs))
		result = append(result, injectionBz)

		remainingBytes := req.MaxTxBytes - injectionSize
		for _, tx := range txs {
			txSize := int64(len(tx))
			if remainingBytes < txSize {
				break // Can't fit more txs
			}
			result = append(result, tx)
			remainingBytes -= txSize
		}

		ctx.Logger().With("module", "x/shield").Info(
			"injected DKG data into proposal",
			"round", dkgState.Round,
			"phase", dkgState.Phase.String(),
			"extensions_count", len(extensions),
		)

		return &abci.ResponsePrepareProposal{Txs: result}, nil
	}
}

// ProcessProposalHandler returns a ProcessProposalHandler that validates
// the DKG injection pseudo-transaction at position 0.
//
// If tx[0] has the DKG magic prefix, it's validated for correctness.
// Invalid injections cause proposal rejection. Remaining txs are accepted.
func ProcessProposalHandler(k keeper.Keeper) sdk.ProcessProposalHandler {
	return func(ctx sdk.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
		if len(req.Txs) == 0 {
			return &abci.ResponseProcessProposal{
				Status: abci.ResponseProcessProposal_ACCEPT,
			}, nil
		}

		// Check if tx[0] is a DKG injection
		if !HasDKGInjectionPrefix(req.Txs[0]) {
			// No DKG injection — accept as normal
			return &abci.ResponseProcessProposal{
				Status: abci.ResponseProcessProposal_ACCEPT,
			}, nil
		}

		// Validate the DKG injection
		injected, err := DecodeDKGInjection(req.Txs[0])
		if err != nil || injected == nil {
			ctx.Logger().With("module", "x/shield").Warn(
				"rejecting proposal: invalid DKG injection encoding")
			return &abci.ResponseProcessProposal{
				Status: abci.ResponseProcessProposal_REJECT,
			}, nil
		}

		if err := validateInjectedDKGData(ctx, k, injected); err != nil {
			ctx.Logger().With("module", "x/shield").Warn(
				"rejecting proposal: invalid DKG injection",
				"err", err,
			)
			return &abci.ResponseProcessProposal{
				Status: abci.ResponseProcessProposal_REJECT,
			}, nil
		}

		return &abci.ResponseProcessProposal{
			Status: abci.ResponseProcessProposal_ACCEPT,
		}, nil
	}
}

// validateInjectedDKGData validates the aggregated DKG data from the proposer.
func validateInjectedDKGData(ctx sdk.Context, k keeper.Keeper, data *types.InjectedDKGData) error {
	dkgState, found := k.GetDKGStateVal(ctx)
	if !found {
		return fmt.Errorf("no DKG state")
	}

	if data.Round != dkgState.Round {
		return fmt.Errorf("round mismatch: injected=%d, state=%d", data.Round, dkgState.Round)
	}

	if data.Phase != dkgState.Phase {
		return fmt.Errorf("phase mismatch: injected=%s, state=%s", data.Phase, dkgState.Phase)
	}

	// Validate each extension is well-formed
	for i, ve := range data.Extensions {
		if len(ve.ValidatorAddress) == 0 {
			return fmt.Errorf("extension %d: empty validator address", i)
		}

		ext := &ve.Extension
		if ext.Round != data.Round || ext.Phase != data.Phase {
			return fmt.Errorf("extension %d: round/phase mismatch", i)
		}

		switch ext.Phase {
		case types.DKGPhase_DKG_PHASE_REGISTERING:
			if len(ext.RegistrationPubKey) == 0 {
				return fmt.Errorf("extension %d: empty pub key", i)
			}
			// Validate G1 point
			point := tleSuite.G1().Point()
			if err := point.UnmarshalBinary(ext.RegistrationPubKey); err != nil {
				return fmt.Errorf("extension %d: invalid pub key: %w", i, err)
			}

		case types.DKGPhase_DKG_PHASE_CONTRIBUTING:
			threshold := computeThreshold(dkgState)
			if uint64(len(ext.FeldmanCommitments)) != threshold {
				return fmt.Errorf("extension %d: expected %d commitments, got %d",
					i, threshold, len(ext.FeldmanCommitments))
			}
			// Validate each commitment is a valid G1 point
			for j, c := range ext.FeldmanCommitments {
				point := tleSuite.G1().Point()
				if err := point.UnmarshalBinary(c); err != nil {
					return fmt.Errorf("extension %d commitment %d: invalid: %w", i, j, err)
				}
			}
			// G2 well-formedness + G1/G2 pairing consistency. Without these
			// the proposer can include a Byzantine contribution whose G2
			// commitments don't match its G1 commitments, deferring the
			// failure to decryption-share verification (which then blames
			// honest validators).
			if err := keeper.ValidateFeldmanCommitmentsG2(ext.FeldmanCommitmentsG2, int(threshold)); err != nil {
				return fmt.Errorf("extension %d: invalid G2 commitments: %w", i, err)
			}
			if err := keeper.ValidateFeldmanCommitmentsConsistency(ext.FeldmanCommitments, ext.FeldmanCommitmentsG2); err != nil {
				return fmt.Errorf("extension %d: G1/G2 commitment mismatch: %w", i, err)
			}
		}
	}

	// Validate decryption shares
	for i, ds := range data.DecryptionShares {
		if len(ds.ValidatorAddress) == 0 {
			return fmt.Errorf("decryption share %d: empty validator address", i)
		}
		if len(ds.Share) == 0 {
			return fmt.Errorf("decryption share %d: empty share", i)
		}
		// Validate it's a valid BN256 G1 point
		point := tleSuite.G1().Point()
		if err := point.UnmarshalBinary(ds.Share); err != nil {
			return fmt.Errorf("decryption share %d: invalid G1 point: %w", i, err)
		}
		if point.Equal(tleSuite.G1().Point().Null()) {
			return fmt.Errorf("decryption share %d: identity element", i)
		}
	}

	return nil
}
