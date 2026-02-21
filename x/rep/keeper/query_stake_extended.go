package keeper

import (
	"context"
	"errors"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// PendingStakeRewards queries pending rewards for a stake
func (q queryServer) PendingStakeRewards(ctx context.Context, req *types.QueryPendingStakeRewardsRequest) (*types.QueryPendingStakeRewardsResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, "request cannot be empty")
	}

	stake, err := q.k.GetStake(ctx, req.StakeId)
	if err != nil {
		return nil, err
	}

	pendingRewards, err := q.k.GetPendingStakingRewards(ctx, stake)
	if err != nil {
		return nil, err
	}

	return &types.QueryPendingStakeRewardsResponse{
		PendingRewards: pendingRewards,
		StakeAmount:    stake.Amount,
		TargetType:     stake.TargetType,
	}, nil
}

// GetMemberStakePool queries a member's stake pool info
func (q queryServer) GetMemberStakePool(ctx context.Context, req *types.QueryGetMemberStakePoolRequest) (*types.QueryGetMemberStakePoolResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, "request cannot be empty")
	}

	memberAddr, err := sdk.AccAddressFromBech32(req.Member)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid member address")
	}

	pool, err := q.k.GetMemberStakePool(ctx, memberAddr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// Return empty pool if not found
			return &types.QueryGetMemberStakePoolResponse{
				Pool: types.MemberStakePool{
					Member: req.Member,
				},
			}, nil
		}
		return nil, err
	}

	return &types.QueryGetMemberStakePoolResponse{
		Pool: pool,
	}, nil
}

// GetTagStakePool queries a tag's stake pool info
func (q queryServer) GetTagStakePool(ctx context.Context, req *types.QueryGetTagStakePoolRequest) (*types.QueryGetTagStakePoolResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, "request cannot be empty")
	}

	pool, err := q.k.GetTagStakePool(ctx, req.Tag)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// Return empty pool if not found
			return &types.QueryGetTagStakePoolResponse{
				Pool: types.TagStakePool{
					Tag: req.Tag,
				},
			}, nil
		}
		return nil, err
	}

	return &types.QueryGetTagStakePoolResponse{
		Pool: pool,
	}, nil
}

// GetProjectStakeInfo queries a project's stake info
func (q queryServer) GetProjectStakeInfo(ctx context.Context, req *types.QueryGetProjectStakeInfoRequest) (*types.QueryGetProjectStakeInfoResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, "request cannot be empty")
	}

	info, err := q.k.GetProjectStakeInfo(ctx, req.ProjectId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// Return empty info if not found
			return &types.QueryGetProjectStakeInfoResponse{
				Info: types.ProjectStakeInfo{
					ProjectId: req.ProjectId,
				},
			}, nil
		}
		return nil, err
	}

	return &types.QueryGetProjectStakeInfoResponse{
		Info: info,
	}, nil
}
