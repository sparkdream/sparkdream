package keeper

import (
	"context"

	"sparkdream/x/service/types"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// OperatorReputationSnapshot returns the aggregate and anti-gaming-
// capped bond-block totals for all live operator records owned by the
// given address. Settles lazily in-memory at query time per §6.6 (no
// state write).
//
//   - total_bond_blocks: sum of total_lifetime_bond_blocks across all
//     live records (raw accrual; pre-cap).
//   - effective_bond_blocks: max single-record bond-blocks for the
//     address — the value that would actually be granted at unbond
//     claim under the anti-gaming cap.
func (q queryServer) OperatorReputationSnapshot(ctx context.Context, req *types.QueryOperatorReputationSnapshotRequest) (*types.QueryOperatorReputationSnapshotResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	addrBytes, err := q.k.addrBytes(req.Address)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	// Iterate all live records owned by addrBytes (prefix on K1 of the
	// (addr, service_type) primary key).
	rng := collections.NewPrefixedPairRange[[]byte, string](addrBytes)
	iter, err := q.k.Operators.Iterate(ctx, rng)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer iter.Close()

	total := sdkmath.ZeroInt()
	effective := sdkmath.ZeroInt()

	for ; iter.Valid(); iter.Next() {
		op, err := iter.Value()
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		// In-memory settle so the snapshot reflects accrual up to the
		// current block (no write). settleBondBlocks only accrues for
		// ACTIVE — UNDERFUNDED / UNBONDING records report their last-
		// settled total unchanged.
		q.k.settleBondBlocks(&op, currentHeight)

		if !op.TotalLifetimeBondBlocks.IsNil() {
			total = total.Add(op.TotalLifetimeBondBlocks)
			if op.TotalLifetimeBondBlocks.GT(effective) {
				effective = op.TotalLifetimeBondBlocks
			}
		}
	}

	return &types.QueryOperatorReputationSnapshotResponse{
		TotalBondBlocks:     total,
		EffectiveBondBlocks: effective,
	}, nil
}
