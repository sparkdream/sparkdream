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

func SimulateMsgRequestMigration(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		member, simAccount, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRequestMigration{}), "failed to get/create member"), nil, nil
		}
		// Mirror the handler's own gates so the delivered tx cannot fail.
		if member.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE || k.HasOpenMemberReport(ctx, member.Address) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRequestMigration{}), "member not eligible for the key path"), nil, nil
		}
		if _, pending := k.GetPendingMigration(ctx, member.Address); pending {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRequestMigration{}), "migration already pending"), nil, nil
		}
		params, err := k.Params.Get(ctx)
		if err != nil || !params.MigrationEnabled || !params.KeyPathEnabled {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRequestMigration{}), "key path disabled"), nil, nil
		}
		now := ctx.BlockTime().Unix()
		if member.LastMigratedAt > 0 && now < member.LastMigratedAt+params.MigrationCooldown {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRequestMigration{}), "migration cooldown"), nil, nil
		}
		inWindow := 0
		for _, t := range member.KeyMigrationTimes {
			if t > now-params.KeyMigrationWindow {
				inWindow++
			}
		}
		if uint32(inWindow) >= params.MaxKeyMigrationsPerWindow {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRequestMigration{}), "key migration window cap"), nil, nil
		}
		target, ok := freshTarget(r, ctx, k, accs, member.Address)
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRequestMigration{}), "no fresh target account"), nil, nil
		}
		msg := &types.MsgRequestMigration{Member: member.Address, NewAddress: target.Address.String()}
		return deliver(r, app, ctx, msg, simAccount, ak, bk, txGen)
	}
}
