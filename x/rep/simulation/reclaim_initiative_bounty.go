package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgReclaimInitiativeBounty(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		noop := func(reason string) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReclaimInitiativeBounty{}), reason), nil, nil
		}
		params, err := k.Params.Get(ctx)
		if err != nil {
			return noop("failed to get params")
		}

		// Find a matured contribution on an OPEN initiative.
		var msg *types.MsgReclaimInitiativeBounty
		var signer simtypes.Account
		height := ctx.BlockHeight()
		_ = k.InitiativeBounty.Walk(ctx, nil, func(id uint64, b types.InitiativeBounty) (bool, error) {
			initiative, gErr := k.Initiative.Get(ctx, id)
			if gErr != nil || initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_OPEN {
				return false, nil
			}
			for _, c := range b.Contributions {
				if height < c.FundedAt+int64(params.InitiativeBountyReclaimDelay) {
					continue
				}
				acc, found := getAccountFromMember(&types.Member{Address: c.Funder}, accs)
				if !found {
					continue
				}
				msg = &types.MsgReclaimInitiativeBounty{Funder: c.Funder, InitiativeId: id}
				signer = acc
				return true, nil
			}
			return false, nil
		})
		if msg == nil {
			return noop("no reclaimable initiative bounty contribution")
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      signer,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}
