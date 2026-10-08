package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
	shieldtypes "sparkdream/x/shield/types"
)

func TestCreateCollection(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture)
		msg            *types.MsgCreateCollection
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, resp *types.MsgCreateCollectionResponse)
	}{
		{
			name: "member creates permanent PUBLIC collection (deposit burned)",
			msg: &types.MsgCreateCollection{
				Creator:    "", // filled in setup
				Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
				Visibility: types.Visibility_VISIBILITY_PUBLIC,
				Name:       "my-collection",
			},
			setup: func(f *testFixture) {
				// msg Creator will be set dynamically
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, resp *types.MsgCreateCollectionResponse) {
				coll, err := f.keeper.Collection.Get(f.ctx, resp.Id)
				require.NoError(t, err)
				require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)
				require.Equal(t, int64(0), coll.ExpiresAt)
				require.True(t, coll.DepositBurned)
				require.Equal(t, "my-collection", coll.Name)

				// Owner index set
				has, err := f.keeper.CollectionsByOwner.Has(f.ctx, collections.Join(f.owner, resp.Id))
				require.NoError(t, err)
				require.True(t, has)

				// Status index set. Key is (status, pinned-rank, id); a fresh
				// collection is unpinned, so pinned-rank is 1.
				has, err = f.keeper.CollectionsByStatus.Has(f.ctx, collections.Join3(int32(types.CollectionStatus_COLLECTION_STATUS_ACTIVE), int32(1), resp.Id))
				require.NoError(t, err)
				require.True(t, has)
			},
		},
		{
			name: "member creates TTL collection (deposit escrowed, expiry index set)",
			setup: func(f *testFixture) {
				f.setBlockHeight(100)
			},
			msg: &types.MsgCreateCollection{
				Creator:    "", // filled dynamically
				Type:       types.CollectionType_COLLECTION_TYPE_LINK,
				Visibility: types.Visibility_VISIBILITY_PUBLIC,
				Name:       "ttl-collection",
				ExpiresAt:  10100,
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, resp *types.MsgCreateCollectionResponse) {
				coll, err := f.keeper.Collection.Get(f.ctx, resp.Id)
				require.NoError(t, err)
				require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)
				require.Equal(t, int64(10100), coll.ExpiresAt)
				require.False(t, coll.DepositBurned)

				// Expiry index set
				has, err := f.keeper.CollectionsByExpiry.Has(f.ctx, collections.Join(int64(10100), resp.Id))
				require.NoError(t, err)
				require.True(t, has)
			},
		},
		{
			name: "non-member creates PENDING TTL collection (endorsement fee escrowed)",
			setup: func(f *testFixture) {
				f.setBlockHeight(100)
			},
			msg: &types.MsgCreateCollection{
				Creator:    "", // will be nonMember
				Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
				Visibility: types.Visibility_VISIBILITY_PUBLIC,
				Name:       "pending-coll",
				ExpiresAt:  100100, // blockHeight(100) + 100000 < max_non_member_ttl (432000)
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, resp *types.MsgCreateCollectionResponse) {
				coll, err := f.keeper.Collection.Get(f.ctx, resp.Id)
				require.NoError(t, err)
				require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_PENDING, coll.Status)
				require.False(t, coll.DepositBurned)

				// EndorsementPending index set
				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)
				expiryBlock := int64(100) + params.EndorsementExpiryBlocks
				has, err := f.keeper.EndorsementPending.Has(f.ctx, collections.Join(expiryBlock, resp.Id))
				require.NoError(t, err)
				require.True(t, has)
			},
		},
		{
			name: "error: non-member without TTL",
			msg: &types.MsgCreateCollection{
				Creator:    "", // will be nonMember
				Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
				Visibility: types.Visibility_VISIBILITY_PUBLIC,
				Name:       "permanent-coll",
				ExpiresAt:  0,
			},
			expErr:         true,
			expErrContains: "non-members cannot create permanent collections",
		},
		{
			name: "error: non-member TTL exceeds max",
			setup: func(f *testFixture) {
				f.setBlockHeight(100)
			},
			msg: &types.MsgCreateCollection{
				Creator:    "", // will be nonMember
				Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
				Visibility: types.Visibility_VISIBILITY_PUBLIC,
				Name:       "long-ttl",
				ExpiresAt:  600000, // 600000 - 100 = 599900 > 432000
			},
			expErr:         true,
			expErrContains: "non-member TTL exceeds max",
		},
		{
			name: "error: max collections exceeded",
			setup: func(f *testFixture) {
				// Default max_collections_base=5, trust ESTABLISHED idx=3, per_trust=15 => max = 5+3*15 = 50
				// Override params to make limit small
				params, _ := f.keeper.Params.Get(f.ctx)
				params.MaxCollectionsBase = 1
				params.MaxCollectionsPerTrustLevel = 0
				f.keeper.Params.Set(f.ctx, params)

				// Create one collection to hit the limit
				f.createCollection(t, f.owner)
			},
			msg: &types.MsgCreateCollection{
				Creator:    "", // will be owner
				Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
				Visibility: types.Visibility_VISIBILITY_PUBLIC,
				Name:       "over-limit",
			},
			expErr:         true,
			expErrContains: "tiered collection limit",
		},
		{
			name: "error: empty name",
			msg: &types.MsgCreateCollection{
				Creator:    "", // will be owner
				Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
				Visibility: types.Visibility_VISIBILITY_PUBLIC,
				Name:       "",
			},
			expErr:         true,
			expErrContains: "name empty or exceeds max length",
		},
		{
			name:  "sequence increments correctly",
			setup: func(f *testFixture) {},
			msg: &types.MsgCreateCollection{
				Creator:    "", // will be owner
				Type:       types.CollectionType_COLLECTION_TYPE_NFT,
				Visibility: types.Visibility_VISIBILITY_PUBLIC,
				Name:       "seq-test",
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, resp *types.MsgCreateCollectionResponse) {
				firstID := resp.Id
				// Create a second collection
				resp2, err := f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
					Creator:    f.owner,
					Type:       types.CollectionType_COLLECTION_TYPE_NFT,
					Visibility: types.Visibility_VISIBILITY_PUBLIC,
					Name:       "seq-test-2",
				})
				require.NoError(t, err)
				require.Equal(t, firstID+1, resp2.Id)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)

			// Determine which creator to use based on test name
			isNonMemberTest := tc.expErrContains == "non-members cannot create permanent collections" ||
				tc.expErrContains == "non-member TTL exceeds max"
			isNonMemberSuccess := tc.name == "non-member creates PENDING TTL collection (endorsement fee escrowed)"

			if isNonMemberTest || isNonMemberSuccess {
				tc.msg.Creator = f.nonMember
			} else {
				tc.msg.Creator = f.owner
			}

			if tc.setup != nil {
				tc.setup(f)
			}

			resp, err := f.msgServer.CreateCollection(f.ctx, tc.msg)
			if tc.expErr {
				require.Error(t, err)
				if tc.expErrContains != "" {
					require.Contains(t, err.Error(), tc.expErrContains)
				}
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			if tc.check != nil {
				tc.check(t, f, resp)
			}
		})
	}
}

func TestCreateCollectionDepositAmount(t *testing.T) {
	f := initTestFixture(t)

	// Track escrow calls
	var escrowCalled bool
	var escrowAmount math.Int
	f.bankKeeper.sendCoinsFromAccountToModuleFn = func(_ context.Context, sender sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
		escrowCalled = true
		escrowAmount = amt.AmountOf("uspark")
		return nil
	}

	// Create TTL collection (escrowed, not burned)
	f.setBlockHeight(100)
	_, err := f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
		Creator:    f.owner,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "deposit-test",
		ExpiresAt:  10100,
	})
	require.NoError(t, err)
	require.True(t, escrowCalled)

	params, _ := f.keeper.Params.Get(f.ctx)
	require.Equal(t, params.BaseCollectionDeposit, escrowAmount)
}

// Anonymous (shield-routed) collections are public, unencrypted and always
// have a TTL. They also skip the per-owner collection limit: every anonymous
// collection shares the shield address as owner, so that limit would be one
// chain-wide cap (x/shield's epoch-scoped nullifier bounds each member).
func TestCreateCollection_Anonymous(t *testing.T) {
	f := initTestFixture(t)
	shield := authtypes.NewModuleAddress("shield").String()
	expires := f.sdkCtx.BlockHeight() + 1000

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxCollectionsBase = 1
	params.MaxCollectionsPerTrustLevel = 0
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	var n byte
	anon := func(mut func(*types.MsgCreateCollection)) error {
		n++
		msg := &types.MsgCreateCollection{
			Creator:    shield,
			Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
			Visibility: types.Visibility_VISIBILITY_PUBLIC,
			Name:       "phoenix",
			ExpiresAt:  expires,
		}
		if mut != nil {
			mut(msg)
		}
		_, err := f.msgServer.CreateCollection(anonCreateCtx(f, n), msg)
		return err
	}

	require.ErrorIs(t, anon(func(m *types.MsgCreateCollection) { m.ExpiresAt = 0 }), types.ErrAnonymousPermanent)
	bond := math.NewInt(5)
	require.ErrorIs(t, anon(func(m *types.MsgCreateCollection) { m.AuthorBond = &bond }), types.ErrAnonymousAuthorBond)
	require.ErrorIs(t, anon(func(m *types.MsgCreateCollection) {
		m.Visibility = types.Visibility_VISIBILITY_PRIVATE
		m.Encrypted = true
		m.Name = ""
		m.EncryptedData = []byte("aurora")
	}), types.ErrAnonymousMustBePublic)

	// Past the per-owner limit of 1.
	for i := 0; i < 3; i++ {
		require.NoError(t, anon(nil), "anonymous collection %d", i+1)
	}

	// An identified member still hits the limit.
	f.createCollection(t, f.owner)
	_, err = f.msgServer.CreateCollection(f.ctx, &types.MsgCreateCollection{
		Creator:    f.owner,
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "zenith",
	})
	require.ErrorIs(t, err, types.ErrMaxCollections)
}

// An expired anonymous collection whose conviction meets the threshold is
// renewed instead of pruned; below the threshold it is pruned.
func TestPruneExpired_AnonymousConvictionRenewal(t *testing.T) {
	f := initTestFixture(t)
	shield := authtypes.NewModuleAddress("shield").String()

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.ConvictionRenewalThreshold = math.LegacyNewDec(10)
	params.ConvictionRenewalPeriod = 500
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	expires := f.sdkCtx.BlockHeight() + 10
	renewed := createAnonCollection(t, f, 1, expires)
	dropped := createAnonCollection(t, f, 2, expires)
	_ = shield

	f.repKeeper.getContentConvictionFn = func(_ context.Context, _ reptypes.StakeTargetType, id uint64) (math.LegacyDec, error) {
		if id == renewed {
			return math.LegacyNewDec(10), nil
		}
		return math.LegacyNewDec(9), nil
	}

	f.setBlockHeight(expires + 1)
	require.NoError(t, f.keeper.PruneExpired(f.ctx))

	coll, err := f.keeper.Collection.Get(f.ctx, renewed)
	require.NoError(t, err, "a sustained anonymous collection is renewed")
	require.Equal(t, expires+1+500, coll.ExpiresAt)
	require.True(t, coll.ConvictionSustained)

	_, err = f.keeper.Collection.Get(f.ctx, dropped)
	require.Error(t, err, "below the threshold the collection is pruned")
}

// anonCreateCtx is the context x/shield runs an anonymous create in: the proven
// trust level and the consumed creation nullifier (distinct per seed).
func anonCreateCtx(f *testFixture, seed byte) context.Context {
	nullifier := make([]byte, 32)
	nullifier[31] = seed
	ctx := shieldtypes.WithProvenTrustLevel(f.sdkCtx, 1)
	return shieldtypes.WithExecNullifier(ctx, shieldtypes.ExecNullifier{Domain: 21, Scope: uint64(seed), Nullifier: nullifier})
}

// createAnonCollection creates a public TTL collection through the anonymous
// path, owned by the shield address with owner tag seeded by seed.
func createAnonCollection(t *testing.T, f *testFixture, seed byte, expiresAt int64) uint64 {
	t.Helper()
	resp, err := f.msgServer.CreateCollection(anonCreateCtx(f, seed), &types.MsgCreateCollection{
		Creator:    authtypes.NewModuleAddress("shield").String(),
		Type:       types.CollectionType_COLLECTION_TYPE_MIXED,
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Name:       "phoenix",
		ExpiresAt:  expiresAt,
	})
	require.NoError(t, err)
	return resp.Id
}
