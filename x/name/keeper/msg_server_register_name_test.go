package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonstypes "sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func TestRegisterName(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx
	mockCK := f.mockCommons
	ms := keeper.NewMsgServerImpl(k)

	// Define Users
	aliceAddr := sdk.AccAddress([]byte("alice_test_address__"))
	alice := aliceAddr.String()

	bobAddr := sdk.AccAddress([]byte("bob_test_address____"))
	bob := bobAddr.String()

	// Helper function for the common setup required by RegisterName
	commonSetup := func(c sdk.Context) {
		mockCK.Reset()

		// 1. Setup CommonsKeeper mock with council and membership
		mockCK.Groups["Commons Council"] = commonstypes.Group{
			GroupId:       1,
			PolicyAddress: f.councilAddr,
		}
		// 2. Setup membership via CommonsKeeper.HasMember
		mockCK.Members["Commons Council|"+alice] = true
		mockCK.Members["Commons Council|"+bob] = false

		// 3. Set default params
		k.SetParams(c, types.DefaultParams())
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
				_ = k.Names.Set(c, "bob", types.NameRecord{Name: "bob", Owner: bob})
				_ = k.OwnerNames.Set(c, collections.Join(bob, "bob"))

				_ = k.Owners.Set(c, bob, types.OwnerInfo{
					Address:        bob,
					LastActiveTime: c.BlockTime().Unix(),
					PrimaryName:    "bob",
				})
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
				_ = k.Names.Set(c, "expired-name", types.NameRecord{Name: "expired-name", Owner: bob, Data: "meta"})
				_ = k.OwnerNames.Set(c, collections.Join(bob, "expired-name"))

				// Set expiration way back (10 years) to guarantee it passes default params check
				expiredTime := c.BlockTime().Add(-(24 * 365 * 10 * time.Hour)).Unix()
				_ = k.Owners.Set(c, bob, types.OwnerInfo{
					Address:        bob,
					LastActiveTime: expiredTime,
					PrimaryName:    "expired-name",
				})
			},
			check: func(t *testing.T, c sdk.Context) {
				rec, err := k.Names.Get(c, "expired-name")
				require.NoError(t, err)
				require.Equal(t, alice, rec.Owner, "Alice should have stolen the name")
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
				_ = k.Names.Set(c, "name1", types.NameRecord{Name: "name1", Owner: alice})
				_ = k.OwnerNames.Set(c, collections.Join(alice, "name1"))
				_ = k.Names.Set(c, "name2", types.NameRecord{Name: "name2", Owner: alice})
				_ = k.OwnerNames.Set(c, collections.Join(alice, "name2"))

				_ = k.Owners.Set(c, alice, types.OwnerInfo{
					Address:        alice,
					LastActiveTime: c.BlockTime().Unix(),
					PrimaryName:    "name1",
				})
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
