package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func TestRegisterName(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx
	mockRK := f.mockRep
	ms := keeper.NewMsgServerImpl(k)

	// Define Users
	aliceAddr := sdk.AccAddress([]byte("alice_test_address__"))
	alice := aliceAddr.String()

	bobAddr := sdk.AccAddress([]byte("bob_test_address____"))
	bob := bobAddr.String()

	carol := sdk.AccAddress([]byte("carol_test_address__")).String()

	// Helper function for the common setup required by RegisterName
	commonSetup := func(c sdk.Context) {
		mockRK.Reset()
		// Alice is an active x/rep member; bob is not.
		mockRK.SetActiveMember(alice)
		require.NoError(t, k.SetParams(c, types.DefaultParams()))
	}

	tests := []struct {
		desc      string
		msg       *types.MsgRegisterName
		runBefore func(sdk.Context)
		check     func(t *testing.T, ctx sdk.Context)
		err       error
		errCode   codes.Code
	}{
		{
			desc: "Success - Valid Registration",
			msg: &types.MsgRegisterName{
				Authority: alice,
				Name:      "alice",
				Data:      "meta",
			},
			runBefore: func(c sdk.Context) {
				commonSetup(c)
			},
			check: func(t *testing.T, c sdk.Context) {
				rec, err := k.Names.Get(c, "alice")
				require.NoError(t, err)
				require.Equal(t, alice, rec.Owner)

				count, err := k.GetOwnedNamesCount(c, aliceAddr)
				require.NoError(t, err)
				require.Equal(t, uint64(1), count)

				info, err := k.Owners.Get(c, alice)
				require.NoError(t, err)
				require.Equal(t, "alice", info.PrimaryName)

				has, err := k.OwnerNames.Has(c, collections.Join(alice, "alice"))
				require.NoError(t, err)
				require.True(t, has)
			},
		},
		{
			desc: "Failure - Invalid Characters (Regex)",
			msg: &types.MsgRegisterName{
				Authority: alice,
				Name:      "invalid_name!", // '!' is forbidden
				Data:      "meta",
			},
			runBefore: func(c sdk.Context) {
				commonSetup(c)
			},
			err: types.ErrInvalidName,
		},
		{
			desc: "Failure - Starts with Hyphen",
			msg: &types.MsgRegisterName{
				Authority: alice,
				Name:      "-invalid",
				Data:      "meta",
			},
			runBefore: func(c sdk.Context) {
				commonSetup(c)
			},
			err: types.ErrInvalidName,
		},
		{
			desc: "Failure - Name already taken and active",
			msg: &types.MsgRegisterName{
				Authority: alice,
				Name:      "bob",
			},
			runBefore: func(c sdk.Context) {
				commonSetup(c)
				require.NoError(t, k.Names.Set(c, "bob", types.NameRecord{Name: "bob", Owner: bob}))
				require.NoError(t, k.OwnerNames.Set(c, collections.Join(bob, "bob")))

				require.NoError(t, k.Owners.Set(c, bob, types.OwnerInfo{
					Address:        bob,
					LastActiveTime: c.BlockTime().Unix(),
					PrimaryName:    "bob",
				}))
			},
			err: types.ErrNameTaken,
		},
		{
			desc: "Success - Scavenge Expired Name",
			msg: &types.MsgRegisterName{
				Authority: alice,
				Name:      "expired-name",
			},
			runBefore: func(c sdk.Context) {
				commonSetup(c)
				require.NoError(t, k.Names.Set(c, "expired-name", types.NameRecord{Name: "expired-name", Owner: bob, Data: "meta"}))
				require.NoError(t, k.OwnerNames.Set(c, collections.Join(bob, "expired-name")))

				// Set expiration way back (10 years) to guarantee it passes default params check
				expiredTime := c.BlockTime().Add(-(24 * 365 * 10 * time.Hour)).Unix()
				require.NoError(t, k.Owners.Set(c, bob, types.OwnerInfo{
					Address:        bob,
					LastActiveTime: expiredTime,
					PrimaryName:    "expired-name",
				}))
			},
			check: func(t *testing.T, c sdk.Context) {
				rec, err := k.Names.Get(c, "expired-name")
				require.NoError(t, err)
				require.Equal(t, alice, rec.Owner, "Alice should have stolen the name")
			},
		},
		{
			// The old owner pointed the name at an accepted target that set it
			// as primary. Scavenging writes a fresh record without a target,
			// so the target's acceptance and primary must go with it —
			// otherwise ReverseResolve(target) keeps showing a name the
			// scavenger now controls.
			desc: "Success - Scavenge Clears Old Accepted Target",
			msg: &types.MsgRegisterName{
				Authority: alice,
				Name:      "hijacked",
			},
			runBefore: func(c sdk.Context) {
				commonSetup(c)
				require.NoError(t, k.Names.Set(c, "hijacked", types.NameRecord{
					Name: "hijacked", Owner: bob, Target: carol, TargetAccepted: true,
				}))
				require.NoError(t, k.OwnerNames.Set(c, collections.Join(bob, "hijacked")))
				require.NoError(t, k.AcceptedTargets.Set(c, collections.Join(carol, "hijacked")))
				expiredTime := c.BlockTime().Add(-(24 * 365 * 10 * time.Hour)).Unix()
				require.NoError(t, k.Owners.Set(c, bob, types.OwnerInfo{Address: bob, LastActiveTime: expiredTime}))
				require.NoError(t, k.Owners.Set(c, carol, types.OwnerInfo{
					Address: carol, LastActiveTime: c.BlockTime().Unix(), PrimaryName: "hijacked",
				}))
			},
			check: func(t *testing.T, c sdk.Context) {
				rec, err := k.Names.Get(c, "hijacked")
				require.NoError(t, err)
				require.Equal(t, alice, rec.Owner)
				require.Empty(t, rec.Target)

				has, err := k.AcceptedTargets.Has(c, collections.Join(carol, "hijacked"))
				require.NoError(t, err)
				require.False(t, has, "old accepted-target entry must be removed")

				info, err := k.Owners.Get(c, carol)
				require.NoError(t, err)
				require.Empty(t, info.PrimaryName, "old target's primary must be cleared")

				_, err = keeper.NewQueryServerImpl(k).ReverseResolve(c, &types.QueryReverseResolveRequest{Address: carol})
				require.Equal(t, codes.NotFound, status.Code(err))
			},
		},
		{
			desc: "Failure - Max Names Limit Reached",
			msg: &types.MsgRegisterName{
				Authority: alice,
				Name:      "name3",
			},
			runBefore: func(c sdk.Context) {
				commonSetup(c)

				// FORCE the limit to 2 for this test
				params := types.DefaultParams()
				params.MaxNamesPerAddress = 2
				err := k.SetParams(c, params)
				require.NoError(t, err)

				// Create 2 existing names
				require.NoError(t, k.Names.Set(c, "name1", types.NameRecord{Name: "name1", Owner: alice}))
				require.NoError(t, k.OwnerNames.Set(c, collections.Join(alice, "name1")))
				require.NoError(t, k.Names.Set(c, "name2", types.NameRecord{Name: "name2", Owner: alice}))
				require.NoError(t, k.OwnerNames.Set(c, collections.Join(alice, "name2")))

				require.NoError(t, k.Owners.Set(c, alice, types.OwnerInfo{
					Address:        alice,
					LastActiveTime: c.BlockTime().Unix(),
					PrimaryName:    "name1",
				}))
			},
			err: types.ErrTooManyNames,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			cacheCtx, _ := ctx.CacheContext()

			if tc.runBefore != nil {
				tc.runBefore(cacheCtx)
			}

			_, err := ms.RegisterName(cacheCtx, tc.msg)

			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else if tc.errCode != codes.OK {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				require.Equal(t, tc.errCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.check != nil {
					tc.check(t, cacheCtx)
				}
			}
		})
	}
}

// TestRegisterNameBurnsFee: the registration fee is burned, like every other
// creation fee, instead of accumulating in the x/name module account (NAM-07).
func TestRegisterNameBurnsFee(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	alice := sdk.AccAddress([]byte("alice_test_address__")).String()
	f.mockRep.SetActiveMember(alice)
	params := types.DefaultParams()
	require.NoError(t, f.keeper.SetParams(f.ctx, params))
	f.mockBank.Reset()

	_, err := ms.RegisterName(f.ctx, &types.MsgRegisterName{Authority: alice, Name: "phoenix"})
	require.NoError(t, err)

	fee := sdk.NewCoins(sdk.NewCoin(f.keeper.BondDenom(f.ctx), params.RegistrationFeeAmount))
	require.False(t, fee.IsZero())
	require.Equal(t, fee, f.mockBank.Burned[types.ModuleName], "fee burned")
	require.True(t, f.mockBank.ToModule[types.ModuleName].IsZero(), "nothing left in the module account")
}
