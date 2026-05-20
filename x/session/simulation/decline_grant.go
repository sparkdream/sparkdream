package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// SimulateMsgDeclineGrant picks a random active grant and submits a
// decline from the grantee. Exercises the universal `MsgDeclineGrant`
// umbrella that replaced per-type decline messages in P6.
//
// NoOps when no active grant has a grantee that is also a sim
// account.
func SimulateMsgDeclineGrant(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgDeclineGrant{})

		typeOptions := []types.GrantType{
			types.GrantType_GRANT_TYPE_SESSION_KEY,
			types.GrantType_GRANT_TYPE_RECURRING_PULL,
			types.GrantType_GRANT_TYPE_SPENDING_ALLOWANCE,
			types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT,
		}
		r.Shuffle(len(typeOptions), func(i, j int) { typeOptions[i], typeOptions[j] = typeOptions[j], typeOptions[i] })

		for _, t := range typeOptions {
			grant, found := findRandomGrantOfType(r, ctx, k, t, func(g types.Grant) bool {
				return g.Status == types.GrantStatus_GRANT_STATUS_ACTIVE ||
					g.Status == types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS
			})
			if !found {
				continue
			}
			granteeAcc, ok := findSimAccount(accs, grant.Grantee)
			if !ok {
				continue
			}

			msg := &types.MsgDeclineGrant{
				Grantee: grant.Grantee,
				GrantId: grant.Id,
			}

			opMsg := simulation.OperationInput{
				R:               r,
				App:             app,
				TxGen:           txGen,
				Cdc:             nil,
				Msg:             msg,
				CoinsSpentInMsg: sdk.NewCoins(),
				Context:         ctx,
				SimAccount:      granteeAcc,
				AccountKeeper:   ak,
				Bankkeeper:      bk,
				ModuleName:      types.ModuleName,
			}

			return simulation.GenAndDeliverTxWithRandFees(opMsg)
		}

		return simtypes.NoOpMsg(types.ModuleName, msgType, "no declinable grant for any sim grantee"), nil, nil
	}
}
