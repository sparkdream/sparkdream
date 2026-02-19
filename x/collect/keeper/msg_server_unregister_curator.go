package keeper

import (
	"context"

	"sparkdream/x/collect/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) UnregisterCurator(ctx context.Context, msg *types.MsgUnregisterCurator) (*types.MsgUnregisterCuratorResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Must be registered active curator
	curator, err := k.Curator.Get(ctx, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrNotCurator, msg.Creator)
	}
	if !curator.Active {
		return nil, errorsmod.Wrap(types.ErrNotCurator, "curator is not active")
	}

	// pending_challenges == 0
	if curator.PendingChallenges > 0 {
		return nil, errorsmod.Wrapf(types.ErrCuratorHasPendingChallenges, "%d pending challenges", curator.PendingChallenges)
	}

	// Unlock DREAM bond via repKeeper.UnlockDREAM
	if err := k.repKeeper.UnlockDREAM(ctx, creatorAddr, curator.BondAmount); err != nil {
		return nil, errorsmod.Wrap(err, "failed to unlock DREAM bond")
	}

	// Delete Curator record
	if err := k.Curator.Remove(ctx, msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove curator")
	}

	// Emit curator_unregistered event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("curator_unregistered",
		sdk.NewAttribute("curator", msg.Creator),
		sdk.NewAttribute("bond_refunded", curator.BondAmount.String()),
	))

	return &types.MsgUnregisterCuratorResponse{}, nil
}
