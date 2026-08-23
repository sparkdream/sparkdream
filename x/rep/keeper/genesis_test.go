package keeper_test

import (
	"testing"

	"sparkdream/x/rep/types"

	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params:    types.DefaultParams(),
		MemberMap: []types.Member{{Address: "0"}, {Address: "1"}}, InvitationList: []types.Invitation{{Id: 0}, {Id: 1}},
		InvitationCount: 2,
		ProjectList:     []types.Project{{Id: 0}, {Id: 1}},
		ProjectCount:    2,
		InitiativeList:  []types.Initiative{{Id: 0}, {Id: 1}},
		InitiativeCount: 2,
		StakeList:       []types.Stake{{Id: 0}, {Id: 1}},
		StakeCount:      2,
		ChallengeList:   []types.Challenge{{Id: 0}, {Id: 1}},
		ChallengeCount:  2,
		JuryReviewList:  []types.JuryReview{{Id: 0}, {Id: 1}},
		JuryReviewCount: 2,
		InterimList:     []types.Interim{{Id: 0}, {Id: 1}},
		InterimCount:    2,
	}
	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.EqualExportedValues(t, genesisState.Params, got.Params)
	require.EqualExportedValues(t, genesisState.MemberMap, got.MemberMap)
	require.EqualExportedValues(t, genesisState.InvitationList, got.InvitationList)
	require.Equal(t, genesisState.InvitationCount, got.InvitationCount)
	require.EqualExportedValues(t, genesisState.ProjectList, got.ProjectList)
	require.Equal(t, genesisState.ProjectCount, got.ProjectCount)
	require.EqualExportedValues(t, genesisState.InitiativeList, got.InitiativeList)
	require.Equal(t, genesisState.InitiativeCount, got.InitiativeCount)
	require.EqualExportedValues(t, genesisState.StakeList, got.StakeList)
	require.Equal(t, genesisState.StakeCount, got.StakeCount)
	require.EqualExportedValues(t, genesisState.ChallengeList, got.ChallengeList)
	require.Equal(t, genesisState.ChallengeCount, got.ChallengeCount)
	require.EqualExportedValues(t, genesisState.JuryReviewList, got.JuryReviewList)
	require.Equal(t, genesisState.JuryReviewCount, got.JuryReviewCount)
	require.EqualExportedValues(t, genesisState.InterimList, got.InterimList)
	require.Equal(t, genesisState.InterimCount, got.InterimCount)

}

// TestGenesis_BondedRoleUnbondingRoundtrip: in-flight unbond state (status
// UNBONDING, PendingUnbondAmount, UnbondCompletionTime) and the per-role
// UnbondCooldown config survive a genesis export/import cycle. Critical for
// chain upgrades and state-export migrations during the cooldown window.
func TestGenesis_BondedRoleUnbondingRoundtrip(t *testing.T) {
	roleAddr := "sprkdrm1ghosthhmidunbond00000000000000000000"
	in := types.GenesisState{
		Params: types.DefaultParams(),
		BondedRoleConfigList: []types.BondedRoleConfig{
			{
				RoleType:          types.RoleType_ROLE_TYPE_CONTENT_SENTINEL,
				MinBond:           "1000",
				MinRepTier:        0,
				MinTrustLevel:     "TRUST_LEVEL_ESTABLISHED",
				MinAgeBlocks:      0,
				DemotionCooldown:  604800,
				DemotionThreshold: "500",
				UnbondCooldown:    1209600,
			},
		},
		BondedRoleList: []types.BondedRole{
			{
				Address:              roleAddr,
				RoleType:             types.RoleType_ROLE_TYPE_CONTENT_SENTINEL,
				BondStatus:           types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING,
				CurrentBond:          "1500",
				TotalCommittedBond:   "0",
				CumulativeRewards:    "0",
				PendingUnbondAmount:  "1500",
				UnbondCompletionTime: 9_999_999_999,
			},
		},
	}

	f := initFixture(t)
	require.NoError(t, f.keeper.InitGenesis(f.ctx, in))

	out, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	// The test fixture pre-seeds three default BondedRoleConfigs via the
	// fixture's own InitGenesis path; our test's InitGenesis call overwrites
	// the FORUM_SENTINEL entry but leaves COLLECT_CURATOR and
	// FEDERATION_VERIFIER seeded. Roundtrip equality is checked per-key.
	require.Len(t, out.BondedRoleList, 1, "single BondedRole record was set")

	gotRole := out.BondedRoleList[0]
	require.Equal(t, in.BondedRoleList[0].Address, gotRole.Address)
	require.Equal(t, in.BondedRoleList[0].BondStatus, gotRole.BondStatus)
	require.Equal(t, in.BondedRoleList[0].PendingUnbondAmount, gotRole.PendingUnbondAmount)
	require.Equal(t, in.BondedRoleList[0].UnbondCompletionTime, gotRole.UnbondCompletionTime)
	require.Equal(t, in.BondedRoleList[0].CurrentBond, gotRole.CurrentBond)

	var gotSentinelCfg *types.BondedRoleConfig
	for i := range out.BondedRoleConfigList {
		if out.BondedRoleConfigList[i].RoleType == types.RoleType_ROLE_TYPE_CONTENT_SENTINEL {
			gotSentinelCfg = &out.BondedRoleConfigList[i]
			break
		}
	}
	require.NotNil(t, gotSentinelCfg, "FORUM_SENTINEL config survives roundtrip")
	require.Equal(t, int64(1209600), gotSentinelCfg.UnbondCooldown)
	require.Equal(t, "TRUST_LEVEL_ESTABLISHED", gotSentinelCfg.MinTrustLevel)
}

// TestGenesis_RoleActivityRoundtrip: the shared accountability record
// (streaks, cooldown, accuracy ring, per-kind counter maps) survives a
// genesis export/import cycle intact. Rings and maps are the fields most at
// risk from lossy encode/decode.
func TestGenesis_RoleActivityRoundtrip(t *testing.T) {
	roleAddr := "sprkdrm1ghosthhmidactivity000000000000000000"
	ring := make([]*types.RoleAccuracyBucket, types.RoleAccuracyRingSize)
	for i := range ring {
		ring[i] = &types.RoleAccuracyBucket{}
	}
	ring[3] = &types.RoleAccuracyBucket{Epoch: 3, Upheld: 5, Overturned: 2}

	in := types.GenesisState{
		Params: types.DefaultParams(),
		RoleActivityList: []types.RoleActivity{
			{
				RoleType:              types.RoleType_ROLE_TYPE_CONTENT_SENTINEL,
				Address:               roleAddr,
				ConsecutiveUpheld:     4,
				ConsecutiveOverturns:  0,
				OverturnCooldownUntil: 12345,
				EpochAppealsResolved:  7,
				AccuracyWindow:        ring,
				EpochActions: map[string]uint64{
					types.ActionKindForumHide:   3,
					types.ActionKindCollectHide: 2,
				},
				TotalActions: map[string]uint64{
					types.ActionKindForumHide:   30,
					types.ActionKindCollectHide: 20,
				},
				UpheldActions:     map[string]uint64{types.ActionKindForumHide: 5},
				OverturnedActions: map[string]uint64{types.ActionKindCollectHide: 2},
			},
		},
	}
	require.NoError(t, in.Validate())

	f := initFixture(t)
	require.NoError(t, f.keeper.InitGenesis(f.ctx, in))

	out, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, out.RoleActivityList, 1)
	got := out.RoleActivityList[0]
	require.Equal(t, in.RoleActivityList[0], got)

	// The imported record is live: the keeper reads flow through it.
	require.Equal(t, int64(12345),
		f.keeper.RoleOverturnCooldownUntil(f.ctx, types.RoleType_ROLE_TYPE_CONTENT_SENTINEL, roleAddr))
	require.Equal(t, uint64(3),
		f.keeper.RoleEpochActionCount(f.ctx, types.RoleType_ROLE_TYPE_CONTENT_SENTINEL, roleAddr, types.ActionKindForumHide))
	up, ov := f.keeper.GetRoleWindowedAccuracy(f.ctx, types.RoleType_ROLE_TYPE_CONTENT_SENTINEL, roleAddr, 3, 4)
	require.Equal(t, uint64(5), up)
	require.Equal(t, uint64(2), ov)
}

// TestGenesis_RoleActivityValidation: duplicate (role_type, address) pairs
// and unspecified role types are rejected at genesis validation.
func TestGenesis_RoleActivityValidation(t *testing.T) {
	base := types.RoleActivity{
		RoleType: types.RoleType_ROLE_TYPE_CONTENT_SENTINEL,
		Address:  "sprkdrm1ghosthhmidactivity000000000000000000",
	}

	dup := types.GenesisState{
		Params:           types.DefaultParams(),
		RoleActivityList: []types.RoleActivity{base, base},
	}
	require.ErrorContains(t, dup.Validate(), "duplicated role activity")

	unspec := types.GenesisState{
		Params: types.DefaultParams(),
		RoleActivityList: []types.RoleActivity{
			{RoleType: types.RoleType_ROLE_TYPE_UNSPECIFIED, Address: "x"},
		},
	}
	require.ErrorContains(t, unspec.Validate(), "unspecified role_type")
}
