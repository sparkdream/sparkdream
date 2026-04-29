package keeper

import (
	"context"
	"encoding/hex"
	"fmt"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) SubmitArbiterHash(ctx context.Context, msg *types.MsgSubmitArbiterHash) (*types.MsgSubmitArbiterHashResponse, error) {
	// This handler supports two paths:
	// 1. Identified: bridge operator signs directly (msg.Creator = operator address)
	// 2. Anonymous: dispatched by x/shield after ZK proof (msg.Creator = shield module address)

	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Verify content is in CHALLENGED or DISPUTED status
	content, err := k.Content.Get(ctx, msg.ContentId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrContentNotFound, "content ID %d not found", msg.ContentId)
	}
	if content.Status != types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED &&
		content.Status != types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_DISPUTED {
		return nil, errorsmod.Wrapf(types.ErrContentNotVerified, "content status is %s, expected CHALLENGED or DISPUTED", content.Status)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	creatorBytes, _ := k.addressCodec.StringToBytes(msg.Creator)
	shieldModuleAddr := k.authKeeper.GetModuleAddress("shield")
	isShieldModule := shieldModuleAddr != nil && sdk.AccAddress(creatorBytes).Equals(shieldModuleAddr)

	// Determine the submission key. Identified callers key by their bech32
	// address so the "no double vote per operator" rule has a stable handle.
	// Anonymous callers key by a monotonic sequence: per-identity uniqueness
	// is enforced upstream by shield's per-content nullifier scope, so the
	// federation-side key only needs to keep ArbiterSubmissions entries from
	// overwriting each other (FEDERATION-S2-5).
	var submitterKey string
	if isShieldModule {
		seq, err := k.ArbiterAnonSubSeq.Next(ctx)
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to allocate anonymous arbiter sequence")
		}
		submitterKey = fmt.Sprintf("anon:%d", seq)
	} else {
		// Identified path — must be active bridge for same peer
		submitterKey = msg.Creator
		bridgeKey := collections.Join(msg.Creator, content.PeerId)
		_, err := k.BridgeOperators.Get(ctx, bridgeKey)
		if err != nil {
			return nil, errorsmod.Wrapf(types.ErrBridgeNotFound, "operator %s not registered for peer %s", msg.Creator, content.PeerId)
		}
		// Cannot arbitrate own content
		if msg.Creator == content.SubmittedBy {
			return nil, errorsmod.Wrap(types.ErrSelfArbiter, "submitting operator cannot arbitrate their own content")
		}
		// Check for duplicate submission by this operator
		arbiterKey := collections.Join(msg.ContentId, submitterKey)
		_, err = k.ArbiterSubmissions.Get(ctx, arbiterKey)
		if err == nil {
			return nil, errorsmod.Wrap(types.ErrBridgeAlreadyExists, "arbiter already submitted hash for this content")
		}
	}

	// Store submission
	submission := types.ArbiterHashSubmission{
		ContentId:   msg.ContentId,
		ContentHash: msg.ContentHash,
		SubmittedAt: blockTime,
		Operator:    msg.Creator, // shield module address for anonymous path
	}
	arbiterKey := collections.Join(msg.ContentId, submitterKey)
	if err := k.ArbiterSubmissions.Set(ctx, arbiterKey, submission); err != nil {
		return nil, err
	}

	// Increment hash count
	hashHex := hex.EncodeToString(msg.ContentHash)
	countKey := collections.Join(msg.ContentId, hashHex)
	currentCount, _ := k.ArbiterHashCounts.Get(ctx, countKey)
	newCount := currentCount + 1
	if err := k.ArbiterHashCounts.Set(ctx, countKey, newCount); err != nil {
		return nil, err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeArbiterHashSubmitted,
			sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", msg.ContentId)),
			sdk.NewAttribute("content_hash", hashHex)),
	)

	// Check if quorum reached
	if newCount >= params.ArbiterQuorum {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeArbiterQuorumReached,
				sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", msg.ContentId)),
				sdk.NewAttribute("quorum_hash", hashHex),
				sdk.NewAttribute("matching_count", fmt.Sprintf("%d", newCount))),
		)

		// Auto-resolve (TODO: implement full auto-resolution logic with slashing/refunds)
		// For now, emit the auto-resolved event and add to escalation queue
		escalationDeadline := blockTime + int64(params.ArbiterEscalationWindow.Seconds())
		if err := k.ArbiterEscalationQueue.Set(ctx, collections.Join(escalationDeadline, msg.ContentId)); err != nil {
			return nil, err
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeChallengeAutoResolved,
				sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", msg.ContentId)),
				sdk.NewAttribute("quorum_hash", hashHex)),
		)
	}

	return &types.MsgSubmitArbiterHashResponse{}, nil
}
