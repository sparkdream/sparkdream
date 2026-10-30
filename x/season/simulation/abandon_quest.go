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

// SimulateMsgAbandonQuest delivers MsgAbandonQuest for a sim account's open
// quest progress on a quest that still exists. Skips members without a
// profile (the handler would write an empty profile for them).
func SimulateMsgAbandonQuest(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAbandonQuest{}

		simAddrs := make(map[string]bool, len(accs))
		for _, a := range accs {
			simAddrs[a.Address.String()] = true
		}
		open, err := openQuestProgress(ctx, k, simAddrs)
		if err != nil {
			return noOpErr(msg, err)
		}
		var abandonable []questRef
		for _, ref := range open {
			if has, err := k.Quest.Has(ctx, ref.questID); err != nil || !has {
				continue
			}
			if _, ok := getProfile(ctx, k, ref.member); !ok {
				continue
			}
			abandonable = append(abandonable, ref)
		}
		ref, ok := pick(r, abandonable)
		if !ok {
			return noOp(msg, "no abandonable quest")
		}
		simAccount, _ := getAccountForAddress(ref.member, accs)
		msg.Creator, msg.QuestId = ref.member, ref.questID

		return deliver(r, app, ctx, txGen, ak, bk, simAccount, msg)
	}
}
