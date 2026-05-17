package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// UpdateMetadata mutates an operator's opaque metadata blob. Operator-
// signed; no state-machine impact (metadata doesn't affect bond or
// reputation accrual). See §5.1 + §3.4 for the per-status table —
// metadata updates are allowed in ACTIVE, UNDERFUNDED, and UNBONDING
// (but not terminal, which is enforced by the operator-not-found check
// since archived records aren't in the live store).
func (k msgServer) UpdateMetadata(ctx context.Context, msg *types.MsgUpdateMetadata) (*types.MsgUpdateMetadataResponse, error) {
	opBytes, err := k.addrBytes(msg.Operator)
	if err != nil {
		return nil, types.ErrInvalidSigner.Wrap("invalid operator address")
	}

	op, exists := k.GetOperator(ctx, opBytes, msg.ServiceType)
	if !exists {
		return nil, types.ErrOperatorNotFound.Wrapf("%s / %s", msg.Operator, msg.ServiceType)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if uint32(len(msg.NewMetadata)) > params.MaxMetadataBytes {
		return nil, types.ErrInvalidMetadataSize.Wrapf("%d > %d", len(msg.NewMetadata), params.MaxMetadataBytes)
	}

	op.Metadata = msg.NewMetadata
	if err := k.PutOperator(ctx, op); err != nil {
		return nil, err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		types.NewMetadataUpdatedEvent(msg.Operator, msg.ServiceType),
	)

	return &types.MsgUpdateMetadataResponse{}, nil
}
