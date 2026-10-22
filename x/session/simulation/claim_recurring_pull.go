package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// SimulateMsgClaimRecurringPull picks a random RECURRING_PULL grant
// whose claim window is open (`last_claim_advance + period_seconds <= block_time`)
// and submits a claim from the grantee. The handler enforces the rate
// limit / cadence / expires checks; the sim is content with the
// happy-path delivery.
//
// NoOps when:
//   - No RECURRING_PULL grants exist yet (SimulateMsgCreateGrant will
//     populate the registry over time).
//   - The selected grant's grantee isn't in the sim account set (we
//     can't sign on their behalf otherwise).
func SimulateMsgClaimRecurringPull(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgClaimRecurringPull{})

		now := ctx.BlockTime().Unix()
		grant, found := findRandomGrantOfType(
			r, ctx, k, types.GrantType_GRANT_TYPE_RECURRING_PULL,
			func(g types.Grant) bool {
				// Only claimable when active + window open.
				if g.Status != types.GrantStatus_GRANT_STATUS_ACTIVE &&
					g.Status != types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS {
					return false
				}
				rp := g.GetRecurringPull()
				if rp == nil {
					return false
				}
				if rp.LastClaimAdvance+rp.PeriodSeconds > now {
					return false
				}
				// Window must still be open (next advance fits before expires_at).
				if rp.LastClaimAdvance+rp.PeriodSeconds > g.ExpiresAt.Unix() {
					return false
				}
				// Mirror the handler's per-UTC-day self-throttle: catch-up
				// claims inside one day can exhaust max_per_epoch.
				maxPerEpoch, ok := sdkmath.NewIntFromString(rp.MaxPerEpoch)
				if !ok {
					return false
				}
				spent := sdkmath.ZeroInt()
				if stored, err := k.EpochSpendByGrant.Get(ctx, collections.Join(g.Id, now/86400)); err == nil {
					if v, ok := sdkmath.NewIntFromString(stored); ok {
						spent = v
					}
				}
				return !spent.Add(rp.AmountPerPeriod.Amount).GT(maxPerEpoch)
			},
		)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no claimable recurring-pull grant"), nil, nil
		}

		granteeAcc, ok := findSimAccount(accs, grant.Grantee)
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "grantee not a sim account"), nil, nil
		}

		msg := &types.MsgClaimRecurringPull{
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
}
