package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestEndorseCollection(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) uint64
		creator        string
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, collID uint64)
	}{
		{
			name: "success member endorses PENDING to ACTIVE",
			setup: func(f *testFixture) uint64 {
				// nonMember creates a PENDING collection
				collID := f.createPendingCollection(t)
				// Owner (nonMember) sets seeking endorsement
				_, err := f.msgServer.SetSeekingEndorsement(f.ctx, &types.MsgSetSeekingEndorsement{
					Creator:      f.nonMember,
					CollectionId: collID,
					Seeking:      true,
				})
				require.NoError(t, err)
				return collID
			},
			creator: "", // f.member (who IS a member and NOT the owner)
			check: func(t *testing.T, f *testFixture, collID uint64) {
				coll, err := f.keeper.Collection.Get(f.ctx, collID)
				require.NoError(t, err)
				require.Equal(t, types.CollectionStatus_COLLECTION_STATUS_ACTIVE, coll.Status)
				require.Equal(t, f.member, coll.EndorsedBy)
				require.True(t, coll.Immutable)

				// Verify endorsement record exists
				endorsement, err := f.keeper.Endorsement.Get(f.ctx, collID)
				require.NoError(t, err)
				require.Equal(t, f.member, endorsement.Endorser)
				require.False(t, endorsement.StakeReleased)
			},
		},
		{
			name: "error not member",
			setup: func(f *testFixture) uint64 {
				collID := f.createPendingCollection(t)
				_, err := f.msgServer.SetSeekingEndorsement(f.ctx, &types.MsgSetSeekingEndorsement{
					Creator:      f.nonMember,
					CollectionId: collID,
					Seeking:      true,
				})
				require.NoError(t, err)

				// Create a second non-member to try endorsing
				return collID
			},
			creator:        "nonMember2",
			expErr:         true,
			expErrContains: "not an active x/rep member",
		},
		{
			name: "error collection not PENDING",
			setup: func(f *testFixture) uint64 {
				// Create an ACTIVE collection (member-owned = auto ACTIVE)
				return f.createCollection(t, f.owner)
			},
			creator:        "", // f.member
			expErr:         true,
			expErrContains: "PENDING",
		},
		{
			name: "error is owner (cannot endorse self)",
			setup: func(f *testFixture) uint64 {
				collID := f.createPendingCollection(t)
				_, err := f.msgServer.SetSeekingEndorsement(f.ctx, &types.MsgSetSeekingEndorsement{
					Creator:      f.nonMember,
					CollectionId: collID,
					Seeking:      true,
				})
				require.NoError(t, err)
				return collID
			},
			creator:        "nonMember", // nonMember is the owner
			expErr:         true,
			expErrContains: "not an active x/rep member",
		},
		{
			name: "error not seeking endorsement",
			setup: func(f *testFixture) uint64 {
				// Create pending but do NOT set seeking_endorsement
				return f.createPendingCollection(t)
			},
			creator:        "", // f.member
			expErr:         true,
			expErrContains: "not seeking endorsement",
		},
		{
			name: "error already endorsed - status changed to ACTIVE",
			setup: func(f *testFixture) uint64 {
				collID := f.createPendingCollection(t)
				_, err := f.msgServer.SetSeekingEndorsement(f.ctx, &types.MsgSetSeekingEndorsement{
					Creator:      f.nonMember,
					CollectionId: collID,
					Seeking:      true,
				})
				require.NoError(t, err)
				// First endorsement transitions to ACTIVE
				_, err = f.msgServer.EndorseCollection(f.ctx, &types.MsgEndorseCollection{
					Creator:      f.member,
					CollectionId: collID,
				})
				require.NoError(t, err)
				return collID
			},
			creator:        "sentinel", // another member tries
			expErr:         true,
			expErrContains: "PENDING", // fails PENDING check since status is now ACTIVE
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)

			collID := tc.setup(f)

			creator := tc.creator
			switch creator {
			case "":
				creator = f.member
			case "nonMember":
				creator = f.nonMember
			case "nonMember2":
				// A truly unknown address that is not a member
				creator = f.nonMember
			case "sentinel":
				creator = f.sentinel
			}

			resp, err := f.msgServer.EndorseCollection(f.ctx, &types.MsgEndorseCollection{
				Creator:      creator,
				CollectionId: collID,
			})

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
