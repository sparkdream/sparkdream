package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) AddCollaborator(ctx context.Context, msg *types.MsgAddCollaborator) (*types.MsgAddCollaboratorResponse, error) {
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

	// IsOwnerOrAdmin
	isOwnerAdmin, err := k.IsOwnerOrAdmin(ctx, coll, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to check authorization")
	}
	if !isOwnerAdmin {
		return nil, types.ErrUnauthorized
	}

	// Collection must not be immutable
	if coll.Immutable {
		return nil, types.ErrCollectionImmutable
	}

	// Target address must be x/rep member
	if !k.isMember(ctx, msg.Address) {
		return nil, types.ErrNotMember
	}

	// Not owner
	if msg.Address == coll.Owner {
		return nil, types.ErrCannotCollaborateSelf
	}

	// Not already collaborator
	isCollab, _, err := k.IsCollaborator(ctx, coll.Id, msg.Address)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to check collaborator")
	}
	if isCollab {
		return nil, types.ErrAlreadyCollaborator
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// max_collaborators_per_collection check
	if coll.CollaboratorCount >= params.MaxCollaboratorsPerCollection {
		return nil, types.ErrMaxCollaborators
	}

	// Only owner can grant ADMIN role
	if msg.Role == types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN && coll.Owner != msg.Creator {
		return nil, types.ErrAdminOnlyOwner
	}

	// Create Collaborator record
	compositeKey := CollaboratorCompositeKey(coll.Id, msg.Address)
	collab := types.Collaborator{
		CollectionId: coll.Id,
		Address:      msg.Address,
		Role:         msg.Role,
		AddedAt:      blockHeight,
	}
	if err := k.Collaborator.Set(ctx, compositeKey, collab); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store collaborator")
	}

	// Set reverse index
	if err := k.CollaboratorReverse.Set(ctx, collections.Join(msg.Address, coll.Id)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set reverse index")
	}

	// Increment collaborator_count
	coll.CollaboratorCount++
	coll.UpdatedAt = blockHeight
	if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update collection")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collaborator_added",
		sdk.NewAttribute("collection_id", strconv.FormatUint(coll.Id, 10)),
		sdk.NewAttribute("address", msg.Address),
		sdk.NewAttribute("role", msg.Role.String()),
		sdk.NewAttribute("added_by", msg.Creator),
	))

	return &types.MsgAddCollaboratorResponse{}, nil
}
