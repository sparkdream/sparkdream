package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestSetSeekingEndorsement(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) uint64
		msg            func(f *testFixture, collID uint64) *types.MsgSetSeekingEndorsement
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, collID uint64)
	}{
		{
			name: "success owner sets seeking true",
			setup: func(f *testFixture) uint64 {
				return f.createPendingCollection(t)
			},
			msg: func(f *testFixture, collID uint64) *types.MsgSetSeekingEndorsement {
				return &types.MsgSetSeekingEndorsement{
					Creator:      f.nonMember, // nonMember is the PENDING collection owner
					CollectionId: collID,
					Seeking:      true,
				}
			},
			check: func(t *testing.T, f *testFixture, collID uint64) {
				coll, err := f.keeper.Collection.Get(f.ctx, collID)
				require.NoError(t, err)
				require.True(t, coll.SeekingEndorsement)
			},
		},
		{
			name: "success owner sets seeking false",
			setup: func(f *testFixture) uint64 {
				collID := f.createPendingCollection(t)
				// First set to true
				_, err := f.msgServer.SetSeekingEndorsement(f.ctx, &types.MsgSetSeekingEndorsement{
					Creator:      f.nonMember,
					CollectionId: collID,
					Seeking:      true,
				})
				require.NoError(t, err)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgSetSeekingEndorsement {
				return &types.MsgSetSeekingEndorsement{
					Creator:      f.nonMember,
					CollectionId: collID,
					Seeking:      false,
				}
			},
			check: func(t *testing.T, f *testFixture, collID uint64) {
				coll, err := f.keeper.Collection.Get(f.ctx, collID)
				require.NoError(t, err)
				require.False(t, coll.SeekingEndorsement)
			},
		},
		{
			name: "error not owner",
			setup: func(f *testFixture) uint64 {
				return f.createPendingCollection(t)
			},
			msg: func(f *testFixture, collID uint64) *types.MsgSetSeekingEndorsement {
				return &types.MsgSetSeekingEndorsement{
					Creator:      f.member, // member is not the owner
					CollectionId: collID,
					Seeking:      true,
				}
			},
			expErr:         true,
			expErrContains: "unauthorized",
		},
		{
			name: "error not PENDING (active collection)",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner) // ACTIVE
			},
			msg: func(f *testFixture, collID uint64) *types.MsgSetSeekingEndorsement {
				return &types.MsgSetSeekingEndorsement{
					Creator:      f.owner,
					CollectionId: collID,
					Seeking:      true,
				}
			},
			expErr:         true,
			expErrContains: "PENDING",
		},
		{
			name: "error already endorsed - collection now ACTIVE so fails PENDING check",
			setup: func(f *testFixture) uint64 {
				collID := f.createPendingCollection(t)
				// Set seeking
				_, err := f.msgServer.SetSeekingEndorsement(f.ctx, &types.MsgSetSeekingEndorsement{
					Creator:      f.nonMember,
					CollectionId: collID,
					Seeking:      true,
				})
				require.NoError(t, err)
				// Endorse it (transitions to ACTIVE)
				_, err = f.msgServer.EndorseCollection(f.ctx, &types.MsgEndorseCollection{
					Creator:      f.member,
					CollectionId: collID,
				})
				require.NoError(t, err)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgSetSeekingEndorsement {
				return &types.MsgSetSeekingEndorsement{
					Creator:      f.nonMember,
					CollectionId: collID,
					Seeking:      true,
				}
			},
			expErr:         true,
			expErrContains: "PENDING",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)

			collID := tc.setup(f)
			msg := tc.msg(f, collID)

			resp, err := f.msgServer.SetSeekingEndorsement(f.ctx, msg)

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
				tc.check(t, f, collID)
			}
		})
	}
}
