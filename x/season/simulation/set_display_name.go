package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgSetDisplayName delivers MsgSetDisplayName from a random account
// (creating its profile when it has none). Skips names the handler refuses,
// a display name under active moderation and the change cooldown.
func SimulateMsgSetDisplayName(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		addr := simAccount.Address.String()
		msg := &types.MsgSetDisplayName{Creator: addr, Name: randomDisplayName(r)}

		if k.ValidateDisplayName(ctx, msg.Name) != nil {
			return noOp(msg, "invalid display name")
		}
		if displayNameBlocked(ctx, k, addr, msg.Name) {
			return noOp(msg, "display name blocked by moderation")
		}
		if profile, ok := getProfile(ctx, k, addr); ok {
			params, err := k.Params.Get(ctx)
			if err != nil {
				return noOpErr(msg, err)
			}
			if profile.LastDisplayNameChangeEpoch > 0 {
				since := k.GetCurrentEpoch(ctx) - profile.LastDisplayNameChangeEpoch
				if commontypes.Saturate[uint64](since) < params.DisplayNameChangeCooldownEpochs {
					return noOp(msg, "display name change cooldown")
				}
			}
			if m, err := k.DisplayNameModeration.Get(ctx, addr); err == nil && m.Active {
				return noOp(msg, "display name is moderated")
			}
		}

		return deliver(r, app, ctx, txGen, ak, bk, simAccount, msg)
	}
}
