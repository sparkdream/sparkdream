package simulation

import (
	"math/rand"
	"time"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

func SimulateMsgCreateSession(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgCreateSession{})

		if len(accs) < 2 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "need at least 2 accounts"), nil, nil
		}

		// 1. Get params
		params, err := k.Params.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get params"), nil, nil
		}

		// 2. Pick random granter — shuffle and find one under the session limit
		perm := r.Perm(len(accs))
		var granterAcc simtypes.Account
		granterFound := false
		for _, idx := range perm {
			acc := accs[idx]
			if countGranterSessions(ctx, k, acc.Address.String()) < params.MaxSessionsPerGranter {
				granterAcc = acc
				granterFound = true
				break
			}
		}
		if !granterFound {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "all accounts at max sessions"), nil, nil
		}

		// 3. Pick a different grantee with no existing session for this pair
		var granteeAcc simtypes.Account
		granteeFound := false
		perm2 := r.Perm(len(accs))
		for _, idx := range perm2 {
			acc := accs[idx]
			if acc.Address.String() == granterAcc.Address.String() {
				continue
			}
			_, getErr := k.GetSession(ctx, granterAcc.Address.String(), acc.Address.String())
			if getErr != nil {
				// No existing session — good
				granteeAcc = acc
				granteeFound = true
				break
			}
		}
		if !granteeFound {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "all grantee pairs taken"), nil, nil
		}

		// 4. Check granter has funds for gas
		balance := bk.SpendableCoins(ctx, granterAcc.Address)
		if balance.AmountOf("uspark").LT(math.NewInt(10000)) {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "granter has insufficient funds"), nil, nil
		}

		// 5. Pick random subset of allowed message types
		if len(params.AllowedMsgTypes) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no allowed msg types"), nil, nil
		}
		maxMsgTypes := int(params.MaxMsgTypesPerSession)
		if maxMsgTypes <= 0 {
			maxMsgTypes = 5
		}
		allowedMsgTypes := randomSubset(r, params.AllowedMsgTypes, maxMsgTypes)

		// 6. Random spend limit
		var spendLimit sdk.Coin
		if r.Intn(3) == 0 {
			spendLimit = sdk.NewInt64Coin("uspark", 0)
		} else {
			maxAmount := params.MaxSpendLimit.Amount.Int64()
			if maxAmount <= 0 {
				maxAmount = 100_000_000
			}
			spendLimit = sdk.NewCoin("uspark", math.NewInt(r.Int63n(maxAmount)+1))
		}

		// 7. Random expiration
		maxExpDuration := params.MaxExpiration
		if maxExpDuration <= 0 {
			maxExpDuration = 7 * 24 * time.Hour
		}
		minDuration := time.Minute
		durationRange := int64(maxExpDuration - minDuration)
		if durationRange <= 0 {
			durationRange = int64(time.Hour)
		}
		expiration := ctx.BlockTime().Add(minDuration + time.Duration(r.Int63n(durationRange)))

		// 8. Random max_exec_count
		var maxExecCount uint64
		if r.Intn(4) == 0 {
			maxExecCount = 0 // unlimited
		} else {
			maxExecCount = uint64(r.Intn(50)) + 1
		}

		// 9. Construct and deliver
		msg := &types.MsgCreateSession{
			Granter:         granterAcc.Address.String(),
			Grantee:         granteeAcc.Address.String(),
			AllowedMsgTypes: allowedMsgTypes,
			SpendLimit:      spendLimit,
			Expiration:      expiration,
			MaxExecCount:    maxExecCount,
		}

		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      granterAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}
