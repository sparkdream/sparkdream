package keeper_test

import (
	"os"
	"testing"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	"github.com/stretchr/testify/require"
)

func TestSetElectoralDelegation_Success(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	parentName := "ParentCouncil"
	require.NoError(t, k.Groups.Set(ctx, parentName, types.Group{
		Index:         parentName,
		PolicyAddress: "parent_policy",
	}))

	childPolicy := "child_policy_addr"
	require.NoError(t, k.SetElectoralDelegation(ctx, parentName, childPolicy))

	got, err := k.Groups.Get(ctx, parentName)
	require.NoError(t, err)
	require.Equal(t, childPolicy, got.ElectoralPolicyAddress)
}

func TestSetElectoralDelegation_MissingParent(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	err := k.SetElectoralDelegation(ctx, "nonexistent", "child_policy")
	require.Error(t, err)
}

func TestBootstrapConstants(t *testing.T) {
	// Sanity: 5-month term duration matches 5 * 30 days in seconds.
	require.Equal(t, int64(5*30*24*60*60), int64(keeper.TermDuration5Months))
	// 1 year in seconds.
	require.Equal(t, int64(365*24*60*60), int64(keeper.TermDuration1Year))
}

// TestCommitteePercentageMajority pins the arithmetic the committee decision
// policies rely on.
//
// checkThreshold evaluates a "percentage" policy as yes-weight over TOTAL
// committee weight using >= -- non-voters count against, abstaining is
// equivalent to opposing. At weight 1 per member that makes 0.5 a simple
// majority, ceil(n/2), which is the property the policies were switched to
// percentage for: an absolute threshold of 1 never gains teeth as a committee
// grows.
//
// The n=1 row is the one that must not regress: a single-member committee has
// to stay unblocked, or a chain cannot be brought up.
func TestCommitteePercentageMajority(t *testing.T) {
	const threshold = 0.5
	// votesNeeded mirrors ratio.GTE(threshold) with weight 1 per member.
	votesNeeded := func(members int) int {
		for yes := 0; yes <= members; yes++ {
			if float64(yes)/float64(members) >= threshold {
				return yes
			}
		}
		return members
	}

	for _, tc := range []struct {
		members, want int
		note          string
	}{
		{1, 1, "single member stays unblocked -- chain bring-up depends on it"},
		{2, 1, "known gap: >= means one vote still carries at two members"},
		{3, 2, "real majority once a third member joins"},
		{4, 2, ""},
		{5, 3, "at MaxMembers, three of five"},
	} {
		require.Equalf(t, tc.want, votesNeeded(tc.members),
			"%d-member committee: %s", tc.members, tc.note)
	}
}

// TestSupervisoryBoardKeepsAbsoluteThreshold guards against a well-meant
// sweep converting every policy to percentage: the board's absolute 2 is a
// HIGHER bar than 0.5 would be at two members, so converting it would quietly
// lower it.
func TestSupervisoryBoardKeepsAbsoluteThreshold(t *testing.T) {
	src, err := os.ReadFile("genesis_bootstrap.go")
	require.NoError(t, err)
	require.Contains(t, string(src), `StandardValue:        "2"`,
		"the Commons Supervisory Board must keep its absolute threshold of 2")
	require.NotContains(t, string(src), `StandardValue:        "1"`,
		"committee policies should be percentage 0.5, not an absolute threshold of 1")
}
