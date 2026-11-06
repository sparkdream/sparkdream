package keeper_test

import (
	"errors"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/vault/keeper"
	"sparkdream/x/vault/types"
)

// VLT-201: after a merge, a member's unaccepted entry can never be accepted
// (the member already holds an accepted one), so its weight is dead even
// when it is the larger one. {alice:1 acc, bob:3 unacc, carol:1 acc},
// threshold 4, alice recovered into bob: the reachable weight is 1 + 1 = 2
// (bob's 3 is dead), so the hook clamps to 2 and the two people act. Before
// the fix the reachable weight was 3 + 1 = 4, nothing clamped, and the vault
// froze until bob's invitation expired.
func TestReachableWeightIgnoresDeadInviteeOfMergedMember(t *testing.T) {
	f := initFixture(t)
	rep := f.withRep()
	v := createListed(t, f, 4, map[string]uint64{alice: 1, bob: 3, carol: 1}, alice, bob, carol)
	require.NoError(t, accept(t, f, alice, v.VaultId))
	require.NoError(t, accept(t, f, carol, v.VaultId))

	h := keeper.NewMigrationHooks(&f.keeper)
	from, to := sdk.MustAccAddressFromBech32(alice), sdk.MustAccAddressFromBech32(bob)
	path := reptypes.MigrationPath_MIGRATION_PATH_RECOVERY
	require.NoError(t, h.BeforeMemberMigration(f.ctx, from, to, path))
	rep.canonicalOf[alice] = bob
	rep.tombstoned[alice] = true
	require.NoError(t, h.AfterMemberMigration(f.ctx, from, to, path))

	v2, err := f.keeper.Vault.Get(f.ctx, v.VaultId)
	require.NoError(t, err)
	require.Equal(t, uint64(2), v2.Threshold, "clamped to the weight the two people can cast")
	require.Equal(t, uint64(2), v2.ConfigVersion)
	require.Len(t, v2.Signers, 3)
	require.ErrorIs(t, accept(t, f, bob, v.VaultId), types.ErrInvalidSignerSet, "bob's own entry stays dead")

	f.bank.balances[v.Address] = math.NewInt(1_000)
	p := propose(t, f, bob, v.VaultId, anySend(t, v.Address, sink, 1))
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: carol, ProposalId: p.ProposalId, MessagesHash: p.MessagesHash, Execute: true})
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_EXECUTED, proposalStatus(t, f, p.ProposalId))
	assertInvariants(t, f)
}

// VLT-202: the per-proposer pending caps are counted across every alias of
// the proposer. alice fills her general cap, migrates to dave (KEY path, her
// proposals stay pending under her old canonical address), and dave is still
// at the cap.
func TestPendingCapCountsAcrossAliases(t *testing.T) {
	f := initFixture(t)
	rep := f.withRep()
	f.bank.balances[alice] = math.NewInt(1_000_000_000)
	res, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
		Creator: alice, Signers: signers(map[string]uint64{alice: 1, bob: 1}, alice, bob),
		Threshold: 2, ProposalTtl: 24 * 3600,
	})
	require.NoError(t, err)
	v, err := f.keeper.Vault.Get(f.ctx, res.VaultId)
	require.NoError(t, err)
	for i := 0; i < types.MaxPendingProposalsPerProposer; i++ {
		propose(t, f, alice, v.VaultId, anySend(t, v.Address, sink, int64(i+1)))
	}
	send := []*codectypes.Any{anySend(t, v.Address, sink, 99)}
	_, err = f.srv.Propose(f.ctx, &types.MsgPropose{Signer: alice, VaultId: v.VaultId, Messages: send})
	require.ErrorIs(t, err, types.ErrTooManyPending)

	h := keeper.NewMigrationHooks(&f.keeper)
	from, to := sdk.MustAccAddressFromBech32(alice), sdk.MustAccAddressFromBech32(dave)
	path := reptypes.MigrationPath_MIGRATION_PATH_KEY
	require.NoError(t, h.BeforeMemberMigration(f.ctx, from, to, path))
	rep.canonicalOf[alice] = dave
	rep.tombstoned[alice] = true
	require.NoError(t, h.AfterMemberMigration(f.ctx, from, to, path))

	general, updateOnly, err := f.keeper.PendingCounts(f.ctx, v.VaultId, dave)
	require.NoError(t, err)
	require.Equal(t, types.MaxPendingProposalsPerProposer, general, "alice's proposals count toward dave")
	require.Zero(t, updateOnly)
	_, err = f.srv.Propose(f.ctx, &types.MsgPropose{Signer: dave, VaultId: v.VaultId, Messages: send})
	require.ErrorIs(t, err, types.ErrTooManyPending, "a migration grants no fresh cap")

	// The update-only cap is separate and also alias-wide.
	upd := anyMsg(t, &types.MsgUpdateVault{Vault: v.Address, Signers: signers(map[string]uint64{alice: 1, bob: 1}, alice, bob), Threshold: 1, ProposalTtl: 24 * 3600})
	for i := 0; i < types.MaxPendingUpdateProposalsPerProposer; i++ {
		propose(t, f, dave, v.VaultId, upd)
	}
	_, err = f.srv.Propose(f.ctx, &types.MsgPropose{Signer: dave, VaultId: v.VaultId, Messages: []*codectypes.Any{upd}})
	require.ErrorIs(t, err, types.ErrTooManyPending)
	assertInvariants(t, f)
}

// VLT-203: the sole unaccepted entry of a vault nobody accepted may decline.
// A vault with no balance and no proposal records is deleted outright; one
// that holds funds is kept as an inert signer-less record that exports and
// re-imports. The last accepted signer still cannot leave.
func TestSoleInviteeDeclines(t *testing.T) {
	f := initFixture(t)
	// Griefing listing: creator lists bob alone. bob declines, the vault is gone.
	v := createListed(t, f, 1, map[string]uint64{bob: 1}, bob)
	require.False(t, v.Signers[0].Accepted)
	require.Zero(t, v.Signers[0].InviteExpiresAt, "a sole entry never expires")
	require.NoError(t, leave(t, f, bob, v.VaultId))
	_, err := f.keeper.Vault.Get(f.ctx, v.VaultId)
	require.True(t, errors.Is(err, collections.ErrNotFound), "deleted")
	require.False(t, f.keeper.IsVault(f.ctx, v.Address))
	has, err := f.keeper.VaultInvitesBySigner.Has(f.ctx, collections.Join(bob, v.VaultId))
	require.NoError(t, err)
	require.False(t, has)
	require.ErrorIs(t, leave(t, f, bob, v.VaultId), types.ErrVaultNotFound)
	assertInvariants(t, f)

	// Funded listing: the record stays, signer-less; nothing can act for it.
	w := createListed(t, f, 1, map[string]uint64{bob: 1}, bob)
	f.bank.balances[w.Address] = math.NewInt(500)
	require.NoError(t, leave(t, f, bob, w.VaultId))
	w2, err := f.keeper.Vault.Get(f.ctx, w.VaultId)
	require.NoError(t, err)
	require.Empty(t, w2.Signers)
	require.Equal(t, uint64(1), w2.Threshold)
	require.True(t, f.keeper.IsVault(f.ctx, w.Address), "the address stays a vault")
	require.ErrorIs(t, accept(t, f, bob, w.VaultId), types.ErrNotSigner)
	require.ErrorIs(t, leave(t, f, bob, w.VaultId), types.ErrNotSigner)
	_, err = f.srv.Propose(f.ctx, &types.MsgPropose{Signer: bob, VaultId: w.VaultId, Messages: []*codectypes.Any{anySend(t, w.Address, sink, 1)}})
	require.ErrorIs(t, err, types.ErrNotSigner)
	assertInvariants(t, f)
	gen, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, gen.Validate(), "a signer-less vault exports")
	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *gen))
	assertInvariants(t, g)

	// Two invitees of one griefing listing: the first decline clamps, the
	// second dissolves (no accepted signer ever existed).
	x := createListed(t, f, 2, map[string]uint64{bob: 1, carol: 1}, bob, carol)
	require.NoError(t, leave(t, f, bob, x.VaultId))
	require.NoError(t, leave(t, f, carol, x.VaultId))
	_, err = f.keeper.Vault.Get(f.ctx, x.VaultId)
	require.True(t, errors.Is(err, collections.ErrNotFound))
	assertInvariants(t, f)

	// The last accepted signer still cannot leave (e2e TEST 9).
	f.bank.balances[alice] = math.NewInt(1_000_000_000)
	res, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
		Creator: alice, Signers: signers(map[string]uint64{alice: 1, carol: 1}, alice, carol),
		Threshold: 1, ProposalTtl: 24 * 3600,
	})
	require.NoError(t, err)
	require.NoError(t, leave(t, f, carol, res.VaultId))
	require.ErrorIs(t, leave(t, f, alice, res.VaultId), types.ErrLeaveBelowThreshold)
	assertInvariants(t, f)
}

// VLT-204: the accepted-leaver gate uses the same reachable weight as the
// clamp. {alice:1 acc, bob:1 acc, carol:1 unacc, dave:1 unacc}, threshold 3,
// bob migrated into carol (KEY path, no clamp: 1 + 1 + 1 = 3 reachable).
// alice's leave would keep a raw weight of 3 but a reachable weight of 2, so
// it is refused instead of clamping the threshold with reason "decline".
func TestAcceptedLeaveGateUsesReachableWeight(t *testing.T) {
	f := initFixture(t)
	rep := f.withRep()
	v := createListed(t, f, 3, map[string]uint64{alice: 1, bob: 1, carol: 1, dave: 1}, alice, bob, carol, dave)
	require.NoError(t, accept(t, f, alice, v.VaultId))
	require.NoError(t, accept(t, f, bob, v.VaultId))

	h := keeper.NewMigrationHooks(&f.keeper)
	from, to := sdk.MustAccAddressFromBech32(bob), sdk.MustAccAddressFromBech32(carol)
	path := reptypes.MigrationPath_MIGRATION_PATH_KEY
	require.NoError(t, h.BeforeMemberMigration(f.ctx, from, to, path), "carol's entry is unaccepted: no veto")
	rep.canonicalOf[bob] = carol
	rep.tombstoned[bob] = true
	require.NoError(t, h.AfterMemberMigration(f.ctx, from, to, path))
	v2, err := f.keeper.Vault.Get(f.ctx, v.VaultId)
	require.NoError(t, err)
	require.Equal(t, uint64(3), v2.Threshold, "still reachable: no clamp")

	require.ErrorIs(t, leave(t, f, alice, v.VaultId), types.ErrLeaveBelowThreshold)
	v3, err := f.keeper.Vault.Get(f.ctx, v.VaultId)
	require.NoError(t, err)
	require.Equal(t, uint64(3), v3.Threshold)
	require.Equal(t, uint64(1), v3.ConfigVersion, "nothing changed")
	require.Len(t, v3.Signers, 4)
	// dave accepts: reachable stays 3, accepted becomes 3, alice still cannot
	// leave (operational guard), but the vault acts.
	require.NoError(t, accept(t, f, dave, v.VaultId))
	require.ErrorIs(t, leave(t, f, alice, v.VaultId), types.ErrLeaveBelowThreshold)
	assertInvariants(t, f)
}

// VLT-205: the invariant flags a non-sole unaccepted entry without an
// expiry (only a hand-written genesis can produce one).
func TestInvariantFlagsNeverExpiringInvitee(t *testing.T) {
	f := initFixture(t)
	v := createListed(t, f, 2, map[string]uint64{alice: 1, bob: 1}, alice, bob)
	assertInvariants(t, f)
	v2, err := f.keeper.Vault.Get(f.ctx, v.VaultId)
	require.NoError(t, err)
	require.NoError(t, f.keeper.InviteExpiryQueue.Remove(f.ctx, collections.Join3(v2.Signers[1].InviteExpiresAt, v.VaultId, bob)))
	v2.Signers[1].InviteExpiresAt = 0
	require.NoError(t, f.keeper.Vault.Set(f.ctx, v.VaultId, v2))
	msg, broken := keeperVaultInvariant(f)(f.ctx)
	require.True(t, broken, msg)
	require.Contains(t, msg, "never expires")
}
