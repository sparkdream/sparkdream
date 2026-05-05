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

// requiredInvitationStakeForSim mirrors keeper.computeRequiredInvitationStake
// (which is unexported). Kept in lock-step so simulation never selects a
// stake that the on-chain handler would reject.
func requiredInvitationStakeForSim(params types.Params, member types.Member) math.Int {
	maxCredits := keeper.GetMaxInvitationCredits(params.TrustLevelConfig, member.TrustLevel)
	if maxCredits <= member.InvitationCredits {
		return params.MinInvitationStake
	}
	used := uint64(maxCredits - member.InvitationCredits)
	if used == 0 || params.InvitationCostMultiplier.LTE(math.LegacyOneDec()) {
		return params.MinInvitationStake
	}
	multiplier := params.InvitationCostMultiplier.Power(used)
	return multiplier.MulInt(params.MinInvitationStake).TruncateInt()
}

func SimulateMsgInviteMember(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Pull the chain's minimum invitation stake from params instead of
		// hardcoding. The on-chain minimum is denominated in micro-DREAM
		// (default 100 DREAM = 100_000_000 udream), and escalates per
		// credit consumed. A hardcoded `100` produced a stake well under
		// the floor and made every simulated invite fail with
		// "insufficient stake".
		params, perr := k.Params.Get(ctx)
		if perr != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgInviteMember{}), "failed to read params"), nil, nil
		}
		minAmount := params.MinInvitationStake
		if minAmount.IsNil() || minAmount.IsZero() {
			minAmount = math.NewInt(100_000_000)
		}
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

		// Floor the simulated stake at the chain's required minimum, scaled
		// up by the per-credit escalator if the inviter has already spent
		// credits this season. ComputeRequiredInvitationStake mirrors the
		// keeper's check exactly so we never pick a number that would be
		// rejected by the message handler.
		minStake := requiredInvitationStakeForSim(params, reloadedMember)
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
