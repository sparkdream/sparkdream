package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/artifact/types"
)

func (k msgServer) RejectIncoming(ctx context.Context, msg *types.MsgRejectIncoming) (*types.MsgRejectIncomingResponse, error) {
	if _, err := k.decodeAddr("recipient", msg.Recipient); err != nil {
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
		expiresAt, _, err := k.pendingFor(ctx, msg.Recipient, r.ClassId, r.TokenId)
		if err != nil {
			return nil, err
		}
		outcome := OutcomeRejected
		if expiresAt <= now(ctx) {
			outcome = OutcomeExpired
		}
		if err := k.resolvePending(ctx, r.ClassId, r.TokenId, outcome); err != nil {
			return nil, err
		}
	}
	return &types.MsgRejectIncomingResponse{}, nil
}
