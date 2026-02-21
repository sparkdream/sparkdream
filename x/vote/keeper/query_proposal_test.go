package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/types"
)

func TestQueryProposal(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(f *testFixture) uint64
		req       func(id uint64) *types.QueryProposalRequest
		wantErr   bool
		errCode   codes.Code
		checkResp func(t *testing.T, resp *types.QueryProposalResponse, id uint64)
	}{
		{
			name: "happy path: get proposal by ID",
			setup: func(f *testFixture) uint64 {
				proposal := types.VotingProposal{
					Id:             1,
					Title:          "Test Proposal",
					Description:    "A test proposal",
					Proposer:       f.member,
					Status:         types.ProposalStatus_PROPOSAL_STATUS_ACTIVE,
					ProposalType:   types.ProposalType_PROPOSAL_TYPE_GENERAL,
					EligibleVoters: 10,
					Quorum:         math.LegacyNewDec(0),
					Threshold:      math.LegacyNewDec(0),
					VetoThreshold:  math.LegacyNewDec(0),
				}
				err := f.keeper.VotingProposal.Set(f.ctx, 1, proposal)
				require.NoError(t, err)
				return 1
			},
			req: func(id uint64) *types.QueryProposalRequest {
				return &types.QueryProposalRequest{ProposalId: id}
			},
			checkResp: func(t *testing.T, resp *types.QueryProposalResponse, _ uint64) {
				require.Equal(t, uint64(1), resp.Proposal.Id)
				require.Equal(t, "Test Proposal", resp.Proposal.Title)
				require.Equal(t, "A test proposal", resp.Proposal.Description)
				require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_ACTIVE, resp.Proposal.Status)
			},
		},
		{
			name: "not found: non-existent proposal ID",
			setup: func(f *testFixture) uint64 {
				return 999
			},
			req: func(id uint64) *types.QueryProposalRequest {
				return &types.QueryProposalRequest{ProposalId: id}
			},
			wantErr: true,
			errCode: codes.NotFound,
		},
		{
			name: "nil request",
			setup: func(f *testFixture) uint64 {
				return 0
			},
			req: func(_ uint64) *types.QueryProposalRequest {
				return nil
			},
			wantErr: true,
			errCode: codes.InvalidArgument,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			id := tc.setup(f)
			resp, err := f.queryServer.Proposal(f.ctx, tc.req(id))
			if tc.wantErr {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				require.Equal(t, tc.errCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				tc.checkResp(t, resp, id)
			}
		})
	}
}
