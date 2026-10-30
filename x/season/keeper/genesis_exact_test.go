package keeper_test

import (
	"bytes"
	"context"
	"testing"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// requireSameStore asserts two module stores hold the same key/value pairs,
// ignoring keys under skip (state genesis deliberately does not carry).
func requireSameStore(t *testing.T, a context.Context, aKey *storetypes.KVStoreKey, b context.Context, bKey *storetypes.KVStoreKey, skip ...[]byte) {
	t.Helper()
	dump := func(ctx context.Context, key *storetypes.KVStoreKey) map[string]string {
		out := map[string]string{}
		it := sdk.UnwrapSDKContext(ctx).KVStore(key).Iterator(nil, nil)
		defer it.Close()
		for ; it.Valid(); it.Next() {
			skipped := false
			for _, p := range skip {
				if bytes.HasPrefix(it.Key(), p) {
					skipped = true
				}
			}
			if !skipped {
				out[string(it.Key())] = string(it.Value())
			}
		}
		return out
	}
	require.Equal(t, dump(a, aKey), dump(b, bKey), "store differs after a genesis round trip")
}

// TestGenesisRoundTripIsExact: export then import reproduces the store byte
// for byte. Covers the transition items (absent ones must stay absent, not
// come back as empty records) and the guild counters (a guild emptied by a
// kick or dissolution holds no zero counter, as InitGenesis rebuilds only
// non-zero ones).
func TestGenesisRoundTripIsExact(t *testing.T) {
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx)
	k := f.keeper
	gs := types.DefaultGenesis()
	gs.Season = &types.Season{Number: 1, EndBlock: 10_000_000, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}
	require.NoError(t, k.InitGenesis(ctx, *gs))
	ms := keeper.NewMsgServerImpl(k)
	founder, _ := f.addressCodec.BytesToString(TestAddrFounder)
	m1, _ := f.addressCodec.BytesToString(TestAddrMember1)
	for _, a := range []sdk.AccAddress{TestAddrFounder, TestAddrMember1, TestAddrMember2} {
		SetupBasicMemberProfile(t, k, ctx, a)
	}
	params, _ := k.Params.Get(ctx)
	params.MinGuildAgeEpochs = 0
	require.NoError(t, k.Params.Set(ctx, params))

	// Guild 1 is emptied by dissolution, guild 2 keeps its founder.
	_, err := ms.CreateGuild(ctx, &types.MsgCreateGuild{Creator: founder, Name: "phoenix-guild", Description: "d"})
	require.NoError(t, err)
	_, err = ms.DissolveGuild(ctx, &types.MsgDissolveGuild{Creator: founder, GuildId: 1})
	require.NoError(t, err)
	_, err = ms.CreateGuild(ctx, &types.MsgCreateGuild{Creator: m1, Name: "aurora-guild", Description: "d"})
	require.NoError(t, err)

	has, err := k.GuildMemberCount.Has(ctx, 1)
	require.NoError(t, err)
	require.False(t, has, "an emptied guild holds no zero counter")

	exported, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	require.Nil(t, exported.SeasonTransitionState, "an absent item exports as nil")
	require.Nil(t, exported.TransitionRecoveryState)
	require.Nil(t, exported.NextSeasonInfo)

	f2 := initFixture(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, *exported))
	requireSameStore(t, f.ctx, f.storeKey, f2.ctx, f2.storeKey, types.PruneCursorKey)
}
