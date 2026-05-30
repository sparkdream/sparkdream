package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) AddCollaborator(ctx context.Context, msg *types.MsgAddCollaborator) (*types.MsgAddCollaboratorResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
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

	// Non-member branch: the inviter locks DREAM as accountability for the
	// non-member's behavior. Stake is refunded on RemoveCollaborator when the
	// collection is ACTIVE; a fraction is burned when the collection is
	// HIDDEN at exit (see helpers.releaseOrSlashCollabStake).
	var inviter string
	dreamStake := math.ZeroInt()
	if !k.isMember(ctx, msg.Address) {
		// Non-members can only hold EDITOR — preventing transitive non-member
		// invites and capping the inviter's per-collection exposure.
		if msg.Role != types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR {
			return nil, types.ErrNonMemberAdminRole
		}
		// Inviter must meet min_sponsor_trust_level (same gate as endorsement /
		// sponsorship — vouching for outsiders).
		if !k.meetsMinTrustLevel(ctx, msg.Creator, params.MinSponsorTrustLevel) {
			return nil, errorsmod.Wrapf(types.ErrInviterTrustLevelTooLow,
				"inviter must be at or above %s", params.MinSponsorTrustLevel)
		}
		// Sub-cap on non-member slots per collection.
		if coll.NonMemberCollaboratorCount >= params.MaxNonMemberCollaboratorsPerCollection {
			return nil, types.ErrMaxNonMemberCollaborators
		}
		// Lock stake from the inviter.
		if err := k.repKeeper.LockDREAM(ctx, creatorAddr, params.NonMemberCollabDreamStake); err != nil {
			return nil, errorsmod.Wrap(err, "failed to lock inviter DREAM stake")
		}
		inviter = msg.Creator
		dreamStake = params.NonMemberCollabDreamStake
	}

	// Create Collaborator record
	compositeKey := CollaboratorCompositeKey(coll.Id, msg.Address)
	collab := types.Collaborator{
		CollectionId: coll.Id,
		Address:      msg.Address,
		Role:         msg.Role,
		AddedAt:      blockHeight,
		Inviter:      inviter,
		DreamStake:   dreamStake,
	}
	if err := k.Collaborator.Set(ctx, compositeKey, collab); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store collaborator")
	}

	// Set reverse index
	if err := k.CollaboratorReverse.Set(ctx, collections.Join(msg.Address, coll.Id)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set reverse index")
	}

	// Increment counters
	coll.CollaboratorCount++
	if dreamStake.IsPositive() {
		coll.NonMemberCollaboratorCount++
	}
	coll.UpdatedAt = blockHeight
	if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update collection")
	}

	attrs := []sdk.Attribute{
		sdk.NewAttribute("collection_id", strconv.FormatUint(coll.Id, 10)),
		sdk.NewAttribute("address", msg.Address),
		sdk.NewAttribute("role", msg.Role.String()),
		sdk.NewAttribute("added_by", msg.Creator),
	}
	if dreamStake.IsPositive() {
		attrs = append(attrs,
			sdk.NewAttribute("inviter", inviter),
			sdk.NewAttribute("dream_stake", dreamStake.String()),
		)
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collaborator_added", attrs...))

	return &types.MsgAddCollaboratorResponse{}, nil
}
