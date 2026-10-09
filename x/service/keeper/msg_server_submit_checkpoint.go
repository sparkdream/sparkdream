package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// checkpointRootLen is the size of a checkpoint root (a sha256 merkle root).
const checkpointRootLen = 32

// SubmitCheckpoint records how far the signing operator's off-chain work has
// progressed under a service type (docs/content-scanning.md §8.2).
func (k msgServer) SubmitCheckpoint(ctx context.Context, msg *types.MsgSubmitCheckpoint) (*types.MsgSubmitCheckpointResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	opBytes, err := k.addrBytes(msg.Operator)
	if err != nil {
		return nil, types.ErrInvalidSigner.Wrap("invalid operator address")
	}
	op, ok := k.GetOperator(ctx, opBytes, msg.ServiceType)
	if !ok {
		return nil, types.ErrOperatorNotFound.Wrapf("%s / %s", msg.Operator, msg.ServiceType)
	}
	if !livenessTracked(op.Status) {
		return nil, types.ErrOperatorNotActive.Wrapf("status %s", op.Status)
	}
	if len(msg.Root) != checkpointRootLen {
		return nil, types.ErrInvalidCheckpoint.Wrapf("root must be %d bytes, got %d", checkpointRootLen, len(msg.Root))
	}
	if msg.Height <= 0 || msg.Height > currentHeight {
		return nil, types.ErrInvalidCheckpoint.Wrapf("height %d must be in (0, %d]", msg.Height, currentHeight)
	}
	if prev, ok := k.GetCheckpoint(ctx, opBytes, msg.ServiceType); ok && msg.Height <= prev.Height {
		return nil, types.ErrInvalidCheckpoint.Wrapf("height %d must exceed previous checkpoint %d", msg.Height, prev.Height)
	}

	cp := types.Checkpoint{
		Operator:    msg.Operator,
		ServiceType: msg.ServiceType,
		Height:      msg.Height,
		Root:        msg.Root,
		SubmittedAt: currentHeight,
	}
	if err := k.Checkpoints.Set(ctx, collections.Join(msg.ServiceType, opBytes), cp); err != nil {
		return nil, err
	}

	cfg, err := k.resolveServiceTypeConfig(ctx, msg.ServiceType)
	if err != nil {
		return nil, err
	}
	if err := k.scheduleLiveness(ctx, op, opBytes, cfg.CheckpointMaxLagBlocks); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(types.NewCheckpointSubmittedEvent(msg.Operator, msg.ServiceType, msg.Height, msg.Root))
	return &types.MsgSubmitCheckpointResponse{}, nil
}
