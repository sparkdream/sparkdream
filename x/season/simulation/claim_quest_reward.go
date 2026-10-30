package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgClaimQuestReward delivers MsgClaimQuestReward for a sim
// account's open quest progress whose objectives are all complete. Skips
// maintenance mode, quests no longer open, and members without a profile.
func SimulateMsgClaimQuestReward(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgClaimQuestReward{}

		if k.IsInMaintenanceMode(ctx) {
			return noOp(msg, "maintenance mode")
		}
		simAddrs := make(map[string]bool, len(accs))
		for _, a := range accs {
			simAddrs[a.Address.String()] = true
		}
		open, err := openQuestProgress(ctx, k, simAddrs)
		if err != nil {
			return noOpErr(msg, err)
		}
		var claimable []questRef
		for _, ref := range open {
			q, err := k.Quest.Get(ctx, ref.questID)
			if err != nil || !questOpenNow(ctx, k, q) || !questObjectivesComplete(q, ref.progress) {
				continue
			}
			if _, ok := getProfile(ctx, k, ref.member); !ok {
				continue
			}
			claimable = append(claimable, ref)
		}
		ref, ok := pick(r, claimable)
		if !ok {
			return noOp(msg, "no claimable quest")
		}
		simAccount, _ := getAccountForAddress(ref.member, accs)
		msg.Creator, msg.QuestId = ref.member, ref.questID

		return deliver(r, app, ctx, txGen, ak, bk, simAccount, msg)
	}
}
