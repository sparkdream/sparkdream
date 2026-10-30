package app

import (
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
)

// A RECOVERY or COUNCIL migration means the old key is lost or stolen, so
// nobody can undelegate the old address's stake. Finalization starts
// unbonding every delegation of the tombstone, x/staking pays the matured
// SPARK to the tombstone (UndelegateCoins credits the account directly, the
// tombstone send restriction does not apply), and MsgSweepTombstone moves it
// to the canonical address. A KEY migration leaves delegations alone.
func TestTombstoneDelegationsUnbondAndSweep(t *testing.T) {
	var genFile string
	for _, f := range snapshotGenesisFiles(t) {
		if strings.Contains(f, "/rep/") {
			genFile = f
		}
	}
	if genFile == "" {
		t.Skip("no x/rep e2e snapshot; run /save-setup rep")
	}

	run := func(t *testing.T, council bool) {
		app, ctx, err := newAppFromGenesisFile(t, genFile)
		if err != nil {
			t.Skipf("snapshot %s does not import (%v); re-run /save-setup", genFile, err)
		}
		denom, err := app.StakingKeeper.BondDenom(ctx)
		require.NoError(t, err)
		params, err := app.RepKeeper.Params.Get(ctx)
		require.NoError(t, err)
		params.MigrationEnabled = true
		params.KeyPathEnabled = true
		if params.MaxKeyMigrationsPerWindow == 0 {
			params.MaxKeyMigrationsPerWindow = 1
		}
		require.NoError(t, app.RepKeeper.Params.Set(ctx, params))

		from := sdk.AccAddress([]byte("tombstone-unbond-a__"))
		to := sdk.AccAddress([]byte("tombstone-unbond-b__"))
		for _, a := range []sdk.AccAddress{from, to} {
			app.AuthKeeper.SetAccount(ctx, app.AuthKeeper.NewAccountWithAddress(ctx, a))
		}
		stake := sdkmath.NewInt(7_000_000)
		coins := sdk.NewCoins(sdk.NewCoin(denom, stake))
		require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, coins))
		require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, from, coins))
		zero := sdkmath.ZeroInt()
		require.NoError(t, app.RepKeeper.Member.Set(ctx, from.String(), reptypes.Member{
			Address:        from.String(),
			Status:         reptypes.MemberStatus_MEMBER_STATUS_ACTIVE,
			DreamBalance:   &zero,
			StakedDream:    &zero,
			LifetimeEarned: &zero,
			LifetimeBurned: &zero,
			TrustLevel:     reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED,
			ContentLicense: "CC0-1.0",
		}))

		vals, err := app.StakingKeeper.GetBondedValidatorsByPower(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, vals)
		val := vals[0]
		valAddr, err := sdk.ValAddressFromBech32(val.GetOperator())
		require.NoError(t, err)
		_, err = app.StakingKeeper.Delegate(ctx, from, stake, stakingtypes.Unbonded, val, true)
		require.NoError(t, err)

		var id uint64
		if council {
			gov := authtypes.NewModuleAddress(govtypes.ModuleName).String()
			id, err = app.RepKeeper.ForceMigration(ctx, gov, from.String(), to.String(), "ipfs://evidence")
		} else {
			id, err = app.RepKeeper.RequestKeyMigration(ctx, from.String(), to.String())
		}
		require.NoError(t, err)
		require.NoError(t, app.RepKeeper.AcceptMigration(ctx, to.String(), id))
		require.NoError(t, app.RepKeeper.FinalizeMigration(ctx, id))
		require.True(t, app.RepKeeper.IsTombstoned(ctx, from.String()))

		dels, err := app.StakingKeeper.GetDelegatorDelegations(ctx, from, 10)
		require.NoError(t, err)
		if !council {
			require.Len(t, dels, 1, "a KEY migration leaves delegations alone")
			return
		}
		require.Empty(t, dels, "finalization unbonds every delegation of the tombstone")
		ubd, err := app.StakingKeeper.GetUnbondingDelegation(ctx, from, valAddr)
		require.NoError(t, err)
		require.Len(t, ubd.Entries, 1)
		require.True(t, ubd.Entries[0].Balance.Equal(stake))

		// Before maturity there is nothing to sweep.
		swept, err := app.RepKeeper.SweepTombstone(ctx, to.String(), from.String())
		require.NoError(t, err)
		require.True(t, swept.AmountOf(denom).IsZero())

		// Unbonding matures: x/staking pays the tombstone directly.
		ctx = ctx.WithBlockTime(ubd.Entries[0].CompletionTime.Add(1))
		_, err = app.StakingKeeper.CompleteUnbonding(ctx, from, valAddr)
		require.NoError(t, err)
		require.True(t, app.BankKeeper.GetBalance(ctx, from, denom).Amount.Equal(stake),
			"the matured SPARK lands at the tombstone")

		before := app.BankKeeper.GetBalance(ctx, to, denom).Amount
		swept, err = app.RepKeeper.SweepTombstone(ctx, to.String(), from.String())
		require.NoError(t, err)
		require.True(t, swept.AmountOf(denom).Equal(stake))
		require.True(t, app.BankKeeper.GetBalance(ctx, from, denom).Amount.IsZero())
		require.True(t, app.BankKeeper.GetBalance(ctx, to, denom).Amount.Equal(before.Add(stake)))

		// Only the canonical address sweeps.
		_, err = app.RepKeeper.SweepTombstone(ctx, from.String(), from.String())
		require.ErrorIs(t, err, reptypes.ErrNotTombstoneOwner)
	}

	t.Run("council path unbonds and sweeps", func(t *testing.T) { run(t, true) })
	t.Run("key path keeps delegations", func(t *testing.T) { run(t, false) })
}
