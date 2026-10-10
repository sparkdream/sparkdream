package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/artifact/types"
)

func (k msgServer) CancelOutgoing(ctx context.Context, msg *types.MsgCancelOutgoing) (*types.MsgCancelOutgoingResponse, error) {
	if _, err := k.decodeAddr("signer", msg.Signer); err != nil {
		return nil, err
	}
	if err := checkBatch(len(msg.Refs), k.GetParams(ctx)); err != nil {
		return nil, err
	}
	refs := make([]U64Pair, len(msg.Refs))
	for i, r := range msg.Refs {
		refs[i] = collections.Join(r.ClassId, r.TokenId)
	}
	if err := checkNoDuplicateRefs(refs); err != nil {
		return nil, err
	}
	for _, r := range msg.Refs {
		key := collections.Join(r.ClassId, r.TokenId)
		var expiresAt int64
		authorized := false
		if pt, err := k.PendingTransfers.Get(ctx, key); err == nil {
			authorized = pt.From == msg.Signer
			expiresAt = pt.ExpiresAt
		} else if pm, err := k.PendingMints.Get(ctx, key); err == nil {
			authorized = pm.Minter == msg.Signer
			if !authorized {
				if c, err := k.getClass(ctx, pm.ClassId); err == nil && c.Owner == msg.Signer {
					authorized = true
				}
			}
			expiresAt = pm.ExpiresAt
		} else {
			return nil, errorsmod.Wrapf(types.ErrPendingNotFound, "%d/%d", r.ClassId, r.TokenId)
		}
		if !authorized {
			return nil, errorsmod.Wrapf(types.ErrNotAuthorized, "cannot cancel %d/%d", r.ClassId, r.TokenId)
		}
		outcome := OutcomeCancelled
		if expiresAt <= now(ctx) {
			outcome = OutcomeExpired
		}
		if err := k.resolvePending(ctx, r.ClassId, r.TokenId, outcome); err != nil {
			return nil, err
		}
	}
	return &types.MsgCancelOutgoingResponse{}, nil
}
