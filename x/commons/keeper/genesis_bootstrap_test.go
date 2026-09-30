package keeper_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// TestOpsCommitteeCanExecuteCommitteeGatedFederationMsgs closes the gap that
// deadlocked the devnet/testnet federation bring-up.
//
// Authorizing a message takes TWO independent grants that live in different
// modules, and satisfying only one makes the message unexecutable rather than
// unauthorized:
//
//  1. the federation keeper must accept the caller -- MsgResumePeer uses
//     IsCouncilOrCommitteePolicy, which accepts a committee policy address;
//  2. x/commons must let that policy carry the message -- msg_server_proposals.go
//     rejects anything outside the policy's AllowedMessages.
//
// The hardening did (1) and missed (2), so the Operations Committee was the
// only body that could pass the vote at n=1 and the only body forbidden from
// executing the result. The Commons Council could execute it but needs 0.51 of
// its entire membership, which no single operator can muster.
//
// So: every federation message gated on IsCouncilOrCommitteePolicy MUST appear
// in the committee's allowlist. This test derives that set from the handlers
// rather than hardcoding it, so a new committee-gated message fails here
// instead of on a live chain.
func TestOpsCommitteeCanExecuteCommitteeGatedFederationMsgs(t *testing.T) {
	keeperDir := filepath.Join("..", "..", "federation", "keeper")
	entries, err := os.ReadDir(keeperDir)
	require.NoError(t, err)

	msgRe := regexp.MustCompile(`func \(k msgServer\) (\w+)\(ctx context\.Context, msg \*types\.(Msg\w+)\)`)

	var gated []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(keeperDir, e.Name()))
		require.NoError(t, err)
		if !strings.Contains(string(body), "IsCouncilOrCommitteePolicy") {
			continue
		}
		m := msgRe.FindStringSubmatch(string(body))
		require.NotNil(t, m, "%s calls IsCouncilOrCommitteePolicy but no msgServer handler was found in it", e.Name())
		gated = append(gated, "/sparkdream.federation.v1."+m[2])
	}

	require.NotEmpty(t, gated,
		"no committee-gated federation handlers found -- if the gate was removed, delete this test deliberately")

	bootstrap, err := os.ReadFile("genesis_bootstrap.go")
	require.NoError(t, err)

	// Scope the search to the Operations Committee's own AllowedMessages
	// block: MsgResumePeer also appears in the Commons Council's list, so a
	// whole-file Contains would pass while the committee still could not
	// execute it -- exactly the bug this guards.
	src := string(bootstrap)
	start := strings.Index(src, `Name:        "Commons Operations Committee"`)
	require.NotEqual(t, -1, start, "Commons Operations Committee block not found")
	end := strings.Index(src[start:], "MaxSpendPerEpoch")
	require.NotEqual(t, -1, end, "could not find the end of the committee's config block")
	committeeBlock := src[start : start+end]

	for _, typeURL := range gated {
		require.Contains(t, committeeBlock, typeURL,
			"%s is gated on IsCouncilOrCommitteePolicy but is NOT in the Commons "+
				"Operations Committee AllowedMessages, so a passed committee proposal "+
				"cannot execute it (ErrUnauthorized from msg_server_proposals.go)", typeURL)
	}
}

// The Operations Committee owns federation author-curation lists (x/collect
// collections named by PeerPolicy.curation). x/collect accepts a group policy
// address as a collection owner, but, as above, that is only half of the
// grant: without these entries a passed committee proposal to create the list
// or add a curator fails with ErrUnauthorized.
func TestOpsCommitteeCanOwnCurationCollections(t *testing.T) {
	bootstrap, err := os.ReadFile("genesis_bootstrap.go")
	require.NoError(t, err)
	src := string(bootstrap)
	start := strings.Index(src, `Name:        "Commons Operations Committee"`)
	require.NotEqual(t, -1, start, "Commons Operations Committee block not found")
	end := strings.Index(src[start:], "MaxSpendPerEpoch")
	require.NotEqual(t, -1, end, "could not find the end of the committee's config block")
	committeeBlock := src[start : start+end]

	for _, msg := range []string{"MsgCreateCollection", "MsgAddCollaborator", "MsgRemoveCollaborator", "MsgAddItem", "MsgRemoveItem"} {
		require.Contains(t, committeeBlock, `"/sparkdream.collect.v1.`+msg+`"`,
			"the Operations Committee cannot execute %s, so it cannot run a federation curation list", msg)
	}
}
