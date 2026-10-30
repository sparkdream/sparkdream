package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/core/address"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	icatypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	module "sparkdream/x/rep/module"
	"sparkdream/x/rep/types"
)

// accountsAuthKeeper serves fixed accounts by address.
type accountsAuthKeeper struct {
	codec    address.Codec
	accounts map[string]sdk.AccountI
}

func (a accountsAuthKeeper) AddressCodec() address.Codec { return a.codec }
func (a accountsAuthKeeper) GetAccount(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	return a.accounts[string(addr)]
}

// Part 4 handoff: an interchain account is not a person. It must not be
// invited, accept an invitation, sign a vault or be a migration target.
func TestIsPersonalAddress_RefusesInterchainAccounts(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	codec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_personal")).Ctx

	ica := sdk.AccAddress([]byte("ica-account________"))
	person := sdk.AccAddress([]byte("person-account_____"))
	icaAcc := icatypes.NewInterchainAccount(authtypes.NewBaseAccountWithAddress(ica), "controller-owner")
	auth := accountsAuthKeeper{codec: codec, accounts: map[string]sdk.AccountI{
		string(ica):    icaAcc,
		string(person): authtypes.NewBaseAccountWithAddress(person),
	}}
	k := keeper.NewKeeper(runtime.NewKVStoreService(storeKey), encCfg.Codec, codec,
		authtypes.NewModuleAddress(types.GovModuleName), auth, &mockBankKeeper{}, nil)

	err := k.IsPersonalAddress(ctx, ica.String())
	require.Error(t, err)
	require.Contains(t, err.Error(), "interchain account")
	require.NoError(t, k.IsPersonalAddress(ctx, person.String()))
}

// Part 7 handoff: no identity moves while x/season is in maintenance mode;
// a due migration waits in the queue and finalizes once maintenance ends.
func TestMigrationFinalizationWaitsOutSeasonMaintenance(t *testing.T) {
	f := migrationFixture(t)
	a := sdk.AccAddress([]byte("maint-a____________"))
	b := sdk.AccAddress([]byte("maint-b____________"))
	seedMigrant(t, f, a, 1)
	id, err := f.keeper.RequestKeyMigration(f.ctx, a.String(), b.String())
	require.NoError(t, err)
	require.NoError(t, f.keeper.AcceptMigration(f.ctx, b.String(), id))
	advance(f, mustParams(t, f).KeyMigrationDelay+1, 1)

	f.seasonKeeper.MaintenanceMode = true
	f.keeper.ProcessMigrations(f.ctx)
	m, _ := f.keeper.GetMemberMigration(f.ctx, id)
	require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_ACCEPTED, m.Status)
	require.False(t, f.keeper.IsTombstoned(f.ctx, a.String()))

	f.seasonKeeper.MaintenanceMode = false
	advance(f, 1, 1)
	f.keeper.ProcessMigrations(f.ctx)
	m, _ = f.keeper.GetMemberMigration(f.ctx, id)
	require.Equal(t, types.MigrationStatus_MIGRATION_STATUS_FINALIZED, m.Status, m.FailureReason)
}
