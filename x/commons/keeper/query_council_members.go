package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (q queryServer) GetCouncilMembers(ctx context.Context, req *types.QueryGetCouncilMembersRequest) (*types.QueryGetCouncilMembersResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "empty request")
	}

	members, err := q.k.GetCouncilMembers(ctx, req.CouncilName)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "council %s not found", req.CouncilName)
	}

	return &types.QueryGetCouncilMembersResponse{Members: members}, nil
}
