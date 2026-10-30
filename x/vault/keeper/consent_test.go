package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/vault/types"
)

// createListed has creator create a vault listing the given signers (none
// of them the creator, so every entry starts unaccepted).
func createListed(t *testing.T, f *fixture, threshold uint64, weights map[string]uint64, order ...string) types.Vault {
	t.Helper()
	f.bank.balances[creator] = math.NewInt(1_000_000_000)
	res, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
		Creator: creator, Signers: signers(weights, order...), Threshold: threshold, ProposalTtl: 24 * 3600, Label: "aurora",
	})
	require.NoError(t, err)
	v, err := f.keeper.Vault.Get(f.ctx, res.VaultId)
	require.NoError(t, err)
	return v
}

func accept(t *testing.T, f *fixture, signer string, id uint64) error {
	t.Helper()
	_, err := f.srv.AcceptVaultSigner(f.ctx, &types.MsgAcceptVaultSigner{Signer: signer, VaultId: id})
	return err
}

func leave(t *testing.T, f *fixture, signer string, id uint64) error {
	t.Helper()
	_, err := f.srv.LeaveVault(f.ctx, &types.MsgLeaveVault{Signer: signer, VaultId: id})
	return err
}

func assertInvariants(t *testing.T, f *fixture) {
	t.Helper()
	msg, broken := keeperVaultInvariant(f)(f.ctx)
	require.False(t, broken, msg)
	msg, broken = keeperProposalInvariant(f)(f.ctx)
	require.False(t, broken, msg)
}

// VLT-02: the accepted flag is chain-set; a client cannot pre-accept an entry.
func TestAcceptedFlagIgnoredOnInput(t *testing.T) {
	f := initFixture(t)
	f.bank.balances[creator] = math.NewInt(1_000_000_000)
	in := signers(map[string]uint64{alice: 1, bob: 1}, alice, bob)
	in[0].Accepted, in[1].Accepted = true, true
	res, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{Creator: creator, Signers: in, Threshold: 2, ProposalTtl: 3600})
	require.NoError(t, err)
	v, _ := f.keeper.Vault.Get(f.ctx, res.VaultId)
	for _, s := range v.Signers {
		require.False(t, s.Accepted, s.Address)
	}
	// The same through MsgUpdateVault: a new entry stays unaccepted.
	acceptAll(t, f, v.VaultId)
	upd := signers(map[string]uint64{alice: 1, bob: 1, carol: 1}, alice, bob, carol)
	upd[2].Accepted = true
	require.NoError(t, f.keeper.UpdateVault(f.ctx, v.Address, upd, 2, 3600, ""))
	v, _ = f.keeper.Vault.Get(f.ctx, v.VaultId)
	require.True(t, v.Signers[0].Accepted)
	require.True(t, v.Signers[1].Accepted)
	require.False(t, v.Signers[2].Accepted, "carol was added by the vault, not by carol")
	assertInvariants(t, f)
}

// VLT-02: a vault whose threshold needs its invitees cannot execute until
// enough weight accepts.
func TestProposalsWaitForAcceptedWeight(t *testing.T) {
	f := initFixture(t)
	v := createListed(t, f, 2, map[string]uint64{alice: 1, bob: 1}, alice, bob)
	f.bank.balances[v.Address] = math.NewInt(1_000)

	require.NoError(t, accept(t, f, alice, v.VaultId))
	p := propose(t, f, alice, v.VaultId, anySend(t, v.Address, sink, 1))
	// bob is listed but not accepted: he can neither approve nor execute.
	_, err := f.srv.Approve(f.ctx, &types.MsgApprove{Signer: bob, ProposalId: p.ProposalId, MessagesHash: p.MessagesHash})
	require.ErrorIs(t, err, types.ErrNotSigner)
	_, err = f.srv.Execute(f.ctx, &types.MsgExecute{Signer: alice, ProposalId: p.ProposalId})
	require.ErrorIs(t, err, types.ErrThresholdNotMet)

	require.NoError(t, accept(t, f, bob, v.VaultId))
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: bob, ProposalId: p.ProposalId, MessagesHash: p.MessagesHash, Execute: true})
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_EXECUTED, proposalStatus(t, f, p.ProposalId))
	assertInvariants(t, f)
}

// VLT-02: any signer may remove itself; an invitee declines with the same
// message. Leaving an accepted entry bumps the config and invalidates
// pending proposals; declining an unaccepted one does not.
func TestLeaveVault(t *testing.T) {
	f := initFixture(t)
	v := createListed(t, f, 2, map[string]uint64{alice: 1, bob: 1, carol: 1, dave: 1}, alice, bob, carol, dave)
	for _, s := range []string{alice, bob, carol} {
		require.NoError(t, accept(t, f, s, v.VaultId))
	}
	p := propose(t, f, alice, v.VaultId, anySend(t, v.Address, sink, 1))

	// dave never accepted: declining keeps the config and the proposal.
	require.NoError(t, leave(t, f, dave, v.VaultId))
	v2, _ := f.keeper.Vault.Get(f.ctx, v.VaultId)
	require.Len(t, v2.Signers, 3)
	require.Equal(t, uint64(1), v2.ConfigVersion)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_PENDING, proposalStatus(t, f, p.ProposalId))
	has, _ := f.keeper.VaultInvitesBySigner.Has(f.ctx, collections.Join(dave, v.VaultId))
	require.False(t, has, "the invitation is gone")
	require.ErrorIs(t, leave(t, f, dave, v.VaultId), types.ErrNotSigner, "only once")
	require.ErrorIs(t, accept(t, f, dave, v.VaultId), types.ErrNotSigner)

	// carol (accepted) leaves: config bumps, alice's proposal is invalidated.
	require.NoError(t, leave(t, f, carol, v.VaultId))
	v2, _ = f.keeper.Vault.Get(f.ctx, v.VaultId)
	require.Len(t, v2.Signers, 2)
	require.Equal(t, uint64(2), v2.ConfigVersion)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_INVALIDATED, proposalStatus(t, f, p.ProposalId))
	has, _ = f.keeper.VaultsBySigner.Has(f.ctx, collections.Join(carol, v.VaultId))
	require.False(t, has)
	_, err := f.srv.Propose(f.ctx, &types.MsgPropose{Signer: carol, VaultId: v.VaultId, Messages: []*codectypes.Any{anySend(t, v.Address, sink, 1)}})
	require.ErrorIs(t, err, types.ErrNotSigner)
	assertInvariants(t, f)
}

// VLT-02: a leave never bricks a vault. Refused when the remaining weight
// cannot reach the threshold, when an accepted leaver would take an
// operational vault below its threshold, and for the last signer. The
// remedy is a MsgUpdateVault by the remaining signers first.
func TestLeaveVaultRefusedBelowThreshold(t *testing.T) {
	f := initFixture(t)
	v := createListed(t, f, 2, map[string]uint64{alice: 1, bob: 1, carol: 1}, alice, bob, carol)
	require.NoError(t, accept(t, f, alice, v.VaultId))
	require.NoError(t, accept(t, f, bob, v.VaultId))

	// Operational 2-of-{alice, bob} (+ carol invited): alice cannot leave,
	// the accepted weight would drop to 1 < 2.
	require.ErrorIs(t, leave(t, f, alice, v.VaultId), types.ErrLeaveBelowThreshold)
	// carol declines: the total stays 2 >= 2.
	require.NoError(t, leave(t, f, carol, v.VaultId))
	// Now bob cannot leave either (total and accepted would be 1 < 2).
	require.ErrorIs(t, leave(t, f, bob, v.VaultId), types.ErrLeaveBelowThreshold)

	// The remedy: the remaining signers lower the threshold, then bob leaves.
	require.NoError(t, f.keeper.UpdateVault(f.ctx, v.Address, signers(map[string]uint64{alice: 1, bob: 1}, alice, bob), 1, 24*3600, ""))
	require.NoError(t, leave(t, f, bob, v.VaultId))
	// alice is the last signer.
	require.ErrorIs(t, leave(t, f, alice, v.VaultId), types.ErrLeaveBelowThreshold)

	// An invitee whose decline would make the threshold unreachable is
	// refused as well (it carries no weight and no cap meanwhile).
	w := createListed(t, f, 2, map[string]uint64{alice: 1, bob: 1}, alice, bob)
	require.ErrorIs(t, leave(t, f, bob, w.VaultId), types.ErrLeaveBelowThreshold)
	// A vault awaiting invitees lets an accepted signer go while the rest can
	// still reach the threshold.
	x := createListed(t, f, 2, map[string]uint64{alice: 1, bob: 1, carol: 1}, alice, bob, carol)
	require.NoError(t, accept(t, f, alice, x.VaultId))
	require.NoError(t, leave(t, f, alice, x.VaultId))
	assertInvariants(t, f)
}

// VLT-02: acceptance goes through the alias contract. After alice migrates
// to dave, dave accepts the entry listed under alice; the tombstoned alice
// cannot. An entry whose member already holds an accepted entry in the
// vault cannot be accepted (one person would count as two signers).
func TestAcceptAliasAwareAfterMigration(t *testing.T) {
	f := initFixture(t)
	rep := f.withRep()
	v := createListed(t, f, 2, map[string]uint64{alice: 1, bob: 1}, alice, bob)
	require.NoError(t, accept(t, f, bob, v.VaultId))

	rep.canonicalOf[alice] = dave
	rep.tombstoned[alice] = true
	require.ErrorIs(t, accept(t, f, alice, v.VaultId), types.ErrNotSigner, "a tombstone never accepts")
	require.NoError(t, accept(t, f, dave, v.VaultId))
	v2, _ := f.keeper.Vault.Get(f.ctx, v.VaultId)
	require.Equal(t, alice, v2.Signers[0].Address, "entries are never re-keyed")
	require.True(t, v2.Signers[0].Accepted)
	has, _ := f.keeper.VaultsBySigner.Has(f.ctx, collections.Join(alice, v.VaultId))
	require.True(t, has, "indexed under the stored address")
	res, err := keeperQuery(f).VaultsBySigner(f.ctx, &types.QueryVaultsBySignerRequest{Address: dave})
	require.NoError(t, err)
	require.Len(t, res.Vaults, 1, "the query unions the member's addresses")
	// dave now signs as alice.
	p := propose(t, f, dave, v.VaultId, anySend(t, v.Address, sink, 1))
	require.Equal(t, []string{dave}, p.Approvals)

	// Merge through an invitation: carol accepted, then an entry for eve is
	// listed and eve later turns out to be carol's new address.
	eve := addr("eve")
	w := createListed(t, f, 1, map[string]uint64{carol: 1, eve: 1}, carol, eve)
	require.NoError(t, accept(t, f, carol, w.VaultId))
	rep.canonicalOf[carol] = eve
	rep.tombstoned[carol] = true
	require.ErrorIs(t, accept(t, f, eve, w.VaultId), types.ErrInvalidSignerSet)
	// A leave removes every entry eve can act as (both of them here), which
	// would empty the vault, so it is refused.
	require.ErrorIs(t, leave(t, f, eve, w.VaultId), types.ErrLeaveBelowThreshold)
	assertInvariants(t, f)
}

// VLT-02: the accepted flag survives export / import and the indexes are
// rebuilt from it.
func TestGenesisRoundTripKeepsAcceptance(t *testing.T) {
	f := initFixture(t)
	v := createListed(t, f, 2, map[string]uint64{alice: 1, bob: 1, carol: 1}, alice, bob, carol)
	require.NoError(t, accept(t, f, alice, v.VaultId))
	require.NoError(t, accept(t, f, bob, v.VaultId))

	gen, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, gen.Validate())
	got := gen.VaultMap[0].Signers
	require.True(t, got[0].Accepted)
	require.True(t, got[1].Accepted)
	require.False(t, got[2].Accepted)

	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *gen))
	for _, s := range []string{alice, bob} {
		has, _ := g.keeper.VaultsBySigner.Has(g.ctx, collections.Join(s, v.VaultId))
		require.True(t, has, s)
	}
	has, _ := g.keeper.VaultsBySigner.Has(g.ctx, collections.Join(carol, v.VaultId))
	require.False(t, has)
	has, _ = g.keeper.VaultInvitesBySigner.Has(g.ctx, collections.Join(carol, v.VaultId))
	require.True(t, has)
	assertInvariants(t, g)

	// The invariant flags an index that disagrees with the flag.
	require.NoError(t, g.keeper.VaultsBySigner.Set(g.ctx, collections.Join(carol, v.VaultId)))
	_, broken := keeperVaultInvariant(g)(g.ctx)
	require.True(t, broken)
}
