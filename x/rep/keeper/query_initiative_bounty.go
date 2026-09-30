package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// InitiativeBounty returns the escrow against an initiative and, per
// contribution, when it becomes reclaimable.
func (q queryServer) InitiativeBounty(ctx context.Context, req *types.QueryInitiativeBountyRequest) (*types.QueryInitiativeBountyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	bounty := q.k.GetInitiativeBounty(ctx, req.InitiativeId)

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	open := false
	if initiative, gErr := q.k.GetInitiative(ctx, req.InitiativeId); gErr == nil {
		open = initiative.Status == types.InitiativeStatus_INITIATIVE_STATUS_OPEN
	}
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()

	statuses := make([]types.InitiativeBountyReclaimStatus, 0, len(bounty.Contributions))
	for _, c := range bounty.Contributions {
		matureAt := c.FundedAt + int64(params.InitiativeBountyReclaimDelay)
		amount := c.Amount
		if amount.IsNil() {
			amount = math.ZeroInt()
		}
		statuses = append(statuses, types.InitiativeBountyReclaimStatus{
			Funder:              c.Funder,
			Amount:              amount,
			ReclaimableAtHeight: matureAt,
			// Assignment wins over the clock: someone is working on the
			// strength of the bounty.
			Reclaimable: open && height >= matureAt,
		})
	}

	return &types.QueryInitiativeBountyResponse{Bounty: bounty, ReclaimStatus: statuses}, nil
}
