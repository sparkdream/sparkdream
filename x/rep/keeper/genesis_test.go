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
		InvitationCount:    2,
		ProjectList:        []types.Project{{Id: 0}, {Id: 1}},
		ProjectCount:       2,
		InitiativeList:     []types.Initiative{{Id: 0}, {Id: 1}},
		InitiativeCount:    2,
		StakeList:          []types.Stake{{Id: 0}, {Id: 1}},
		StakeCount:         2,
		ChallengeList:      []types.Challenge{{Id: 0}, {Id: 1}},
		ChallengeCount:     2,
		JuryReviewList:     []types.JuryReview{{Id: 0}, {Id: 1}},
		JuryReviewCount:    2,
		InterimList:        []types.Interim{{Id: 0}, {Id: 1}},
		InterimCount:       2,
		InterimTemplateMap: []types.InterimTemplate{{Id: "0"}, {Id: "1"}}}
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
	require.EqualExportedValues(t, genesisState.InterimTemplateMap, got.InterimTemplateMap)

}
