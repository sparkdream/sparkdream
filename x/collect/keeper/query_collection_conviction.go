package keeper

import (
	"context"

	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"

	"cosmossdk.io/math"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) CollectionConviction(ctx context.Context, req *types.QueryCollectionConvictionRequest) (*types.QueryCollectionConvictionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Verify collection exists
	_, err := q.k.Collection.Get(ctx, req.CollectionId)
	if err != nil {
		return nil, status.Error(codes.NotFound, "collection not found")
	}

	targetType := reptypes.StakeTargetType_STAKE_TARGET_COLLECTION_CONTENT

	// Get conviction score
	conviction, err := q.k.repKeeper.GetContentConviction(ctx, targetType, req.CollectionId)
	if err != nil {
		conviction = math.LegacyZeroDec()
	}

	// Get stakes to compute count and total
	stakes, err := q.k.repKeeper.GetContentStakes(ctx, targetType, req.CollectionId)
	if err != nil {
		stakes = nil
	}

	stakeCount := uint32(len(stakes))
	totalStaked := math.ZeroInt()
	for _, s := range stakes {
		totalStaked = totalStaked.Add(s.Amount)
	}

	// Get author bond
	authorBond := math.ZeroInt()
	bond, err := q.k.repKeeper.GetAuthorBond(ctx, targetType, req.CollectionId)
	if err == nil {
		authorBond = bond.Amount
	}

	return &types.QueryCollectionConvictionResponse{
		ConvictionScore: conviction,
		StakeCount:      stakeCount,
		TotalStaked:     totalStaked,
		AuthorBond:      authorBond,
	}, nil
}
