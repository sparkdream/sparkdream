package keeper_test

import (
	"context"
	"errors"
	"testing"
	"time"

	corestore "cosmossdk.io/core/store"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govtypesv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	"github.com/stretchr/testify/require"

	guardiankeeper "sparkdream/x/guardian/keeper"
	guardianmodule "sparkdream/x/guardian/module"
	guardiantypes "sparkdream/x/guardian/types"
)

// APP-201: the gov voting-period floors follow the network. devnet ships
// voting_period 1 h / expedited 30 min, which the production floors (6 h /
// 1 h) would refuse, forcing every devnet gov retune to lengthen its voting
// periods as a side effect. Each network's floors must admit its own
// shipped cadence; the production floors must still refuse the devnet one.
func TestGovVotingPeriodFloorsPerNetwork(t *testing.T) {
	devnetShipped := func(p *govtypesv1.Params) {
		vp, evp := time.Hour, 30*time.Minute
		p.VotingPeriod, p.ExpeditedVotingPeriod = &vp, &evp
	}
	cases := []struct {
		network string
		mutate  func(*govtypesv1.Params)
		wantErr string
	}{
		{"devnet", devnetShipped, ""},
		{"testnet", devnetShipped, "voting_period below floor"},
		{"mainnet", devnetShipped, "voting_period below floor"},
		{"devnet", func(p *govtypesv1.Params) {
			vp := 29 * time.Minute
			p.VotingPeriod = &vp
		}, "voting_period below floor"},
		{"devnet", func(p *govtypesv1.Params) {
			evp := 9 * time.Minute
			p.ExpeditedVotingPeriod = &evp
		}, "expedited_voting_period below floor"},
		{"mainnet", func(p *govtypesv1.Params) {
			vp, evp := 6*time.Hour, time.Hour
			p.VotingPeriod, p.ExpeditedVotingPeriod = &vp, &evp
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.network+"/"+tc.wantErr, func(t *testing.T) {
			r := &okRouter{}
			ctx, k := setupGuardian(t, guardianDeps{identity: mockIdentityKeeper{sealed: newValidSealed()}, router: r})
			require.NoError(t, k.UseNetworkBounds(tc.network))
			ms := guardiankeeper.NewMsgServerImpl(k)
			p := validGovParams()
			tc.mutate(&p)
			any, err := codectypes.NewAnyWithValue(&govtypesv1.MsgUpdateParams{Params: p})
			require.NoError(t, err)
			_, err = ms.Exec(ctx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
			if tc.wantErr == "" {
				require.NoError(t, err)
				require.Equal(t, 1, r.routed)
				return
			}
			require.ErrorIs(t, err, guardiantypes.ErrImmutableField)
			require.Contains(t, err.Error(), tc.wantErr)
			require.Zero(t, r.routed)
		})
	}
}

// The bounds a fresh keeper enforces are the build tag's network entry, and
// every network in the table has floors (a zero floor would admit a 1 s
// voting period). Run under -tags devnet|testnet|mainnet by
// `make test-production`.
func TestNetworkBoundsActive(t *testing.T) {
	_, k := setupGuardian(t, guardianDeps{})
	want, err := guardiankeeper.NetworkBoundsFor(guardiankeeper.ActiveNetwork())
	require.NoError(t, err)
	require.Equal(t, want, k.NetworkBounds())

	require.Equal(t, []string{"devnet", "mainnet", "testnet"}, guardiankeeper.NetworkNames())
	for _, n := range guardiankeeper.NetworkNames() {
		b, err := guardiankeeper.NetworkBoundsFor(n)
		require.NoError(t, err)
		require.Positive(t, b.GovVotingPeriodFloor, n)
		require.Positive(t, b.GovExpeditedVotingPeriodFloor, n)
		require.Less(t, b.GovExpeditedVotingPeriodFloor, b.GovVotingPeriodFloor, n)
	}
	require.Error(t, k.UseNetworkBounds("phoenix"), "an unknown network has no bounds")
	require.Equal(t, want, k.NetworkBounds(), "a failed switch leaves the bounds untouched")
}

// --- cooldown read fails closed ---------------------------------------------

// failingKVStoreService wraps a store service so every Get fails, standing in
// for a corrupt or unreadable guardian store.
type failingKVStoreService struct {
	inner corestore.KVStoreService
	err   error
}

func (f failingKVStoreService) OpenKVStore(ctx context.Context) corestore.KVStore {
	return failingKVStore{KVStore: f.inner.OpenKVStore(ctx), err: f.err}
}

type failingKVStore struct {
	corestore.KVStore
	err error
}

func (f failingKVStore) Get([]byte) ([]byte, error) { return nil, f.err }

// A blocks_per_year change whose cooldown record cannot be read is refused,
// not waved through as "never changed" (the independent review's fail-open
// finding); the error is the store's, not ErrNotFound.
func TestBlocksPerYearCooldownReadFailsClosed(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(guardianmodule.AppModule{})
	minttypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	storeKey := storetypes.NewKVStoreKey(guardiantypes.StoreKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx
	storeErr := errors.New("phoenix: store unreadable")
	r := &okRouter{}
	k := guardiankeeper.NewKeeper(
		failingKVStoreService{inner: runtime.NewKVStoreService(storeKey), err: storeErr},
		encCfg.Codec,
		addresscodec.NewBech32Codec("sprkdrm"),
		r,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
	)
	cur := currentMintParams()
	k.SetMintKeeper(mockMintKeeper{params: cur})
	ms := guardiankeeper.NewMsgServerImpl(&k)
	sdkCtx := ctx.WithBlockTime(time.Unix(1_800_000_000, 0))

	exec := func(bpy uint64) error {
		p := cur
		p.BlocksPerYear = bpy
		any, err := codectypes.NewAnyWithValue(&minttypes.MsgUpdateParams{Params: p})
		require.NoError(t, err)
		_, err = ms.Exec(sdkCtx, &guardiantypes.MsgExec{Authority: govAuth(), Inner: any})
		return err
	}
	err := exec(cur.BlocksPerYear + cur.BlocksPerYear/4)
	require.ErrorIs(t, err, storeErr)
	require.Contains(t, err.Error(), "blocks_per_year cooldown read")
	require.Zero(t, r.routed, "the inner update is not routed")
	require.NoError(t, exec(cur.BlocksPerYear), "an unchanged value never reads the cooldown")
	require.Equal(t, 1, r.routed)
}
