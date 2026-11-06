package keeper_test

import (
	"testing"
	"time"

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

	// An invitee whose decline leaves the threshold unreachable is not
	// refused: the vault was never operational, so the threshold is clamped
	// to the remaining reachable weight instead and the config bumps.
	w := createListed(t, f, 2, map[string]uint64{alice: 1, bob: 1}, alice, bob)
	require.NoError(t, leave(t, f, bob, w.VaultId))
	w2, _ := f.keeper.Vault.Get(f.ctx, w.VaultId)
	require.Len(t, w2.Signers, 1)
	require.Equal(t, uint64(1), w2.Threshold, "clamped to alice's weight")
	require.Equal(t, uint64(2), w2.ConfigVersion, "a clamp is a config change")
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

// Every unaccepted entry of a vault with more than one entry carries an
// expiry InviteTTL after the listing and sits in the invite-expiry queue;
// accepting clears both. The creator's entry and a vault's only entry
// carry none.
func TestInviteExpirySet(t *testing.T) {
	f := initFixture(t)
	now := f.ctx.BlockTime().Unix()
	v := createListed(t, f, 2, map[string]uint64{alice: 1, bob: 1}, alice, bob)
	for _, s := range v.Signers {
		require.Equal(t, now+types.InviteTTL, s.InviteExpiresAt, s.Address)
		has, _ := f.keeper.InviteExpiryQueue.Has(f.ctx, collections.Join3(s.InviteExpiresAt, v.VaultId, s.Address))
		require.True(t, has, s.Address)
	}
	// Input values are ignored.
	in := signers(map[string]uint64{alice: 1, bob: 1}, alice, bob)
	in[0].InviteExpiresAt, in[1].InviteExpiresAt = 1, 1
	f.bank.balances[alice] = math.NewInt(1_000_000_000)
	res, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{Creator: alice, Signers: in, Threshold: 2, ProposalTtl: 3600})
	require.NoError(t, err)
	w, _ := f.keeper.Vault.Get(f.ctx, res.VaultId)
	require.True(t, w.Signers[0].Accepted)
	require.Zero(t, w.Signers[0].InviteExpiresAt, "an accepted entry has no expiry")
	require.Equal(t, now+types.InviteTTL, w.Signers[1].InviteExpiresAt)

	require.NoError(t, accept(t, f, alice, v.VaultId))
	v2, _ := f.keeper.Vault.Get(f.ctx, v.VaultId)
	require.Zero(t, v2.Signers[0].InviteExpiresAt)
	has, _ := f.keeper.InviteExpiryQueue.Has(f.ctx, collections.Join3(v.Signers[0].InviteExpiresAt, v.VaultId, alice))
	require.False(t, has, "accepting leaves the queue")

	// A vault whose only entry is an invitee: no expiry.
	solo := createListed(t, f, 1, map[string]uint64{carol: 1}, carol)
	require.Zero(t, solo.Signers[0].InviteExpiresAt)
	assertInvariants(t, f)
}

// An invitee who never answers is removed by the EndBlocker at
// invite_expires_at. A threshold that needed its weight is clamped to the
// remaining reachable weight, config_version bumps and pending proposals
// are invalidated; a threshold the rest already reach is untouched.
func TestInviteExpiryClampsThreshold(t *testing.T) {
	f := initFixture(t)
	// 3-of-{alice, bob, carol}: alice and bob accept, carol never does.
	v := createListed(t, f, 3, map[string]uint64{alice: 1, bob: 1, carol: 1}, alice, bob, carol)
	require.NoError(t, accept(t, f, alice, v.VaultId))
	require.NoError(t, accept(t, f, bob, v.VaultId))
	f.bank.balances[v.Address] = math.NewInt(1_000)
	p := propose(t, f, alice, v.VaultId, anySend(t, v.Address, sink, 1))
	_, err := f.srv.Approve(f.ctx, &types.MsgApprove{Signer: bob, ProposalId: p.ProposalId, MessagesHash: p.MessagesHash})
	require.NoError(t, err)
	_, err = f.srv.Execute(f.ctx, &types.MsgExecute{Signer: alice, ProposalId: p.ProposalId})
	require.ErrorIs(t, err, types.ErrThresholdNotMet, "2 < 3 while carol is pending")
	// 2-of-{alice, bob, dave}: dave's expiry changes nothing.
	w := createListed(t, f, 2, map[string]uint64{alice: 1, bob: 1, dave: 1}, alice, bob, dave)
	require.NoError(t, accept(t, f, alice, w.VaultId))
	require.NoError(t, accept(t, f, bob, w.VaultId))

	// Not yet due: the EndBlocker leaves both.
	f.advance(time.Duration(types.InviteTTL-1) * time.Second)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	v2, _ := f.keeper.Vault.Get(f.ctx, v.VaultId)
	require.Len(t, v2.Signers, 3)

	f.advance(time.Second)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	v2, _ = f.keeper.Vault.Get(f.ctx, v.VaultId)
	require.Len(t, v2.Signers, 2)
	require.Equal(t, uint64(2), v2.Threshold, "clamped to the reachable weight")
	require.Equal(t, uint64(2), v2.ConfigVersion)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_INVALIDATED, proposalStatus(t, f, p.ProposalId))
	has, _ := f.keeper.VaultInvitesBySigner.Has(f.ctx, collections.Join(carol, v.VaultId))
	require.False(t, has)
	require.ErrorIs(t, accept(t, f, carol, v.VaultId), types.ErrNotSigner, "the listing is gone")
	// The vault acts again with the signers who accepted.
	q := propose(t, f, alice, v.VaultId, anySend(t, v.Address, sink, 1))
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: bob, ProposalId: q.ProposalId, MessagesHash: q.MessagesHash, Execute: true})
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_EXECUTED, proposalStatus(t, f, q.ProposalId))

	w2, _ := f.keeper.Vault.Get(f.ctx, w.VaultId)
	require.Len(t, w2.Signers, 2)
	require.Equal(t, uint64(2), w2.Threshold, "already reachable: untouched")
	require.Equal(t, uint64(1), w2.ConfigVersion, "no config change")
	n, _ := f.keeper.InviteExpiryQueue.Iterate(f.ctx, nil)
	keys, _ := n.Keys()
	require.Empty(t, keys, "the queue is drained")
	assertInvariants(t, f)
}

// The vault's only entry never expires: at its expiry the EndBlocker zeroes
// the clock instead of removing it (an empty vault would be worse), and the
// listing stays acceptable. A rotation that lists someone else restarts
// every invitee's clock.
func TestInviteExpiryKeepsLastEntry(t *testing.T) {
	f := initFixture(t)
	v := createListed(t, f, 2, map[string]uint64{alice: 1, bob: 1}, alice, bob)
	require.NoError(t, leave(t, f, alice, v.VaultId)) // declines; bob is the only entry now
	v2, _ := f.keeper.Vault.Get(f.ctx, v.VaultId)
	require.Len(t, v2.Signers, 1)
	require.Equal(t, uint64(1), v2.Threshold)
	// bob's clock was set at creation (two entries then) and is still queued.
	require.NotZero(t, v2.Signers[0].InviteExpiresAt)
	f.advance(time.Duration(types.InviteTTL) * time.Second)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	v3, _ := f.keeper.Vault.Get(f.ctx, v.VaultId)
	require.Len(t, v3.Signers, 1, "the last entry is kept")
	require.False(t, v3.Signers[0].Accepted)
	require.Zero(t, v3.Signers[0].InviteExpiresAt, "and no longer expires")
	require.Equal(t, uint64(2), v3.ConfigVersion, "only the earlier clamp bumped it")
	require.NoError(t, accept(t, f, bob, v.VaultId))
	assertInvariants(t, f)

	// MsgUpdateVault: a carried-over accepted entry stays without expiry,
	// every new entry gets a fresh clock.
	now := f.ctx.BlockTime().Unix()
	require.NoError(t, f.keeper.UpdateVault(f.ctx, v.Address, signers(map[string]uint64{bob: 1, carol: 1}, bob, carol), 2, 3600, ""))
	v4, _ := f.keeper.Vault.Get(f.ctx, v.VaultId)
	require.True(t, v4.Signers[0].Accepted)
	require.Zero(t, v4.Signers[0].InviteExpiresAt)
	require.Equal(t, now+types.InviteTTL, v4.Signers[1].InviteExpiresAt)
	assertInvariants(t, f)
}

// Genesis carries invite_expires_at with the entry and rebuilds the queue;
// an accepted entry with an expiry is refused.
func TestGenesisRoundTripKeepsInviteExpiry(t *testing.T) {
	f := initFixture(t)
	v := createListed(t, f, 2, map[string]uint64{alice: 1, bob: 1}, alice, bob)
	require.NoError(t, accept(t, f, alice, v.VaultId))
	gen, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, gen.Validate())
	exp := gen.VaultMap[0].Signers[1].InviteExpiresAt
	require.NotZero(t, exp)

	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *gen))
	has, _ := g.keeper.InviteExpiryQueue.Has(g.ctx, collections.Join3(exp, v.VaultId, bob))
	require.True(t, has, "the queue is rebuilt from the entries")
	assertInvariants(t, g)
	g.ctx = g.ctx.WithBlockTime(time.Unix(exp, 0))
	require.NoError(t, g.keeper.EndBlocker(g.ctx))
	v2, _ := g.keeper.Vault.Get(g.ctx, v.VaultId)
	require.Len(t, v2.Signers, 1)
	require.Equal(t, uint64(1), v2.Threshold)
	assertInvariants(t, g)

	bad := *gen
	bad.VaultMap = []types.Vault{gen.VaultMap[0]}
	bad.VaultMap[0].Signers = append([]types.VaultSigner(nil), gen.VaultMap[0].Signers...)
	bad.VaultMap[0].Signers[0].InviteExpiresAt = exp
	require.Error(t, bad.Validate(), "an accepted entry carries no expiry")
}
