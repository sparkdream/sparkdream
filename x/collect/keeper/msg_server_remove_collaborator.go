package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) RemoveCollaborator(ctx context.Context, msg *types.MsgRemoveCollaborator) (*types.MsgRemoveCollaboratorResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	if _, err := k.addressCodec.StringToBytes(msg.Address); err != nil {
		return nil, errorsmod.Wrap(err, "invalid collaborator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	// Get collection
	coll, err := k.Collection.Get(ctx, msg.CollectionId)
	if err != nil {
		return nil, types.ErrCollectionNotFound
	}

	isSelfRemoval := msg.Creator == msg.Address

	// Check immutability (exception for self-removal)
	if coll.Immutable && !isSelfRemoval {
		return nil, types.ErrCollectionImmutable
	}

	// The collaborator to remove must exist
	compositeKey := CollaboratorCompositeKey(coll.Id, msg.Address)
	targetCollab, err := k.Collaborator.Get(ctx, compositeKey)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized, "address %s is not a collaborator", msg.Address)
	}

	if isSelfRemoval {
		// Self-removal always allowed
	} else if coll.Owner == msg.Creator {
		// Owner can remove anyone
	} else {
		// ADMIN can remove non-ADMINs
		isCollab, creatorRole, err := k.IsCollaborator(ctx, coll.Id, msg.Creator)
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to check creator role")
		}
		if !isCollab || creatorRole != types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN {
			return nil, types.ErrUnauthorized
		}
		// ADMIN can't remove other ADMINs
		if targetCollab.Role == types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN {
			return nil, types.ErrAdminRemoveAdmin
		}
	}

	// Remove Collaborator record
	if err := k.Collaborator.Remove(ctx, compositeKey); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove collaborator")
	}

	// Remove reverse index
	k.CollaboratorReverse.Remove(ctx, collections.Join(msg.Address, coll.Id)) //nolint:errcheck

	// Decrement collaborator_count
	if coll.CollaboratorCount > 0 {
		coll.CollaboratorCount--
	}
	coll.UpdatedAt = blockHeight
	if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update collection")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collaborator_removed",
		sdk.NewAttribute("collection_id", strconv.FormatUint(coll.Id, 10)),
		sdk.NewAttribute("address", msg.Address),
		sdk.NewAttribute("removed_by", msg.Creator),
	))

	return &types.MsgRemoveCollaboratorResponse{}, nil
}
