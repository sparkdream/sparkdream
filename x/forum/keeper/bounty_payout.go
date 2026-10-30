package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/forum/types"
)

// eligibleAward reports whether an assigned award can still be paid: the reply
// must exist, be ACTIVE, and not be anonymous (its author would be the shield
// module account, whose balance is the communal gas reserve).
func (k Keeper) eligibleAward(ctx context.Context, award *types.BountyAward) bool {
	if award == nil {
		return false
	}
	reply, err := k.Post.Get(ctx, award.PostId)
	if err != nil || reply.Status != types.PostStatus_POST_STATUS_ACTIVE {
		return false
	}
	return !k.isAnonymous(award.Recipient)
}

// payBountyAwards splits the escrow equally among the bounty's still-eligible
// awards (largest-remainder: the integer-division remainder goes one unit at a
// time in award order, so the full escrow is always paid out) and records each
// paid share on its award. Returns the number of awards paid; zero means
// nothing was eligible and the caller decides what happens to the escrow.
func (k Keeper) payBountyAwards(ctx context.Context, bounty *types.Bounty) (int, error) {
	bountyAmount, ok := math.NewIntFromString(bounty.Amount)
	if !ok {
		return 0, errorsInvalidEscrow(bounty.Id)
	}

	var eligible []*types.BountyAward
	for _, award := range bounty.Awards {
		if k.eligibleAward(ctx, award) {
			eligible = append(eligible, award)
		}
	}
	if len(eligible) == 0 {
		return 0, nil
	}

	numAwards := int64(len(eligible))
	share := bountyAmount.Quo(math.NewInt(numAwards))
	extra := bountyAmount.Mod(math.NewInt(numAwards)).Int64()

	for i, award := range eligible {
		awardAmount := share
		if int64(i) < extra {
			awardAmount = awardAmount.AddRaw(1)
		}
		if !awardAmount.IsPositive() {
			return 0, fmt.Errorf("bounty %d escrow too small to split among %d awards: %w", bounty.Id, numAwards, types.ErrInvalidAmount)
		}
		recipientAddr, err := sdk.AccAddressFromBech32(award.Recipient)
		if err != nil {
			return 0, fmt.Errorf("invalid recipient address for award to post %d: %w", award.PostId, err)
		}
		award.Amount = awardAmount.String()
		awardCoins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), awardAmount))
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipientAddr, awardCoins); err != nil {
			return 0, fmt.Errorf("failed to transfer bounty award to %s: %w", award.Recipient, err)
		}
	}
	return len(eligible), nil
}

func errorsInvalidEscrow(id uint64) error {
	return fmt.Errorf("bounty %d has an invalid escrow amount: %w", id, types.ErrInvalidAmount)
}

// clearActiveBountyIndex drops the thread's ActiveBountyByThread entry when it
// still points at this bounty (terminal transition: award, cancel, expiry).
// A stale entry would otherwise keep sentinels from curating the thread.
func (k Keeper) clearActiveBountyIndex(ctx context.Context, bounty types.Bounty) {
	if id, err := k.ActiveBountyByThread.Get(ctx, bounty.ThreadId); err == nil && id == bounty.Id {
		if err := k.ActiveBountyByThread.Remove(ctx, bounty.ThreadId); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("clearActiveBountyIndex: ActiveBountyByThread.Remove failed", "err", err)
		}
	}
}
