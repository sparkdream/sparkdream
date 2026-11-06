package keeper_test

import (
	"testing"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// HasVoted attributes a decision to the members who voted on the proposal
// (x/rep pays a project's review interim only to them). A secret ballot
// cannot attribute votes, so it reports every address as a voter; an unknown
// proposal has none.
func TestHasVoted(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	voter := sdk.AccAddress([]byte("voter_hasvoted______")).String()
	silent := sdk.AccAddress([]byte("silent_hasvoted_____")).String()

	require.NoError(t, k.Proposals.Set(ctx, 1, types.Proposal{Id: 1, CouncilName: "c", PolicyAddress: "p"}))
	require.NoError(t, k.Votes.Set(ctx, collections.Join(uint64(1), voter), types.Vote{
		Voter: voter, Option: types.VoteOption_VOTE_OPTION_NO,
	}))
	require.NoError(t, k.Proposals.Set(ctx, 2, types.Proposal{Id: 2, CouncilName: "c", PolicyAddress: "p", SecretBallot: true}))

	require.True(t, k.HasVoted(ctx, 1, voter), "any option counts")
	require.False(t, k.HasVoted(ctx, 1, silent))
	require.True(t, k.HasVoted(ctx, 2, silent), "secret ballot: voters cannot be attributed")
	require.False(t, k.HasVoted(ctx, 3, voter), "unknown proposal")
}
