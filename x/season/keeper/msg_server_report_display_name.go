package keeper

import (
	"context"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) ReportDisplayName(ctx context.Context, msg *types.MsgReportDisplayName) (*types.MsgReportDisplayNameResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// TODO: Handle the message

	return &types.MsgReportDisplayNameResponse{}, nil
}
