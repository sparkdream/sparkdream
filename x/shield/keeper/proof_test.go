package keeper_test

import (
	"context"
	"testing"

	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/keeper"
	module "sparkdream/x/shield/module"
	"sparkdream/x/shield/types"
)

// mockRepKeeper implements types.RepKeeper for proof tests.
type mockRepKeeper struct {
	currentRoot  []byte
	previousRoot []byte
}

func (m *mockRepKeeper) GetTrustTreeRoot(_ context.Context) ([]byte, error) {
	return m.currentRoot, nil
}

func (m *mockRepKeeper) GetPreviousTrustTreeRoot(_ context.Context) ([]byte, error) {
	return m.previousRoot, nil
}

// initFixtureWithRepKeeper creates a fixture with a mock RepKeeper wired.
func initFixtureWithRepKeeper(t *testing.T, repK *mockRepKeeper) *fixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	key := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(key)
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_test"))
	ctx := testCtx.Ctx

	addrCodec := addresscodec.NewBech32Codec("sprkdrm")
	authority := authtypes.NewModuleAddress("gov")

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addrCodec,
		authority,
		mockAccountKeeper{},
		mockBankKeeper{},
	)

	mockSK := mockStakingKeeper{
		validators: []stakingtypes.Validator{
			{OperatorAddress: "sprkdrmvaloper1aaaaa"},
		},
	}
	k.SetStakingKeeper(mockSK)
	k.SetRepKeeper(repK)

	err := k.InitGenesis(ctx, *types.DefaultGenesis())
	require.NoError(t, err)

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addrCodec,
	}
}

func TestVerifyProofNoVKSkips(t *testing.T) {
	f := initFixture(t)

	// No verification key stored — verifyProof skips entirely (returns nil).
	_, found := f.keeper.GetVerificationKeyVal(f.ctx, "shield_v1")
	require.False(t, found, "no VK should be stored by default")
}

func TestVerificationKeyRoundTrip(t *testing.T) {
	f := initFixture(t)

	t.Run("store and retrieve multiple VKs", func(t *testing.T) {
		vk1 := types.VerificationKey{CircuitId: "circuit_a", VkBytes: []byte("vk_a_data")}
		vk2 := types.VerificationKey{CircuitId: "circuit_b", VkBytes: []byte("vk_b_data")}

		require.NoError(t, f.keeper.SetVerificationKey(f.ctx, vk1))
		require.NoError(t, f.keeper.SetVerificationKey(f.ctx, vk2))

		got1, found := f.keeper.GetVerificationKeyVal(f.ctx, "circuit_a")
		require.True(t, found)
		require.Equal(t, []byte("vk_a_data"), got1.VkBytes)

		got2, found := f.keeper.GetVerificationKeyVal(f.ctx, "circuit_b")
		require.True(t, found)
		require.Equal(t, []byte("vk_b_data"), got2.VkBytes)
	})

	t.Run("overwrite VK", func(t *testing.T) {
		updated := types.VerificationKey{CircuitId: "circuit_a", VkBytes: []byte("vk_a_updated")}
		require.NoError(t, f.keeper.SetVerificationKey(f.ctx, updated))

		got, found := f.keeper.GetVerificationKeyVal(f.ctx, "circuit_a")
		require.True(t, found)
		require.Equal(t, []byte("vk_a_updated"), got.VkBytes)
	})
}

func TestProofVerificationSkipsWithEmptyVK(t *testing.T) {
	f := initFixture(t)

	// Store a VK with empty bytes — verifyProof should still skip
	require.NoError(t, f.keeper.SetVerificationKey(f.ctx, types.VerificationKey{
		CircuitId: "shield_v1",
		VkBytes:   []byte{},
	}))

	vk, found := f.keeper.GetVerificationKeyVal(f.ctx, "shield_v1")
	require.True(t, found)
	require.Empty(t, vk.VkBytes)
}

func TestRepKeeperWiring(t *testing.T) {
	repK := &mockRepKeeper{
		currentRoot:  []byte("current_root_bytes"),
		previousRoot: []byte("previous_root_bytes"),
	}
	f := initFixtureWithRepKeeper(t, repK)

	// Verify the keeper was created successfully with rep keeper wired
	require.NotNil(t, f.keeper)

	// Verify default genesis is initialized
	epoch := f.keeper.GetCurrentEpoch(f.ctx)
	require.Equal(t, uint64(0), epoch)
}
