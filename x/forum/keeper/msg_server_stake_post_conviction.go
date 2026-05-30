package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"

	"sparkdream/x/forum/types"
)

// StakePostConviction opens a PostConvictionStake. Validation here is the
// caller-policy layer (membership + trust level + non-self); keeper.OpenPostConvictionStake
// owns the structural validation (post status, amount floor, lock-period
// presence, DREAM lock).
func (k msgServer) StakePostConviction(
	ctx context.Context,
	msg *types.MsgStakePostConviction,
) (*types.MsgStakePostConvictionResponse, error) {
	stakerBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	staker := sdk.AccAddress(stakerBytes)

	if k.repKeeper == nil {
		return nil, fmt.Errorf("rep keeper not wired")
	}

	// Only ESTABLISHED+ members may open conviction stakes — discourse
	// endorsements should not come from PROVISIONAL accounts because the
	// gating function of the trust ladder breaks if NEW/PROVISIONAL members
	// can boost each other to ESTABLISHED via stakes.
	if !k.repKeeper.IsActiveMember(ctx, staker) {
		return nil, fmt.Errorf("only active members may open conviction stakes")
	}
	tl, err := k.repKeeper.GetTrustLevel(ctx, staker)
	if err != nil {
		return nil, fmt.Errorf("trust level lookup: %w", err)
	}
	if tl < reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED {
		return nil, fmt.Errorf("conviction stakes require ESTABLISHED+ (caller is %s)", tl.String())
	}

	id, err := k.Keeper.OpenPostConvictionStake(ctx, staker, msg.PostId, msg.Amount)
	if err != nil {
		return nil, err
	}
	return &types.MsgStakePostConvictionResponse{StakeId: id}, nil
}
