package abci

import (
	"fmt"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

// injectionBudgetDivisor bounds the DKG injection to this fraction of
// MaxTxBytes. Extensions that do not fit are left out; their validators keep
// re-sending them in every vote until they are stored, so a large validator
// set's ceremony spreads over several blocks instead of never landing.
const injectionBudgetDivisor = 2

// PrepareProposalHandler returns a PrepareProposalHandler that aggregates
// DKG vote extensions from the previous block's commit into an InjectedDKGData
// pseudo-transaction at position 0 of the block, then lets `next` (the SDK's
// default handler: byte and block-gas limits over the mempool) fill the rest.
//
// Each extension is checked against the phase's acceptance rule with the
// state ProcessProposal will use, so an honest proposer never injects
// something that would get its block rejected. If no DKG is active or no
// extension qualifies, the block is whatever `next` returns.
func PrepareProposalHandler(k keeper.Keeper, next sdk.PrepareProposalHandler) sdk.PrepareProposalHandler {
	return func(ctx sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
		dkgState, found := k.GetDKGStateVal(ctx)
		if !found || dkgState.Phase == types.DKGPhase_DKG_PHASE_INACTIVE || ceremonyFrozen(ctx, k, dkgState.Phase) {
			return next(ctx, req)
		}

		budget := req.MaxTxBytes / injectionBudgetDivisor
		injected := &types.InjectedDKGData{Round: dkgState.Round, Phase: dkgState.Phase}
		size := int64(len(DKGInjectionPrefix)) + 16 // prefix + round/phase varints, generous
		skipped := 0

		for _, vote := range req.LocalLastCommit.Votes {
			if len(vote.VoteExtension) == 0 {
				continue
			}
			var ext types.DKGVoteExtension
			if err := ext.Unmarshal(vote.VoteExtension); err != nil {
				continue // Skip malformed extensions
			}
			opAddr, err := consAddrToOperator(ctx, k, vote.Validator.Address)
			if err != nil {
				continue
			}
			if err := validateExtension(ctx, k, &ext, dkgState, opAddr); err != nil {
				ctx.Logger().With("module", "x/shield").Debug("DKG vote extension left out of proposal", "validator", opAddr, "err", err)
				continue
			}

			var item interface{ Size() int }
			if ext.Phase == types.DKGPhase_DKG_PHASE_ACTIVE {
				share := &types.InjectedDecryptionShare{
					ValidatorAddress: vote.Validator.Address,
					Epoch:            ext.DecryptionEpoch,
					Share:            ext.DecryptionShare,
				}
				if size+int64(share.Size())+8 > budget {
					skipped++
					continue
				}
				item = share
				injected.DecryptionShares = append(injected.DecryptionShares, share)
			} else {
				ve := &types.ValidatorDKGExtension{ValidatorAddress: vote.Validator.Address, Extension: ext}
				if size+int64(ve.Size())+8 > budget {
					skipped++
					continue
				}
				item = ve
				injected.Extensions = append(injected.Extensions, ve)
			}
			size += int64(item.Size()) + 8
		}

		if skipped > 0 {
			ctx.Logger().With("module", "x/shield").Warn(
				"DKG injection budget reached; extensions deferred to a later block",
				"round", dkgState.Round, "phase", dkgState.Phase.String(),
				"included", len(injected.Extensions)+len(injected.DecryptionShares), "deferred", skipped,
			)
		}
		if len(injected.Extensions) == 0 && len(injected.DecryptionShares) == 0 {
			return next(ctx, req)
		}

		injectionBz, err := EncodeDKGInjection(injected)
		if err != nil {
			ctx.Logger().With("module", "x/shield").Error("failed to encode DKG injection", "err", err)
			return next(ctx, req)
		}

		// Let the default handler select the block's txs within what is left.
		rest := *req
		rest.MaxTxBytes = req.MaxTxBytes - int64(len(injectionBz))
		resp, err := next(ctx, &rest)
		if err != nil {
			return nil, err
		}
		result := make([][]byte, 0, 1+len(resp.Txs))
		result = append(result, injectionBz)
		result = append(result, resp.Txs...)

		ctx.Logger().With("module", "x/shield").Info(
			"injected DKG data into proposal",
			"round", dkgState.Round,
			"phase", dkgState.Phase.String(),
			"extensions_count", len(injected.Extensions),
			"shares_count", len(injected.DecryptionShares),
		)

		return &abci.ResponsePrepareProposal{Txs: result}, nil
	}
}

// ProcessProposalHandler returns a ProcessProposalHandler that validates
// the DKG injection pseudo-transaction at position 0, then hands the
// remaining txs to `next` (the SDK's default handler).
//
// If tx[0] has the DKG magic prefix, it's validated for correctness.
// Invalid injections cause proposal rejection.
func ProcessProposalHandler(k keeper.Keeper, next sdk.ProcessProposalHandler) sdk.ProcessProposalHandler {
	return func(ctx sdk.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
		if len(req.Txs) == 0 || !HasDKGInjectionPrefix(req.Txs[0]) {
			return next(ctx, req)
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

		rest := *req
		rest.Txs = req.Txs[1:]
		return next(ctx, &rest)
	}
}

// validateInjectedDKGData validates the aggregated DKG data from the proposer
// with the same rules VerifyVoteExtension applied to each extension.
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

	if ceremonyFrozen(ctx, k, dkgState.Phase) {
		return fmt.Errorf("shield disabled during DKG phase %s", dkgState.Phase)
	}

	for i, ve := range data.Extensions {
		if ve == nil || len(ve.ValidatorAddress) == 0 {
			return fmt.Errorf("extension %d: empty validator address", i)
		}
		opAddr, err := consAddrToOperator(ctx, k, ve.ValidatorAddress)
		if err != nil {
			return fmt.Errorf("extension %d: %w", i, err)
		}
		if err := validateExtension(ctx, k, &ve.Extension, dkgState, opAddr); err != nil {
			return fmt.Errorf("extension %d from %s: %w", i, opAddr, err)
		}
	}

	// Validate decryption shares with the same rule VerifyVoteExtension
	// applied: an honest proposer never includes one that fails it.
	for i, ds := range data.DecryptionShares {
		if ds == nil || len(ds.ValidatorAddress) == 0 {
			return fmt.Errorf("decryption share %d: empty validator address", i)
		}
		if len(ds.Share) == 0 {
			return fmt.Errorf("decryption share %d: empty share", i)
		}
		opAddr, err := consAddrToOperator(ctx, k, ds.ValidatorAddress)
		if err != nil {
			return fmt.Errorf("decryption share %d: %w", i, err)
		}
		if err := k.VerifyValidatorDecryptionShare(ctx, opAddr, ds.Epoch, ds.Share); err != nil {
			return fmt.Errorf("decryption share %d from %s: %w", i, opAddr, err)
		}
	}

	return nil
}
