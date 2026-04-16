package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) EndorseCollection(ctx context.Context, msg *types.MsgEndorseCollection) (*types.MsgEndorseCollectionResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	// Creator must be an active x/rep member
	if !k.isMember(ctx, msg.Creator) {
		return nil, types.ErrNotMember
	}

	// Get params (needed for trust level check and later)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Endorser must meet min_sponsor_trust_level (same gate as sponsorship)
	if !k.meetsMinTrustLevel(ctx, msg.Creator, params.MinSponsorTrustLevel) {
		return nil, errorsmod.Wrapf(types.ErrTrustLevelTooLow, "endorser must be at or above %s", params.MinSponsorTrustLevel)
	}

	// Get collection
	coll, err := k.Collection.Get(ctx, msg.CollectionId)
	if err != nil {
		return nil, types.ErrCollectionNotFound
	}

	// Collection must have status=PENDING
	if coll.Status != types.CollectionStatus_COLLECTION_STATUS_PENDING {
		return nil, types.ErrCollectionNotPending
	}

	// Collection owner must NOT be x/rep member
	if k.isMember(ctx, coll.Owner) {
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "collection owner is already a member")
	}

	// seeking_endorsement must be true
	if !coll.SeekingEndorsement {
		return nil, types.ErrNotSeekingEndorsement
	}

	// Creator must NOT be collection owner
	if coll.Owner == msg.Creator {
		return nil, types.ErrCannotEndorseSelf
	}

	// Collection must not already be endorsed
	if coll.EndorsedBy != "" {
		return nil, types.ErrAlreadyEndorsed
	}

	// Lock endorsement_dream_stake DREAM from creator
	if err := k.repKeeper.LockDREAM(ctx, creatorAddr, params.EndorsementDreamStake); err != nil {
		return nil, errorsmod.Wrap(err, "failed to lock DREAM stake")
	}

	// Create Endorsement record
	stakeReleaseAt := blockHeight + params.EndorsementStakeDuration
	endorsement := types.Endorsement{
		CollectionId:   msg.CollectionId,
		Endorser:       msg.Creator,
		DreamStake:     params.EndorsementDreamStake,
		EndorsedAt:     blockHeight,
		StakeReleaseAt: stakeReleaseAt,
		StakeReleased:  false,
	}
	if err := k.Endorsement.Set(ctx, msg.CollectionId, endorsement); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store endorsement")
	}

	// Update collection: status=ACTIVE, endorsed_by=creator, immutable=true
	coll.Status = types.CollectionStatus_COLLECTION_STATUS_ACTIVE
	coll.EndorsedBy = msg.Creator
	coll.Immutable = true
	if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update collection")
	}

	// Split endorsement_creation_fee: endorser_share to creator, remainder burned from module account
	endorserShareAmt := params.EndorsementFeeEndorserShare.MulInt(params.EndorsementCreationFee).TruncateInt()
	burnAmt := params.EndorsementCreationFee.Sub(endorserShareAmt)

	// Pay endorser share from module account
	if endorserShareAmt.IsPositive() {
		if err := k.RefundSPARK(ctx, creatorAddr, endorserShareAmt); err != nil {
			return nil, errorsmod.Wrap(err, "failed to pay endorser share")
		}
	}

	// Burn remainder from module account
	if burnAmt.IsPositive() {
		if err := k.BurnSPARK(ctx, burnAmt); err != nil {
			return nil, errorsmod.Wrap(err, "failed to burn endorsement fee remainder")
		}
	}

	// Set EndorsementStakeExpiry index
	if err := k.EndorsementStakeExpiry.Set(ctx, collections.Join(stakeReleaseAt, msg.CollectionId)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set endorsement stake expiry")
	}

	// Remove from EndorsementPending index.
	// Compute the key directly using CreatedAt + EndorsementExpiryBlocks instead of walking.
	expiryBlock := coll.CreatedAt + params.EndorsementExpiryBlocks
	k.EndorsementPending.Remove(ctx, collections.Join(expiryBlock, msg.CollectionId)) //nolint:errcheck

	// Update CollectionsByStatus index: remove PENDING, add ACTIVE
	k.CollectionsByStatus.Remove(ctx, collections.Join(int32(types.CollectionStatus_COLLECTION_STATUS_PENDING), coll.Id)) //nolint:errcheck
	if err := k.CollectionsByStatus.Set(ctx, collections.Join(int32(types.CollectionStatus_COLLECTION_STATUS_ACTIVE), coll.Id)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update status index")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collection_endorsed",
		sdk.NewAttribute("collection_id", strconv.FormatUint(msg.CollectionId, 10)),
		sdk.NewAttribute("endorser", msg.Creator),
		sdk.NewAttribute("owner", coll.Owner),
		sdk.NewAttribute("dream_stake", params.EndorsementDreamStake.String()),
		sdk.NewAttribute("stake_release_at", strconv.FormatInt(stakeReleaseAt, 10)),
		sdk.NewAttribute("endorser_share", endorserShareAmt.String()),
		sdk.NewAttribute("burned", burnAmt.String()),
	))

	return &types.MsgEndorseCollectionResponse{}, nil
}
