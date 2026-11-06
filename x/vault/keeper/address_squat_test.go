package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/vault/types"
)

// TestCreateVaultAddressSquat (VLT-101): anyone can compute the next
// VaultAddress(id) and put a vesting account there with the SDK vesting
// msgs. Creation must still succeed: a keyless vesting squatter is turned
// into the vault's BaseAccount (keeping its account number), and any other
// occupant only costs the vault that id.
func TestCreateVaultAddressSquat(t *testing.T) {
	f := initFixture(t)
	f.bank.balances[creator] = math.NewInt(100_000_000)
	coins := sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1))
	now := f.ctx.BlockTime().Unix()

	squat := map[uint64]func(*authtypes.BaseAccount) (sdk.AccountI, error){
		1: func(b *authtypes.BaseAccount) (sdk.AccountI, error) {
			return vestingtypes.NewContinuousVestingAccount(b, coins, now, now+1_000_000_000)
		},
		2: func(b *authtypes.BaseAccount) (sdk.AccountI, error) {
			return vestingtypes.NewPermanentLockedAccount(b, coins)
		},
		3: func(b *authtypes.BaseAccount) (sdk.AccountI, error) {
			return vestingtypes.NewPeriodicVestingAccount(b, coins, now, vestingtypes.Periods{{Length: 1_000_000_000, Amount: coins}})
		},
	}
	for id := uint64(1); id <= 3; id++ {
		base := authtypes.NewBaseAccount(types.VaultAddress(id), nil, 900+id, 0)
		acc, err := squat[id](base)
		require.NoError(t, err)
		f.auth.SetAccount(f.ctx, acc)
	}
	// Id 4 holds an account with a public key: never adoptable (no key
	// can exist for a vault address, so this only guards future account
	// types); the create skips it.
	pk := secp256k1.GenPrivKey().PubKey()
	withKey := authtypes.NewBaseAccount(types.VaultAddress(4), pk, 904, 0)
	f.auth.SetAccount(f.ctx, withKey)

	create := func() types.Vault {
		t.Helper()
		res, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
			Creator: creator, Signers: signers(map[string]uint64{alice: 1}, alice), Threshold: 1, ProposalTtl: 3600,
		})
		require.NoError(t, err)
		v, err := f.keeper.Vault.Get(f.ctx, res.VaultId)
		require.NoError(t, err)
		return v
	}
	for id := uint64(1); id <= 3; id++ {
		v := create()
		require.Equal(t, id, v.VaultId)
		want, err := f.addressCodec.BytesToString(types.VaultAddress(id))
		require.NoError(t, err)
		require.Equal(t, want, v.Address)
		acc := f.auth.accounts[v.Address]
		base, ok := acc.(*authtypes.BaseAccount)
		require.True(t, ok, "vesting squatter replaced by a base account, got %T", acc)
		require.Equal(t, 900+id, base.GetAccountNumber(), "account number kept")
		require.Nil(t, base.GetPubKey())
	}
	v := create()
	require.Equal(t, uint64(5), v.VaultId, "the keyed occupant's id is skipped")
	require.False(t, f.keeper.IsVault(f.ctx, sdk.AccAddress(types.VaultAddress(4)).String()))
	require.Equal(t, withKey, f.auth.accounts[sdk.AccAddress(types.VaultAddress(4)).String()], "a skipped occupant is left alone")

	// Every candidate occupied: refused, bounded.
	for id := uint64(6); id <= 6+types.MaxVaultAddressSkips; id++ {
		f.auth.SetAccount(f.ctx, authtypes.NewBaseAccount(types.VaultAddress(id), pk, 1000+id, 0))
	}
	_, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
		Creator: creator, Signers: signers(map[string]uint64{alice: 1}, alice), Threshold: 1, ProposalTtl: 3600,
	})
	require.ErrorIs(t, err, types.ErrAddressOccupied)
}
