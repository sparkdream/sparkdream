package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/artifact/types"
)

// pendingFor returns the pending item addressed to recipient, its expiry,
// and its metadata hash.
func (k Keeper) pendingFor(ctx context.Context, recipient string, classID, tokenID uint64) (int64, string, error) {
	key := collections.Join(classID, tokenID)
	if pt, err := k.PendingTransfers.Get(ctx, key); err == nil && pt.To == recipient {
		t, err := k.getToken(ctx, classID, tokenID)
		if err != nil {
			return 0, "", err
		}
		return pt.ExpiresAt, types.MetadataHash(t.Metadata), nil
	}
	if pm, err := k.PendingMints.Get(ctx, key); err == nil && pm.To == recipient {
		return pm.ExpiresAt, types.MetadataHash(pm.Metadata), nil
	}
	return 0, "", errorsmod.Wrapf(types.ErrPendingNotFound, "%d/%d", classID, tokenID)
}

func (k msgServer) AcceptIncoming(ctx context.Context, msg *types.MsgAcceptIncoming) (*types.MsgAcceptIncomingResponse, error) {
	if _, err := k.decodeAddr("recipient", msg.Recipient); err != nil {
		return nil, err
	}
	if err := checkBatch(len(msg.Entries), k.GetParams(ctx)); err != nil {
		return nil, err
	}
	refs := make([]U64Pair, len(msg.Entries))
	for i, e := range msg.Entries {
		refs[i] = collections.Join(e.ClassId, e.TokenId)
	}
	if err := checkNoDuplicateRefs(refs); err != nil {
		return nil, err
	}
	for _, e := range msg.Entries {
		expiresAt, hash, err := k.pendingFor(ctx, msg.Recipient, e.ClassId, e.TokenId)
		if err != nil {
			return nil, err
		}
		if expiresAt <= now(ctx) {
			return nil, errorsmod.Wrapf(types.ErrPendingExpired, "%d/%d", e.ClassId, e.TokenId)
		}
		if e.ExpectedMetadataHash != "" && e.ExpectedMetadataHash != hash {
			return nil, errorsmod.Wrapf(types.ErrMetadataChanged, "%d/%d", e.ClassId, e.TokenId)
		}
		if err := k.resolvePending(ctx, e.ClassId, e.TokenId, OutcomeAccepted); err != nil {
			return nil, err
		}
	}
	return &types.MsgAcceptIncomingResponse{}, nil
}
