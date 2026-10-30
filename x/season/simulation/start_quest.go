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

// SimulateMsgStartQuest delivers MsgStartQuest from a random account
// (creating its profile first when needed) for a quest it may start now
// (quest definitions come from SimulateMsgCreateQuest's proposals). Skips maintenance mode and accounts for which no quest
// passes the handler's window, level, prerequisite, cooldown and
// active-quest checks.
func SimulateMsgStartQuest(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		addr := simAccount.Address.String()
		msg := &types.MsgStartQuest{Creator: addr}

		if k.IsInMaintenanceMode(ctx) {
			return noOp(msg, "maintenance mode")
		}
		if ok, err := ensureProfile(r, app, ctx, txGen, ak, bk, k, simAccount); err != nil || !ok {
			if err != nil {
				return noOpErr(msg, err)
			}
			return noOp(msg, "could not create member profile")
		}
		profile, _ := getProfile(ctx, k, addr)
		params, err := k.Params.Get(ctx)
		if err != nil {
			return noOpErr(msg, err)
		}
		var startable []string
		if err := k.Quest.Walk(ctx, nil, func(id string, q types.Quest) (bool, error) {
			if canStartQuest(ctx, k, addr, profile, q, params.MaxActiveQuestsPerMember) {
				startable = append(startable, id)
			}
			return false, nil
		}); err != nil {
			return noOpErr(msg, err)
		}
		questID, ok := pick(r, startable)
		if !ok {
			return noOp(msg, "no startable quest")
		}
		msg.QuestId = questID

		return deliver(r, app, ctx, txGen, ak, bk, simAccount, msg)
	}
}
