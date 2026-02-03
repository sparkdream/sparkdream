package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgInviteMember(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create an inviter with sufficient DREAM
		minAmount := math.NewInt(100)
		inviterMember, inviterAcc, err := getOrCreateMemberWithDream(r, ctx, k, accs, minAmount)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgInviteMember{}), "failed to get/create inviter with DREAM"), nil, nil
		}

		// Inviter must already have invitation credits
		// (We can't reliably grant them here as the change won't be visible to the tx delivery context)
		if inviterMember.InvitationCredits == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgInviteMember{}), "inviter has no invitation credits"), nil, nil
		}

		// Get or create an invitee (different from inviter, not a member, no pending invitation)
		var inviteeAcc simtypes.Account
		var inviteeAddr string
		for i := 0; i < 10; i++ {
			inviteeAcc, _ = simtypes.RandomAcc(r, accs)
			if inviteeAcc.Address.String() == inviterMember.Address {
				continue
			}
			// Check if invitee is already a member
			_, err := k.Member.Get(ctx, inviteeAcc.Address.String())
			if err == nil {
				continue // Already a member
			}
			// Check for existing pending invitation
			hasPendingInvite := false
			k.Invitation.Walk(ctx, nil, func(id uint64, inv types.Invitation) (bool, error) {
				if inv.InviteeAddress == inviteeAcc.Address.String() && inv.Status == types.InvitationStatus_INVITATION_STATUS_PENDING {
					hasPendingInvite = true
					return true, nil
				}
				return false, nil
			})
			if !hasPendingInvite {
				inviteeAddr = inviteeAcc.Address.String()
				break
			}
		}
		if inviteeAddr == "" {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgInviteMember{}), "unable to find suitable invitee"), nil, nil
		}

		// Generate random stake amount (100-500 DREAM)
		// Reload member to get current balance (may have changed)
		reloadedMember, err := k.Member.Get(ctx, inviterMember.Address)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgInviteMember{}), "failed to reload inviter"), nil, nil
		}

		minStake := math.NewInt(100)
		if reloadedMember.DreamBalance == nil || reloadedMember.DreamBalance.LT(minStake) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgInviteMember{}), "insufficient balance"), nil, nil
		}

		// Calculate available (unstaked) balance
		availableBalance := *reloadedMember.DreamBalance
		if reloadedMember.StakedDream != nil {
			availableBalance = availableBalance.Sub(*reloadedMember.StakedDream)
		}

		if availableBalance.LT(minStake) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgInviteMember{}), "insufficient unstaked balance"), nil, nil
		}

		maxStake := availableBalance.QuoRaw(2)
		if maxStake.LT(minStake) {
			maxStake = minStake
		}
		// Don't exceed available balance
		if maxStake.GT(availableBalance) {
			maxStake = availableBalance
		}

		var stakedDream math.Int
		rangeVal := maxStake.Sub(minStake).Int64()
		if rangeVal > 0 {
			stakedDream = math.NewInt(int64(r.Intn(int(rangeVal))) + minStake.Int64())
		} else {
			stakedDream = minStake
		}

		msg := &types.MsgInviteMember{
			Inviter:        inviterMember.Address,
			InviteeAddress: inviteeAddr,
			StakedDream:    &stakedDream,
			VouchedTags:    randomTags(r),
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      inviterAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}
