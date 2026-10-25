package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgCancelMigration(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		var found *types.MemberMigration
		_ = k.MemberMigrations.Walk(ctx, nil, func(_ uint64, m types.MemberMigration) (bool, error) {
			if m.Path == types.MigrationPath_MIGRATION_PATH_KEY && types.IsPendingMigrationStatus(m.Status) {
				if _, ok := accountFor(accs, m.FromAddress); ok {
					found = &m
					return true, nil
				}
			}
			return false, nil
		})
		if found == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCancelMigration{}), "no pending key migration to cancel"), nil, nil
		}
		acc, _ := accountFor(accs, found.FromAddress)
		msg := &types.MsgCancelMigration{Signer: found.FromAddress, MigrationId: found.Id}
		return deliver(r, app, ctx, msg, acc, ak, bk, txGen)
	}
}
