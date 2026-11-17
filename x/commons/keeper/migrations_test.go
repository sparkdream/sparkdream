package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/types"
)

// The v2 -> v3 migration gives the Commons Council and its Operations
// Committee the x/storage council messages, keeps what they held, skips a
// body that does not exist, and adds nothing twice.
func TestMigrateV2ToV3GrantsStorageMessages(t *testing.T) {
	f := initFixture(t)
	const council, committee, other = "council-policy", "committee-policy", "other-policy"
	require.NoError(t, f.keeper.Groups.Set(f.ctx, "Commons Council", types.Group{PolicyAddress: council}))
	require.NoError(t, f.keeper.Groups.Set(f.ctx, "Technical Council", types.Group{PolicyAddress: other}))
	require.NoError(t, f.keeper.PolicyPermissions.Set(f.ctx, council, types.PolicyPermissions{
		PolicyAddress:   council,
		AllowedMessages: []string{"/sparkdream.commons.v1.MsgSpendFromCommons", "/sparkdream.storage.v1.MsgVoidCompletion"},
	}))
	require.NoError(t, f.keeper.PolicyPermissions.Set(f.ctx, other, types.PolicyPermissions{
		PolicyAddress: other, AllowedMessages: []string{"/sparkdream.commons.v1.MsgRenewGroup"},
	}))

	// No Operations Committee yet: the migration must not fail.
	require.NoError(t, f.keeper.MigrateV2ToV3(f.ctx))
	perms, err := f.keeper.PolicyPermissions.Get(f.ctx, council)
	require.NoError(t, err)
	require.Len(t, perms.AllowedMessages, 7) // the one it had, plus the six storage messages once each
	require.Equal(t, "/sparkdream.commons.v1.MsgSpendFromCommons", perms.AllowedMessages[0])
	for _, m := range []string{"MsgApproveOperator", "MsgRemoveOperator", "MsgApproveScanner", "MsgRemoveScanner", "MsgVoidCompletion", "MsgUnflagRemoved"} {
		require.Contains(t, perms.AllowedMessages, "/sparkdream.storage.v1."+m)
	}
	require.NotContains(t, perms.AllowedMessages, "/sparkdream.storage.v1.MsgUpdateOperationalParams")

	// Another council is not given them. (The Technical Council gets its
	// board seat messages, see TestMigrateV2ToV3GrantsBoardSeatMessages.)
	otherPerms, err := f.keeper.PolicyPermissions.Get(f.ctx, other)
	require.NoError(t, err)
	for _, m := range otherPerms.AllowedMessages {
		require.NotContains(t, m, "/sparkdream.storage.v1.")
	}

	// The committee, with no allowlist record yet, gets one; prices included.
	require.NoError(t, f.keeper.Groups.Set(f.ctx, "Commons Operations Committee", types.Group{PolicyAddress: committee}))
	require.NoError(t, f.keeper.MigrateV2ToV3(f.ctx))
	cperms, err := f.keeper.PolicyPermissions.Get(f.ctx, committee)
	require.NoError(t, err)
	require.Len(t, cperms.AllowedMessages, 7)
	require.Contains(t, cperms.AllowedMessages, "/sparkdream.storage.v1.MsgUpdateOperationalParams")

	// Running it again changes nothing.
	perms2, err := f.keeper.PolicyPermissions.Get(f.ctx, council)
	require.NoError(t, err)
	require.Equal(t, perms, perms2)
}

// The same migration gives the Technical and Ecosystem Councils, on their
// standard and veto policies, the messages a council needs to propose and
// vote as a member of the Commons Supervisory Board. Without them the board
// could never reach its threshold of two.
func TestMigrateV2ToV3GrantsBoardSeatMessages(t *testing.T) {
	f := initFixture(t)
	policies := map[string][2]string{
		"Technical Council": {"tech-policy", "tech-veto"},
		"Ecosystem Council": {"eco-policy", "eco-veto"},
		"Commons Council":   {"commons-policy", "commons-veto"},
	}
	for name, p := range policies {
		require.NoError(t, f.keeper.Groups.Set(f.ctx, name, types.Group{PolicyAddress: p[0], VetoPolicyAddress: p[1]}))
		require.NoError(t, f.keeper.PolicyPermissions.Set(f.ctx, p[0], types.PolicyPermissions{
			PolicyAddress: p[0], AllowedMessages: []string{"/sparkdream.commons.v1.MsgRenewGroup"},
		}))
		require.NoError(t, f.keeper.PolicyPermissions.Set(f.ctx, p[1], types.PolicyPermissions{
			PolicyAddress: p[1], AllowedMessages: []string{"/sparkdream.commons.v1.MsgVetoGroupProposals"},
		}))
	}

	require.NoError(t, f.keeper.MigrateV2ToV3(f.ctx))
	require.NoError(t, f.keeper.MigrateV2ToV3(f.ctx)) // twice: nothing is added twice

	for _, name := range []string{"Technical Council", "Ecosystem Council"} {
		for _, policy := range policies[name] {
			perms, err := f.keeper.PolicyPermissions.Get(f.ctx, policy)
			require.NoError(t, err)
			require.Len(t, perms.AllowedMessages, 3, policy)
			require.Contains(t, perms.AllowedMessages, "/sparkdream.commons.v1.MsgSubmitProposal")
			require.Contains(t, perms.AllowedMessages, "/sparkdream.commons.v1.MsgVoteProposal")
		}
	}
	// The Commons Council holds no board seat: its veto policy is untouched.
	perms, err := f.keeper.PolicyPermissions.Get(f.ctx, "commons-veto")
	require.NoError(t, err)
	require.Equal(t, []string{"/sparkdream.commons.v1.MsgVetoGroupProposals"}, perms.AllowedMessages)
}
