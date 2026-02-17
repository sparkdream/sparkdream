package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/reveal/types"
)

func (k msgServer) Propose(ctx context.Context, msg *types.MsgPropose) (*types.MsgProposeResponse, error) {
	contributorAddr, err := k.addressCodec.StringToBytes(msg.Contributor)
	if err != nil {
		return nil, types.ErrNotMember.Wrapf("invalid contributor address: %s", err)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Verify contributor is an active member with sufficient trust level
	if !k.repKeeper.IsMember(ctx, sdk.AccAddress(contributorAddr)) {
		return nil, types.ErrNotMember
	}

	trustLevel, err := k.repKeeper.GetTrustLevel(ctx, sdk.AccAddress(contributorAddr))
	if err != nil {
		return nil, err
	}
	if uint32(trustLevel) < params.MinProposerTrustLevel {
		return nil, types.ErrInsufficientTrustLevel.Wrapf("requires trust level %d, has %d", params.MinProposerTrustLevel, trustLevel)
	}

	// Check proposal cooldown: scan contributor's contributions for active cooldowns
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentEpoch := sdkCtx.BlockHeight()
	err = k.ContributionsByContributor.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](msg.Contributor),
		func(key collections.Pair[string, uint64]) (bool, error) {
			contrib, err := k.Contribution.Get(ctx, key.K2())
			if err != nil {
				return true, err
			}
			if contrib.ProposalEligibleAt > 0 && currentEpoch < contrib.ProposalEligibleAt {
				return true, types.ErrProposalCooldown.Wrapf("eligible at epoch %d, current epoch %d", contrib.ProposalEligibleAt, currentEpoch)
			}
			return false, nil
		},
	)
	if err != nil {
		return nil, err
	}

	// Validate proposal fields
	if msg.ProjectName == "" {
		return nil, types.ErrEmptyProjectName
	}
	if len(msg.Tranches) == 0 {
		return nil, types.ErrNoTranches
	}
	if uint32(len(msg.Tranches)) > params.MaxTranches {
		return nil, types.ErrTooManyTranches.Wrapf("max %d, got %d", params.MaxTranches, len(msg.Tranches))
	}
	if msg.TotalValuation.GT(params.MaxTotalValuation) {
		return nil, types.ErrValuationTooHigh.Wrapf("max %s, got %s", params.MaxTotalValuation, msg.TotalValuation)
	}

	// Validate tranches and sum thresholds
	sum := math.ZeroInt()
	for i, t := range msg.Tranches {
		if t.StakeThreshold.GT(params.MaxTrancheValuation) {
			return nil, types.ErrTrancheValuationTooHigh.Wrapf("tranche %d: max %s, got %s", i, params.MaxTrancheValuation, t.StakeThreshold)
		}
		if !t.StakeThreshold.IsPositive() {
			return nil, types.ErrTrancheValuationTooHigh.Wrapf("tranche %d: stake threshold must be positive", i)
		}
		sum = sum.Add(t.StakeThreshold)
	}
	if !sum.Equal(msg.TotalValuation) {
		return nil, types.ErrValuationMismatch.Wrapf("sum of thresholds %s != total valuation %s", sum, msg.TotalValuation)
	}

	// Calculate and lock bond
	bondAmount := params.BondRate.MulInt(msg.TotalValuation).TruncateInt()
	if err := k.repKeeper.LockDREAM(ctx, sdk.AccAddress(contributorAddr), bondAmount); err != nil {
		return nil, types.ErrInsufficientBond.Wrapf("failed to lock bond of %s DREAM: %s", bondAmount, err)
	}

	// Build tranches
	tranches := make([]types.RevealTranche, len(msg.Tranches))
	for i, td := range msg.Tranches {
		status := types.TrancheStatus_TRANCHE_STATUS_LOCKED
		tranches[i] = types.RevealTranche{
			Id:             uint32(i),
			Name:           td.Name,
			Description:    td.Description,
			Components:     td.Components,
			StakeThreshold: td.StakeThreshold,
			DreamStaked:    math.ZeroInt(),
			PreviewUri:     td.PreviewUri,
			Status:         status,
		}
	}

	// Allocate contribution ID
	contribID, err := k.ContributionSeq.Next(ctx)
	if err != nil {
		return nil, err
	}

	contrib := types.Contribution{
		Id:              contribID,
		Contributor:     msg.Contributor,
		ProjectName:     msg.ProjectName,
		Description:     msg.Description,
		Tranches:        tranches,
		CurrentTranche:  0,
		TotalValuation:  msg.TotalValuation,
		BondAmount:      bondAmount,
		BondRemaining:   bondAmount,
		InitialLicense:  msg.InitialLicense,
		FinalLicense:    msg.FinalLicense,
		Status:          types.ContributionStatus_CONTRIBUTION_STATUS_PROPOSED,
		CreatedAt:       currentEpoch,
		HoldbackAmount:  math.ZeroInt(),
	}

	// Save contribution and indexes
	if err := k.Contribution.Set(ctx, contribID, contrib); err != nil {
		return nil, err
	}
	if err := k.ContributionsByStatus.Set(ctx, collections.Join(int32(contrib.Status), contribID)); err != nil {
		return nil, err
	}
	if err := k.ContributionsByContributor.Set(ctx, collections.Join(msg.Contributor, contribID)); err != nil {
		return nil, err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"contribution_proposed",
			sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contribID)),
			sdk.NewAttribute("contributor", msg.Contributor),
			sdk.NewAttribute("project_name", msg.ProjectName),
			sdk.NewAttribute("total_valuation", msg.TotalValuation.String()),
			sdk.NewAttribute("bond_amount", bondAmount.String()),
		),
	)

	return &types.MsgProposeResponse{ContributionId: contribID}, nil
}
