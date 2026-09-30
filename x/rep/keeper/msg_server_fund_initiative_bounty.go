package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// FundInitiativeBounty escrows DREAM against an initiative, paid to its
// assignee if it completes.
func (k msgServer) FundInitiativeBounty(ctx context.Context, msg *types.MsgFundInitiativeBounty) (*types.MsgFundInitiativeBountyResponse, error) {
	funder, err := k.addressCodec.StringToBytes(msg.Funder)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, "invalid funder address")
	}
	if _, err := k.Keeper.GetMember(ctx, sdk.AccAddress(funder)); err != nil {
		return nil, errorsmod.Wrap(types.ErrNotMember, "funder must be a member")
	}
	total, err := k.Keeper.EscrowInitiativeBounty(ctx, sdk.AccAddress(funder), msg.InitiativeId, msg.Amount)
	if err != nil {
		return nil, err
	}
	return &types.MsgFundInitiativeBountyResponse{Total: total}, nil
}
