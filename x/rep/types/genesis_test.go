package types_test

import (
	"testing"

	"sparkdream/x/rep/types"

	"github.com/stretchr/testify/require"
)

func TestGenesisState_Validate(t *testing.T) {
	tests := []struct {
		desc     string
		genState *types.GenesisState
		valid    bool
	}{
		{
			desc:     "default is valid",
			genState: types.DefaultGenesis(),
			valid:    true,
		},
		{
			desc: "valid genesis state",
			// Added Params: types.DefaultParams() to ensure EpochBlocks > 0
			genState: &types.GenesisState{
				Params:             types.DefaultParams(),
				MemberMap:          []types.Member{{Address: "0"}, {Address: "1"}},
				InvitationList:     []types.Invitation{{Id: 0}, {Id: 1}},
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
				InterimTemplateMap: []types.InterimTemplate{{Id: "0"}, {Id: "1"}},
			},
			valid: true,
		}, {
			desc: "duplicated member",
			genState: &types.GenesisState{
				MemberMap: []types.Member{
					{
						Address: "0",
					},
					{
						Address: "0",
					},
				},
				InvitationList: []types.Invitation{{Id: 0}, {Id: 1}}, InvitationCount: 2,
				ProjectList: []types.Project{{Id: 0}, {Id: 1}}, ProjectCount: 2, InitiativeList: []types.Initiative{{Id: 0}, {Id: 1}}, InitiativeCount: 2, StakeList: []types.Stake{{Id: 0}, {Id: 1}}, StakeCount: 2, ChallengeList: []types.Challenge{{Id: 0}, {Id: 1}}, ChallengeCount: 2, JuryReviewList: []types.JuryReview{{Id: 0}, {Id: 1}}, JuryReviewCount: 2, InterimList: []types.Interim{{Id: 0}, {Id: 1}}, InterimCount: 2, InterimTemplateMap: []types.InterimTemplate{{Id: "0"}, {Id: "1"}}}, valid: false,
		}, {
			desc: "duplicated invitation",
			genState: &types.GenesisState{
				InvitationList: []types.Invitation{
					{
						Id: 0,
					},
					{
						Id: 0,
					},
				},
				ProjectList: []types.Project{{Id: 0}, {Id: 1}}, ProjectCount: 2,
				InitiativeList: []types.Initiative{{Id: 0}, {Id: 1}}, InitiativeCount: 2, StakeList: []types.Stake{{Id: 0}, {Id: 1}}, StakeCount: 2, ChallengeList: []types.Challenge{{Id: 0}, {Id: 1}}, ChallengeCount: 2, JuryReviewList: []types.JuryReview{{Id: 0}, {Id: 1}}, JuryReviewCount: 2, InterimList: []types.Interim{{Id: 0}, {Id: 1}}, InterimCount: 2, InterimTemplateMap: []types.InterimTemplate{{Id: "0"}, {Id: "1"}}}, valid: false,
		}, {
			desc: "invalid invitation count",
			genState: &types.GenesisState{
				InvitationList: []types.Invitation{
					{
						Id: 1,
					},
				},
				InvitationCount: 0,
				ProjectList:     []types.Project{{Id: 0}, {Id: 1}}, ProjectCount: 2,
				InitiativeList: []types.Initiative{{Id: 0}, {Id: 1}}, InitiativeCount: 2, StakeList: []types.Stake{{Id: 0}, {Id: 1}}, StakeCount: 2, ChallengeList: []types.Challenge{{Id: 0}, {Id: 1}}, ChallengeCount: 2, JuryReviewList: []types.JuryReview{{Id: 0}, {Id: 1}}, JuryReviewCount: 2, InterimList: []types.Interim{{Id: 0}, {Id: 1}}, InterimCount: 2, InterimTemplateMap: []types.InterimTemplate{{Id: "0"}, {Id: "1"}}}, valid: false,
		}, {
			desc: "duplicated project",
			genState: &types.GenesisState{
				ProjectList: []types.Project{
					{
						Id: 0,
					},
					{
						Id: 0,
					},
				},
				InitiativeList: []types.Initiative{{Id: 0}, {Id: 1}}, InitiativeCount: 2,
				StakeList: []types.Stake{{Id: 0}, {Id: 1}}, StakeCount: 2, ChallengeList: []types.Challenge{{Id: 0}, {Id: 1}}, ChallengeCount: 2, JuryReviewList: []types.JuryReview{{Id: 0}, {Id: 1}}, JuryReviewCount: 2, InterimList: []types.Interim{{Id: 0}, {Id: 1}}, InterimCount: 2, InterimTemplateMap: []types.InterimTemplate{{Id: "0"}, {Id: "1"}}}, valid: false,
		}, {
			desc: "invalid project count",
			genState: &types.GenesisState{
				ProjectList: []types.Project{
					{
						Id: 1,
					},
				},
				ProjectCount:   0,
				InitiativeList: []types.Initiative{{Id: 0}, {Id: 1}}, InitiativeCount: 2,
				StakeList: []types.Stake{{Id: 0}, {Id: 1}}, StakeCount: 2, ChallengeList: []types.Challenge{{Id: 0}, {Id: 1}}, ChallengeCount: 2, JuryReviewList: []types.JuryReview{{Id: 0}, {Id: 1}}, JuryReviewCount: 2, InterimList: []types.Interim{{Id: 0}, {Id: 1}}, InterimCount: 2, InterimTemplateMap: []types.InterimTemplate{{Id: "0"}, {Id: "1"}}}, valid: false,
		}, {
			desc: "duplicated initiative",
			genState: &types.GenesisState{
				InitiativeList: []types.Initiative{
					{
						Id: 0,
					},
					{
						Id: 0,
					},
				},
				StakeList: []types.Stake{{Id: 0}, {Id: 1}}, StakeCount: 2,
				ChallengeList: []types.Challenge{{Id: 0}, {Id: 1}}, ChallengeCount: 2, JuryReviewList: []types.JuryReview{{Id: 0}, {Id: 1}}, JuryReviewCount: 2, InterimList: []types.Interim{{Id: 0}, {Id: 1}}, InterimCount: 2, InterimTemplateMap: []types.InterimTemplate{{Id: "0"}, {Id: "1"}}}, valid: false,
		}, {
			desc: "invalid initiative count",
			genState: &types.GenesisState{
				InitiativeList: []types.Initiative{
					{
						Id: 1,
					},
				},
				InitiativeCount: 0,
				StakeList:       []types.Stake{{Id: 0}, {Id: 1}}, StakeCount: 2,
				ChallengeList: []types.Challenge{{Id: 0}, {Id: 1}}, ChallengeCount: 2, JuryReviewList: []types.JuryReview{{Id: 0}, {Id: 1}}, JuryReviewCount: 2, InterimList: []types.Interim{{Id: 0}, {Id: 1}}, InterimCount: 2, InterimTemplateMap: []types.InterimTemplate{{Id: "0"}, {Id: "1"}}}, valid: false,
		}, {
			desc: "duplicated stake",
			genState: &types.GenesisState{
				StakeList: []types.Stake{
					{
						Id: 0,
					},
					{
						Id: 0,
					},
				},
				ChallengeList: []types.Challenge{{Id: 0}, {Id: 1}}, ChallengeCount: 2,
				JuryReviewList: []types.JuryReview{{Id: 0}, {Id: 1}}, JuryReviewCount: 2, InterimList: []types.Interim{{Id: 0}, {Id: 1}}, InterimCount: 2, InterimTemplateMap: []types.InterimTemplate{{Id: "0"}, {Id: "1"}}}, valid: false,
		}, {
			desc: "invalid stake count",
			genState: &types.GenesisState{
				StakeList: []types.Stake{
					{
						Id: 1,
					},
				},
				StakeCount:    0,
				ChallengeList: []types.Challenge{{Id: 0}, {Id: 1}}, ChallengeCount: 2,
				JuryReviewList: []types.JuryReview{{Id: 0}, {Id: 1}}, JuryReviewCount: 2, InterimList: []types.Interim{{Id: 0}, {Id: 1}}, InterimCount: 2, InterimTemplateMap: []types.InterimTemplate{{Id: "0"}, {Id: "1"}}}, valid: false,
		}, {
			desc: "duplicated challenge",
			genState: &types.GenesisState{
				ChallengeList: []types.Challenge{
					{
						Id: 0,
					},
					{
						Id: 0,
					},
				},
				JuryReviewList: []types.JuryReview{{Id: 0}, {Id: 1}}, JuryReviewCount: 2,
				InterimList: []types.Interim{{Id: 0}, {Id: 1}}, InterimCount: 2, InterimTemplateMap: []types.InterimTemplate{{Id: "0"}, {Id: "1"}}}, valid: false,
		}, {
			desc: "invalid challenge count",
			genState: &types.GenesisState{
				ChallengeList: []types.Challenge{
					{
						Id: 1,
					},
				},
				ChallengeCount: 0,
				JuryReviewList: []types.JuryReview{{Id: 0}, {Id: 1}}, JuryReviewCount: 2,
				InterimList: []types.Interim{{Id: 0}, {Id: 1}}, InterimCount: 2, InterimTemplateMap: []types.InterimTemplate{{Id: "0"}, {Id: "1"}}}, valid: false,
		}, {
			desc: "duplicated juryReview",
			genState: &types.GenesisState{
				JuryReviewList: []types.JuryReview{
					{
						Id: 0,
					},
					{
						Id: 0,
					},
				},
				InterimList: []types.Interim{{Id: 0}, {Id: 1}}, InterimCount: 2,
				InterimTemplateMap: []types.InterimTemplate{{Id: "0"}, {Id: "1"}}}, valid: false,
		}, {
			desc: "invalid juryReview count",
			genState: &types.GenesisState{
				JuryReviewList: []types.JuryReview{
					{
						Id: 1,
					},
				},
				JuryReviewCount: 0,
				InterimList:     []types.Interim{{Id: 0}, {Id: 1}}, InterimCount: 2,
				InterimTemplateMap: []types.InterimTemplate{{Id: "0"}, {Id: "1"}}}, valid: false,
		}, {
			desc: "duplicated interim",
			genState: &types.GenesisState{
				InterimList: []types.Interim{
					{
						Id: 0,
					},
					{
						Id: 0,
					},
				},
				InterimTemplateMap: []types.InterimTemplate{{Id: "0"}, {Id: "1"}}},
			valid: false,
		}, {
			desc: "invalid interim count",
			genState: &types.GenesisState{
				InterimList: []types.Interim{
					{
						Id: 1,
					},
				},
				InterimCount:       0,
				InterimTemplateMap: []types.InterimTemplate{{Id: "0"}, {Id: "1"}}},
			valid: false,
		}, {
			desc: "duplicated interimTemplate",
			genState: &types.GenesisState{
				InterimTemplateMap: []types.InterimTemplate{
					{
						Id: "0",
					},
					{
						Id: "0",
					},
				},
			},
			valid: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := tc.genState.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
