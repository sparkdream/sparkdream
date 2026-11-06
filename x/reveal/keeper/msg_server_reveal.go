package keeper

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/reveal/types"
)

func (k msgServer) Reveal(ctx context.Context, msg *types.MsgReveal) (*types.MsgRevealResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Contributor); err != nil {
		return nil, types.ErrNotContributor.Wrapf("invalid contributor address: %s", err)
	}

	// Get contribution
	contrib, err := k.Contribution.Get(ctx, msg.ContributionId)
	if err != nil {
		return nil, types.ErrContributionNotFound.Wrapf("contribution %d", msg.ContributionId)
	}

	// Must be IN_PROGRESS
	if contrib.Status != types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS {
		return nil, types.ErrNotInProgress
	}

	// Only the contributor can reveal
	if !k.canActAs(ctx, msg.Contributor, contrib.Contributor) {
		return nil, types.ErrNotContributor
	}

	// Get tranche
	tranche, err := GetTranche(&contrib, msg.TrancheId)
	if err != nil {
		return nil, err
	}

	// Tranche must be BACKED
	if tranche.Status != types.TrancheStatus_TRANCHE_STATUS_BACKED {
		return nil, types.ErrTrancheNotBacked
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentEpoch := sdkCtx.BlockHeight()
	// The EndBlocker fails the tranche at RevealDeadline; a reveal in that
	// block (or later, while a backlog delays the EndBlocker) must not
	// escape the slash (REV-103).
	if tranche.RevealDeadline > 0 && currentEpoch >= tranche.RevealDeadline {
		return nil, types.ErrDeadlinePassed.Wrapf("reveal deadline %d, height %d", tranche.RevealDeadline, currentEpoch)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	if err := types.ValidateFieldLength("code_uri", msg.CodeUri, types.MaxCodeURILength); err != nil {
		return nil, err
	}
	if strings.TrimSpace(msg.CodeUri) == "" || strings.TrimSpace(msg.CommitHash) == "" {
		return nil, types.ErrEmptyReveal
	}

	// Update tranche with reveal data
	tranche.CodeUri = msg.CodeUri
	tranche.DocsUri = msg.DocsUri
	tranche.CommitHash = msg.CommitHash
	tranche.Status = types.TrancheStatus_TRANCHE_STATUS_REVEALED
	tranche.RevealedAt = currentEpoch
	tranche.VerificationDeadline = currentEpoch + params.VerificationPeriodEpochs

	// Save updated contribution
	if err := k.saveContribution(ctx, &contrib); err != nil {
		return nil, err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"tranche_revealed",
			sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contrib.Id)),
			sdk.NewAttribute("tranche_id", fmt.Sprintf("%d", msg.TrancheId)),
			sdk.NewAttribute("code_uri", msg.CodeUri),
			sdk.NewAttribute("commit_hash", msg.CommitHash),
		),
	)

	return &types.MsgRevealResponse{}, nil
}
