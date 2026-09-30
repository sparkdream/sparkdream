package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ReclaimInitiativeBounty returns a funder's own matured contributions while
// the initiative is still unassigned.
func (k msgServer) ReclaimInitiativeBounty(ctx context.Context, msg *types.MsgReclaimInitiativeBounty) (*types.MsgReclaimInitiativeBountyResponse, error) {
	funder, err := k.addressCodec.StringToBytes(msg.Funder)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, "invalid funder address")
	}
	refunded, err := k.Keeper.WithdrawInitiativeBounty(ctx, sdk.AccAddress(funder), msg.InitiativeId)
	if err != nil {
		return nil, err
	}
	return &types.MsgReclaimInitiativeBountyResponse{Refunded: refunded}, nil
}
