package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
	sessiontypes "sparkdream/x/session/types"
	shieldtypes "sparkdream/x/shield/types"
	"sparkdream/x/vault/types"
)

var (
	alice   = addr("alice")
	bob     = addr("bob")
	carol   = addr("carol")
	dave    = addr("dave")
	creator = addr("creator")
	sink    = addr("sink")
)

// create2of3 funds the creator and creates alice(1), bob(1), carol(1),
// threshold 2, every signer having accepted its listing.
func create2of3(t *testing.T, f *fixture) types.Vault {
	t.Helper()
	f.bank.balances[creator] = math.NewInt(100_000_000)
	res, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{
		Creator: creator, Signers: signers(map[string]uint64{alice: 1, bob: 1, carol: 1}, alice, bob, carol),
		Threshold: 2, ProposalTtl: 24 * 3600, Label: "phoenix treasury",
	})
	require.NoError(t, err)
	v := acceptAll(t, f, res.VaultId)
	// give the vault some SPARK to send
	f.bank.balances[v.Address] = math.NewInt(50_000)
	return v
}

// acceptAll has every unaccepted signer of vault id accept its listing and
// returns the vault.
func acceptAll(t *testing.T, f *fixture, id uint64) types.Vault {
	t.Helper()
	v, err := f.keeper.Vault.Get(f.ctx, id)
	require.NoError(t, err)
	for _, s := range v.Signers {
		if !s.Accepted {
			_, err := f.srv.AcceptVaultSigner(f.ctx, &types.MsgAcceptVaultSigner{Signer: s.Address, VaultId: id})
			require.NoError(t, err)
		}
	}
	v, err = f.keeper.Vault.Get(f.ctx, id)
	require.NoError(t, err)
	return v
}

func anySend(t *testing.T, from, to string, amt int64) *codectypes.Any {
	t.Helper()
	a, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{FromAddress: from, ToAddress: to, Amount: sdk.NewCoins(sdk.NewInt64Coin(testDenom, amt))})
	require.NoError(t, err)
	return a
}

func anyMsg(t *testing.T, m sdk.Msg) *codectypes.Any {
	t.Helper()
	a, err := codectypes.NewAnyWithValue(m)
	require.NoError(t, err)
	return a
}

func propose(t *testing.T, f *fixture, signer string, vaultID uint64, msgs ...*codectypes.Any) types.VaultProposal {
	t.Helper()
	res, err := f.srv.Propose(f.ctx, &types.MsgPropose{Signer: signer, VaultId: vaultID, Messages: msgs, Memo: "m"})
	require.NoError(t, err)
	p, err := f.keeper.VaultProposal.Get(f.ctx, res.ProposalId)
	require.NoError(t, err)
	return p
}

func proposalStatus(t *testing.T, f *fixture, id uint64) types.ProposalStatus {
	t.Helper()
	p, err := f.keeper.VaultProposal.Get(f.ctx, id)
	require.NoError(t, err)
	return p.Status
}

func TestCreateVault(t *testing.T) {
	f := initFixture(t)
	v := create2of3(t, f)

	require.Equal(t, uint64(1), v.VaultId)
	require.Equal(t, uint64(1), v.ConfigVersion)
	want, err := f.addressCodec.BytesToString(types.VaultAddress(1))
	require.NoError(t, err)
	require.Equal(t, want, v.Address, "address derives from the id")
	require.Equal(t, types.DefaultCreationFeeAmount, f.bank.burned, "creation fee burned")
	require.Equal(t, math.NewInt(100_000_000).Sub(types.DefaultCreationFeeAmount), f.bank.balance(creator))
	require.NotNil(t, f.auth.accounts[v.Address], "keyless base account created")
	require.True(t, f.keeper.IsVault(f.ctx, v.Address))
	require.False(t, f.keeper.IsVault(f.ctx, alice))
	id, err := f.keeper.VaultByAddress.Get(f.ctx, v.Address)
	require.NoError(t, err)
	require.Equal(t, v.VaultId, id)
	for _, s := range []string{alice, bob, carol} {
		has, err := f.keeper.VaultsBySigner.Has(f.ctx, collections.Join(s, v.VaultId))
		require.NoError(t, err)
		require.True(t, has)
	}

	// Pre-funded address: bank already created a base account -> accepted.
	preAddr, _ := f.addressCodec.BytesToString(types.VaultAddress(2))
	f.auth.SetAccount(f.ctx, f.auth.NewAccountWithAddress(f.ctx, types.VaultAddress(2)))
	_, err = f.srv.CreateVault(f.ctx, &types.MsgCreateVault{Creator: creator, Signers: signers(map[string]uint64{alice: 1}, alice), Threshold: 1, ProposalTtl: 3600})
	require.NoError(t, err)
	require.True(t, f.keeper.IsVault(f.ctx, preAddr))
}

func TestCreateVaultRefusals(t *testing.T) {
	f := initFixture(t)
	f.bank.balances[creator] = math.NewInt(100_000_000)
	base := func() *types.MsgCreateVault {
		return &types.MsgCreateVault{Creator: creator, Signers: signers(map[string]uint64{alice: 2, bob: 1}, alice, bob), Threshold: 2, ProposalTtl: 3600, Label: "x"}
	}
	cases := map[string]struct {
		mutate func(m *types.MsgCreateVault)
		err    error
	}{
		"no signers": {func(m *types.MsgCreateVault) { m.Signers = nil }, types.ErrInvalidSignerSet},
		"duplicate signer": {func(m *types.MsgCreateVault) {
			m.Signers = append(m.Signers, types.VaultSigner{Address: alice, Weight: 1})
		}, types.ErrInvalidSignerSet},
		"zero weight":      {func(m *types.MsgCreateVault) { m.Signers[0].Weight = 0 }, types.ErrInvalidSignerSet},
		"weight above max": {func(m *types.MsgCreateVault) { m.Signers[0].Weight = types.MaxSignerWeight + 1 }, types.ErrInvalidSignerSet},
		"zero threshold":   {func(m *types.MsgCreateVault) { m.Threshold = 0 }, types.ErrInvalidThreshold},
		"unreachable":      {func(m *types.MsgCreateVault) { m.Threshold = 4 }, types.ErrInvalidThreshold},
		"ttl too short":    {func(m *types.MsgCreateVault) { m.ProposalTtl = types.MinProposalTTL - 1 }, types.ErrInvalidProposalTTL},
		"ttl too long":     {func(m *types.MsgCreateVault) { m.ProposalTtl = types.MaxProposalTTL + 1 }, types.ErrInvalidProposalTTL},
		"label too long":   {func(m *types.MsgCreateVault) { m.Label = string(make([]byte, types.MaxLabelLength+1)) }, types.ErrInvalidLabel},
		"too many signers": {func(m *types.MsgCreateVault) {
			m.Signers = nil
			for i := 0; i <= types.MaxSigners; i++ {
				m.Signers = append(m.Signers, types.VaultSigner{Address: addr(string(rune('a' + i))), Weight: 1})
			}
		}, types.ErrInvalidSignerSet},
		"invalid address": {func(m *types.MsgCreateVault) { m.Signers[0].Address = "not-an-address" }, types.ErrInvalidSignerSet},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := base()
			tc.mutate(m)
			_, err := f.srv.CreateVault(f.ctx, m)
			require.ErrorIs(t, err, tc.err)
		})
	}

	// A vault address can never be a signer (no nesting).
	v := create2of3(t, f)
	m := base()
	m.Signers = signers(map[string]uint64{v.Address: 1, alice: 1}, v.Address, alice)
	_, err := f.srv.CreateVault(f.ctx, m)
	require.ErrorIs(t, err, types.ErrNotPersonalAddress)

	// x/rep's personal-address gate is consulted when wired.
	rep := f.withRep()
	rep.nonPersonal[bob] = true
	_, err = f.srv.CreateVault(f.ctx, base())
	require.ErrorIs(t, err, types.ErrNotPersonalAddress)

	// creation_enabled=false refuses creation only.
	p, _ := f.keeper.Params.Get(f.ctx)
	p.CreationEnabled = false
	require.NoError(t, f.keeper.Params.Set(f.ctx, p))
	rep.nonPersonal = map[string]bool{}
	_, err = f.srv.CreateVault(f.ctx, base())
	require.ErrorIs(t, err, types.ErrCreationDisabled)
	// ...existing vaults keep working.
	prop := propose(t, f, alice, v.VaultId, anySend(t, v.Address, sink, 10))
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: bob, ProposalId: prop.ProposalId, MessagesHash: prop.MessagesHash, Execute: true})
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_EXECUTED, proposalStatus(t, f, prop.ProposalId))

	// Insufficient balance for the fee.
	p.CreationEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, p))
	m = base()
	m.Creator = dave
	_, err = f.srv.CreateVault(f.ctx, m)
	require.Error(t, err)
}

func TestProposeApproveExecute(t *testing.T) {
	f := initFixture(t)
	v := create2of3(t, f)

	// A non-signer cannot propose.
	_, err := f.srv.Propose(f.ctx, &types.MsgPropose{Signer: dave, VaultId: v.VaultId, Messages: []*codectypes.Any{anySend(t, v.Address, sink, 10)}})
	require.ErrorIs(t, err, types.ErrNotSigner)
	// No messages / too many.
	_, err = f.srv.Propose(f.ctx, &types.MsgPropose{Signer: alice, VaultId: v.VaultId})
	require.ErrorIs(t, err, types.ErrNoMessages)
	many := make([]*codectypes.Any, types.MaxMessagesPerProposal+1)
	for i := range many {
		many[i] = anySend(t, v.Address, sink, 1)
	}
	_, err = f.srv.Propose(f.ctx, &types.MsgPropose{Signer: alice, VaultId: v.VaultId, Messages: many})
	require.ErrorIs(t, err, types.ErrTooManyMessages)
	// Inner signer must be the vault.
	_, err = f.srv.Propose(f.ctx, &types.MsgPropose{Signer: alice, VaultId: v.VaultId, Messages: []*codectypes.Any{anySend(t, alice, sink, 10)}})
	require.ErrorIs(t, err, types.ErrInnerSignerMismatch)

	p := propose(t, f, alice, v.VaultId, anySend(t, v.Address, sink, 10_000))
	require.Equal(t, []string{alice}, p.Approvals, "proposer approves implicitly")
	require.Equal(t, types.MessagesHash(p.Messages), p.MessagesHash)
	require.Equal(t, f.ctx.BlockTime().Unix()+v.ProposalTtl, p.ExpiresAt)
	require.Equal(t, uint64(1), f.keeper.Tally(f.ctx, v, p))

	// Below threshold: execute refused.
	_, err = f.srv.Execute(f.ctx, &types.MsgExecute{Signer: alice, ProposalId: p.ProposalId})
	require.ErrorIs(t, err, types.ErrThresholdNotMet)
	// Non-signer cannot approve; wrong hash refused; double approval refused.
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: dave, ProposalId: p.ProposalId, MessagesHash: p.MessagesHash})
	require.ErrorIs(t, err, types.ErrNotSigner)
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: bob, ProposalId: p.ProposalId, MessagesHash: []byte("nope")})
	require.ErrorIs(t, err, types.ErrMessagesHashMismatch)
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: alice, ProposalId: p.ProposalId, MessagesHash: p.MessagesHash})
	require.ErrorIs(t, err, types.ErrAlreadyApproved)

	// Second approval without execute: pending, tally 2.
	res, err := f.srv.Approve(f.ctx, &types.MsgApprove{Signer: bob, ProposalId: p.ProposalId, MessagesHash: p.MessagesHash})
	require.NoError(t, err)
	require.False(t, res.Executed)
	p, _ = f.keeper.VaultProposal.Get(f.ctx, p.ProposalId)
	require.Equal(t, uint64(2), f.keeper.Tally(f.ctx, v, p))

	// Revoke, tally back to 1, re-approve.
	_, err = f.srv.RevokeApproval(f.ctx, &types.MsgRevokeApproval{Signer: bob, ProposalId: p.ProposalId})
	require.NoError(t, err)
	_, err = f.srv.RevokeApproval(f.ctx, &types.MsgRevokeApproval{Signer: bob, ProposalId: p.ProposalId})
	require.ErrorIs(t, err, types.ErrNotApproved)
	p, _ = f.keeper.VaultProposal.Get(f.ctx, p.ProposalId)
	require.Equal(t, uint64(1), f.keeper.Tally(f.ctx, v, p))
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: carol, ProposalId: p.ProposalId, MessagesHash: p.MessagesHash})
	require.NoError(t, err)

	// Execute by any current signer; funds move; status EXECUTED; indexes cleared.
	before := f.bank.balance(sink)
	_, err = f.srv.Execute(f.ctx, &types.MsgExecute{Signer: bob, ProposalId: p.ProposalId})
	require.NoError(t, err)
	require.Equal(t, before.AddRaw(10_000), f.bank.balance(sink))
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_EXECUTED, proposalStatus(t, f, p.ProposalId))
	has, _ := f.keeper.PendingByVault.Has(f.ctx, collections.Join(v.VaultId, p.ProposalId))
	require.False(t, has)
	has, _ = f.keeper.ExpiryQueue.Has(f.ctx, collections.Join(p.ExpiresAt, p.ProposalId))
	require.False(t, has)
	has, _ = f.keeper.RetentionQueue.Has(f.ctx, collections.Join(f.ctx.BlockTime().Unix(), p.ProposalId))
	require.True(t, has)
	// Not pending any more.
	_, err = f.srv.Execute(f.ctx, &types.MsgExecute{Signer: bob, ProposalId: p.ProposalId})
	require.ErrorIs(t, err, types.ErrProposalNotPending)

	// A failing inner message leaves the proposal PENDING (the tx reverts).
	big := propose(t, f, alice, v.VaultId, anySend(t, v.Address, sink, 1_000_000_000))
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: bob, ProposalId: big.ProposalId, MessagesHash: big.MessagesHash, Execute: true})
	require.Error(t, err)
	// (the msg server returned an error, so a real tx would revert the
	// cache; here we only check the keeper's own pre-write state)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_PENDING, proposalStatus(t, f, big.ProposalId), "status write happened in the cache context only")

	// Propose with execute on a 1-of-n vault executes at once.
	f.bank.balances[creator] = math.NewInt(100_000_000)
	one, err := f.srv.CreateVault(f.ctx, &types.MsgCreateVault{Creator: creator, Signers: signers(map[string]uint64{alice: 1}, alice), Threshold: 1, ProposalTtl: 3600})
	require.NoError(t, err)
	acceptAll(t, f, one.VaultId)
	f.bank.balances[one.Address] = math.NewInt(100)
	pr, err := f.srv.Propose(f.ctx, &types.MsgPropose{Signer: alice, VaultId: one.VaultId, Messages: []*codectypes.Any{anySend(t, one.Address, sink, 5)}, Execute: true})
	require.NoError(t, err)
	require.True(t, pr.Executed)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_EXECUTED, proposalStatus(t, f, pr.ProposalId))
}

func TestCancelAndExpiryAndRetention(t *testing.T) {
	f := initFixture(t)
	v := create2of3(t, f)
	p := propose(t, f, alice, v.VaultId, anySend(t, v.Address, sink, 1))

	// Only the proposer (or the vault) cancels.
	_, err := f.srv.CancelProposal(f.ctx, &types.MsgCancelProposal{Signer: bob, ProposalId: p.ProposalId})
	require.ErrorIs(t, err, types.ErrNotProposer)
	_, err = f.srv.CancelProposal(f.ctx, &types.MsgCancelProposal{Signer: alice, ProposalId: p.ProposalId})
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_CANCELLED, proposalStatus(t, f, p.ProposalId))

	// The vault cancels another proposal through an executed proposal.
	victim := propose(t, f, bob, v.VaultId, anySend(t, v.Address, sink, 1))
	cancel := propose(t, f, alice, v.VaultId, anyMsg(t, &types.MsgCancelProposal{Signer: v.Address, ProposalId: victim.ProposalId}))
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: carol, ProposalId: cancel.ProposalId, MessagesHash: cancel.MessagesHash, Execute: true})
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_CANCELLED, proposalStatus(t, f, victim.ProposalId))
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_EXECUTED, proposalStatus(t, f, cancel.ProposalId))

	// Expiry: the EndBlocker marks it EXPIRED; approve/execute refuse after expires_at even before the EndBlocker.
	late := propose(t, f, alice, v.VaultId, anySend(t, v.Address, sink, 1))
	f.advance(time.Duration(v.ProposalTtl) * time.Second)
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: bob, ProposalId: late.ProposalId, MessagesHash: late.MessagesHash})
	require.ErrorIs(t, err, types.ErrProposalExpired)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_EXPIRED, proposalStatus(t, f, late.ProposalId))
	has, _ := f.keeper.PendingByProposer.Has(f.ctx, collections.Join3(v.VaultId, alice, late.ProposalId))
	require.False(t, has)

	// Retention: resolved proposals are pruned after ProposalRetention.
	f.advance(time.Duration(types.ProposalRetention) * time.Second)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	for _, id := range []uint64{p.ProposalId, victim.ProposalId, cancel.ProposalId, late.ProposalId} {
		_, err := f.keeper.VaultProposal.Get(f.ctx, id)
		require.ErrorIs(t, err, collections.ErrNotFound, "proposal %d pruned", id)
	}
	n := 0
	require.NoError(t, f.keeper.RetentionQueue.Walk(f.ctx, nil, func(collections.Pair[int64, uint64]) (bool, error) { n++; return false, nil }))
	require.Zero(t, n)
}

func TestUpdateVaultRotation(t *testing.T) {
	f := initFixture(t)
	v := create2of3(t, f)
	other := propose(t, f, bob, v.VaultId, anySend(t, v.Address, sink, 1))

	// MsgUpdateVault cannot be signed directly by anyone but the vault (no key),
	// so a direct call with a wrong address is a plain not-found.
	_, err := f.srv.UpdateVault(f.ctx, &types.MsgUpdateVault{Vault: alice, Signers: v.Signers, Threshold: 2, ProposalTtl: 3600})
	require.ErrorIs(t, err, types.ErrVaultNotFound)

	// Rotate carol -> dave with weight 2, threshold 3, alongside a send (atomic).
	newSet := signers(map[string]uint64{alice: 1, bob: 1, dave: 2}, alice, bob, dave)
	rot := propose(t, f, alice, v.VaultId,
		anyMsg(t, &types.MsgUpdateVault{Vault: v.Address, Signers: newSet, Threshold: 3, ProposalTtl: 7200, Label: "rotated"}),
		anySend(t, v.Address, sink, 7))
	// Invalid configs are refused at execution time too.
	bad := propose(t, f, alice, v.VaultId, anyMsg(t, &types.MsgUpdateVault{Vault: v.Address, Signers: newSet, Threshold: 99, ProposalTtl: 7200}))
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: bob, ProposalId: bad.ProposalId, MessagesHash: bad.MessagesHash, Execute: true})
	require.ErrorIs(t, err, types.ErrInvalidThreshold)

	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: carol, ProposalId: rot.ProposalId, MessagesHash: rot.MessagesHash, Execute: true})
	require.NoError(t, err)
	v2, _ := f.keeper.Vault.Get(f.ctx, v.VaultId)
	require.Equal(t, uint64(2), v2.ConfigVersion)
	require.Equal(t, uint64(3), v2.Threshold)
	require.Equal(t, int64(7200), v2.ProposalTtl)
	require.Equal(t, "rotated", v2.Label)
	// alice and bob carry over their acceptance; dave is new and must accept.
	wantSet := signers(map[string]uint64{alice: 1, bob: 1, dave: 2}, alice, bob, dave)
	wantSet[0].Accepted, wantSet[1].Accepted = true, true
	require.Equal(t, wantSet, v2.Signers)
	require.Equal(t, math.NewInt(50_000-7), f.bank.balance(v.Address), "the send in the same proposal ran")
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_EXECUTED, proposalStatus(t, f, rot.ProposalId), "the executing proposal is not invalidated by its own update")
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_INVALIDATED, proposalStatus(t, f, other.ProposalId))
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_INVALIDATED, proposalStatus(t, f, bad.ProposalId))
	has, _ := f.keeper.VaultsBySigner.Has(f.ctx, collections.Join(carol, v.VaultId))
	require.False(t, has, "removed signer dropped from the index")
	has, _ = f.keeper.VaultsBySigner.Has(f.ctx, collections.Join(dave, v.VaultId))
	require.False(t, has, "dave has not accepted yet")
	has, _ = f.keeper.VaultInvitesBySigner.Has(f.ctx, collections.Join(dave, v.VaultId))
	require.True(t, has)
	require.Equal(t, uint64(2), f.keeper.AcceptedWeight(f.ctx, v2.Signers), "threshold 3 waits for dave")

	// Removed signer cannot act; approvals from a removed signer never count.
	_, err = f.srv.Propose(f.ctx, &types.MsgPropose{Signer: carol, VaultId: v.VaultId, Messages: []*codectypes.Any{anySend(t, v.Address, sink, 1)}})
	require.ErrorIs(t, err, types.ErrNotSigner)
	np := propose(t, f, alice, v.VaultId, anySend(t, v.Address, sink, 1))
	np.Approvals = append(np.Approvals, carol) // a stale approval smuggled into state
	require.NoError(t, f.keeper.VaultProposal.Set(f.ctx, np.ProposalId, np))
	require.Equal(t, uint64(1), f.keeper.Tally(f.ctx, v2, np))
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: dave, ProposalId: np.ProposalId, MessagesHash: np.MessagesHash})
	require.ErrorIs(t, err, types.ErrNotSigner, "an unaccepted entry cannot approve")
	v2 = acceptAll(t, f, v.VaultId)
	require.Equal(t, uint64(2), v2.ConfigVersion, "accepting does not bump the config")
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: dave, ProposalId: np.ProposalId, MessagesHash: np.MessagesHash})
	require.NoError(t, err)
	np, _ = f.keeper.VaultProposal.Get(f.ctx, np.ProposalId)
	require.Equal(t, uint64(3), f.keeper.Tally(f.ctx, v2, np))
}

func TestPendingCapsAntiLockout(t *testing.T) {
	f := initFixture(t)
	v := create2of3(t, f)
	for i := 0; i < types.MaxPendingProposalsPerProposer; i++ {
		propose(t, f, alice, v.VaultId, anySend(t, v.Address, sink, 1))
	}
	_, err := f.srv.Propose(f.ctx, &types.MsgPropose{Signer: alice, VaultId: v.VaultId, Messages: []*codectypes.Any{anySend(t, v.Address, sink, 1)}})
	require.ErrorIs(t, err, types.ErrTooManyPending)
	// Other signers are unaffected.
	propose(t, f, bob, v.VaultId, anySend(t, v.Address, sink, 1))
	// The capped proposer can still propose rotation (update-only cap).
	upd := &types.MsgUpdateVault{Vault: v.Address, Signers: signers(map[string]uint64{bob: 1, carol: 1}, bob, carol), Threshold: 2, ProposalTtl: 3600}
	for i := 0; i < types.MaxPendingUpdateProposalsPerProposer; i++ {
		propose(t, f, alice, v.VaultId, anyMsg(t, upd))
	}
	_, err = f.srv.Propose(f.ctx, &types.MsgPropose{Signer: alice, VaultId: v.VaultId, Messages: []*codectypes.Any{anyMsg(t, upd)}})
	require.ErrorIs(t, err, types.ErrTooManyPending, "the update exemption is itself bounded")
	// Rotating alice out invalidates every one of her proposals.
	rot := propose(t, f, bob, v.VaultId, anyMsg(t, upd))
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: carol, ProposalId: rot.ProposalId, MessagesHash: rot.MessagesHash, Execute: true})
	require.NoError(t, err)
	n := 0
	require.NoError(t, f.keeper.PendingByVault.Walk(f.ctx, nil, func(collections.Pair[uint64, uint64]) (bool, error) { n++; return false, nil }))
	require.Zero(t, n, "nothing pending after a rotation")
	general, updateOnly, err := f.keeper.PendingCounts(f.ctx, v.VaultId, alice)
	require.NoError(t, err)
	require.Zero(t, general+updateOnly)
}

func TestInnerMsgDenylist(t *testing.T) {
	f := initFixture(t)
	v := create2of3(t, f)
	denied := []sdk.Msg{
		&reptypes.MsgAcceptMigration{NewAddress: v.Address},
		&reptypes.MsgRegisterZkPublicKey{Member: v.Address},
		&reptypes.MsgClearZkPublicKey{Member: v.Address},
		&reptypes.MsgRequestMigration{Member: v.Address},
		&shieldtypes.MsgShieldedExec{Submitter: v.Address},
		&sessiontypes.MsgExecSession{Grantee: v.Address},
		&types.MsgCreateVault{Creator: v.Address},
		&types.MsgPropose{Signer: v.Address},
		&types.MsgApprove{Signer: v.Address},
		&types.MsgExecute{Signer: v.Address},
		&types.MsgRevokeApproval{Signer: v.Address},
		// A vault is never a person: it cannot accept a listing or leave.
		&types.MsgAcceptVaultSigner{Signer: v.Address},
		&types.MsgLeaveVault{Signer: v.Address},
	}
	for _, m := range denied {
		_, err := f.srv.Propose(f.ctx, &types.MsgPropose{Signer: alice, VaultId: v.VaultId, Messages: []*codectypes.Any{anyMsg(t, m)}})
		require.ErrorIs(t, err, types.ErrInnerMsgDenied, sdk.MsgTypeURL(m))
	}
	// CMN-12: every message guarded by an ante-only rule is kept out, since
	// a proposal's inner messages skip the ante. commons MsgSubmitProposal
	// (proposal-fee floor) is refused outright; the gnovm messages are
	// refused whenever they are mixed with any other message (the gnovm
	// ante's no-mix rule). The type URLs are checked before unpacking, so
	// raw Anys suffice.
	for _, url := range commontypes.AnteOnlyRuleMsgTypeURLs() {
		raw := &codectypes.Any{TypeUrl: url}
		if commontypes.IsGnoVMMsgTypeURL(url) {
			_, err := f.srv.Propose(f.ctx, &types.MsgPropose{Signer: alice, VaultId: v.VaultId, Messages: []*codectypes.Any{raw, anySend(t, v.Address, sink, 1)}})
			require.ErrorIs(t, err, types.ErrInnerMsgDenied, "%s mixed with a send", url)
			_, err = f.srv.Propose(f.ctx, &types.MsgPropose{Signer: alice, VaultId: v.VaultId, Messages: []*codectypes.Any{anySend(t, v.Address, sink, 1), raw}})
			require.ErrorIs(t, err, types.ErrInnerMsgDenied, "send mixed with %s", url)
			continue
		}
		_, err := f.srv.Propose(f.ctx, &types.MsgPropose{Signer: alice, VaultId: v.VaultId, Messages: []*codectypes.Any{raw}})
		require.ErrorIs(t, err, types.ErrInnerMsgDenied, url)
	}
	_, err := f.srv.Propose(f.ctx, &types.MsgPropose{Signer: alice, VaultId: v.VaultId, Messages: []*codectypes.Any{{TypeUrl: "/sparkdream.commons.v1.MsgSubmitProposal"}}})
	require.ErrorIs(t, err, types.ErrInnerMsgDenied, "commons MsgSubmitProposal by literal URL")
	// The allowed vault messages pass the denylist (signer check still applies).
	for _, m := range []sdk.Msg{&types.MsgUpdateVault{Vault: v.Address, Signers: v.Signers, Threshold: 2, ProposalTtl: 3600}, &types.MsgCancelProposal{Signer: v.Address, ProposalId: 1}} {
		_, err := f.srv.Propose(f.ctx, &types.MsgPropose{Signer: alice, VaultId: v.VaultId, Messages: []*codectypes.Any{anyMsg(t, m)}})
		require.NoError(t, err, sdk.MsgTypeURL(m))
	}
}

func TestAliasContract(t *testing.T) {
	f := initFixture(t)
	rep := f.withRep()
	v := create2of3(t, f)
	p := propose(t, f, alice, v.VaultId, anySend(t, v.Address, sink, 1))

	// alice migrates to dave: dave acts for the stored signer alice; alice is a tombstone.
	rep.canonicalOf[alice] = dave
	rep.tombstoned[alice] = true

	_, err := f.srv.Approve(f.ctx, &types.MsgApprove{Signer: alice, ProposalId: p.ProposalId, MessagesHash: p.MessagesHash})
	require.ErrorIs(t, err, types.ErrNotSigner, "tombstone refused")
	// dave inherits alice's approval: cannot approve twice.
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: dave, ProposalId: p.ProposalId, MessagesHash: p.MessagesHash})
	require.ErrorIs(t, err, types.ErrAlreadyApproved)
	require.Equal(t, uint64(1), f.keeper.Tally(f.ctx, v, p), "the inherited approval still counts once")
	// dave proposes and executes as a signer.
	p2 := propose(t, f, dave, v.VaultId, anySend(t, v.Address, sink, 2))
	require.Equal(t, []string{dave}, p2.Approvals, "recorded under the canonical address")
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: bob, ProposalId: p2.ProposalId, MessagesHash: p2.MessagesHash})
	require.NoError(t, err)
	_, err = f.srv.Execute(f.ctx, &types.MsgExecute{Signer: dave, ProposalId: p2.ProposalId})
	require.NoError(t, err)
	// dave (as alice) cancels alice's proposal.
	_, err = f.srv.CancelProposal(f.ctx, &types.MsgCancelProposal{Signer: dave, ProposalId: p.ProposalId})
	require.NoError(t, err)
	// Query by signer unions over every address of the member.
	q := keeperQuery(f)
	res, err := q.VaultsBySigner(f.ctx, &types.QueryVaultsBySignerRequest{Address: dave})
	require.NoError(t, err)
	require.Len(t, res.Vaults, 1)
	require.Equal(t, v.VaultId, res.Vaults[0].VaultId)
	// The vault record itself is untouched: no config bump.
	v2, _ := f.keeper.Vault.Get(f.ctx, v.VaultId)
	require.Equal(t, uint64(1), v2.ConfigVersion)
}

func TestGenesisRoundTripAndInvariants(t *testing.T) {
	f := initFixture(t)
	v := create2of3(t, f)
	p := propose(t, f, alice, v.VaultId, anySend(t, v.Address, sink, 1))
	done := propose(t, f, bob, v.VaultId, anySend(t, v.Address, sink, 1))
	_, err := f.srv.CancelProposal(f.ctx, &types.MsgCancelProposal{Signer: bob, ProposalId: done.ProposalId})
	require.NoError(t, err)

	gen, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, gen.VaultMap, 1)
	require.Len(t, gen.VaultProposalMap, 2)

	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *gen))
	require.True(t, g.keeper.IsVault(g.ctx, v.Address))
	has, _ := g.keeper.PendingByVault.Has(g.ctx, collections.Join(v.VaultId, p.ProposalId))
	require.True(t, has)
	has, _ = g.keeper.RetentionQueue.Has(g.ctx, collections.Join(f.ctx.BlockTime().Unix(), done.ProposalId))
	require.True(t, has)
	// Sequences continue after the imported ids.
	g.bank.balances[creator] = math.NewInt(100_000_000)
	nv, err := g.srv.CreateVault(g.ctx, &types.MsgCreateVault{Creator: creator, Signers: signers(map[string]uint64{alice: 1}, alice), Threshold: 1, ProposalTtl: 3600})
	require.NoError(t, err)
	require.Equal(t, uint64(2), nv.VaultId)

	for _, fx := range []*fixture{f, g} {
		for _, inv := range []sdk.Invariant{keeperVaultInvariant(fx), keeperProposalInvariant(fx)} {
			msg, broken := inv(fx.ctx)
			require.False(t, broken, msg)
		}
	}
	// Invariant catches a dangling index.
	require.NoError(t, g.keeper.VaultsBySigner.Set(g.ctx, collections.Join(dave, uint64(1))))
	_, broken := keeperVaultInvariant(g)(g.ctx)
	require.True(t, broken)
}

func TestQueries(t *testing.T) {
	f := initFixture(t)
	v := create2of3(t, f)
	p := propose(t, f, alice, v.VaultId, anySend(t, v.Address, sink, 1))
	q := keeperQuery(f)

	byAddr, err := q.VaultByAddress(f.ctx, &types.QueryVaultByAddressRequest{Address: v.Address})
	require.NoError(t, err)
	require.Equal(t, v.VaultId, byAddr.Vault.VaultId)
	_, err = q.VaultByAddress(f.ctx, &types.QueryVaultByAddressRequest{Address: alice})
	require.Error(t, err)

	tally, err := q.Tally(f.ctx, &types.QueryTallyRequest{ProposalId: p.ProposalId})
	require.NoError(t, err)
	require.Equal(t, uint64(1), tally.ApprovedWeight)
	require.Equal(t, uint64(2), tally.Threshold)
	require.False(t, tally.Met)
	_, err = f.srv.Approve(f.ctx, &types.MsgApprove{Signer: bob, ProposalId: p.ProposalId, MessagesHash: p.MessagesHash})
	require.NoError(t, err)
	tally, _ = q.Tally(f.ctx, &types.QueryTallyRequest{ProposalId: p.ProposalId})
	require.True(t, tally.Met)

	// VLT-08: met follows Execute's expiry rule. One second before expires_at
	// it is met; at expires_at, before the EndBlocker marks the proposal
	// EXPIRED, it is not, and MsgExecute fails too.
	at := func(unix int64) sdk.Context { return f.ctx.WithBlockTime(time.Unix(unix, 0)) }
	tally, _ = q.Tally(at(p.ExpiresAt-1), &types.QueryTallyRequest{ProposalId: p.ProposalId})
	require.True(t, tally.Met)
	tally, _ = q.Tally(at(p.ExpiresAt), &types.QueryTallyRequest{ProposalId: p.ProposalId})
	require.False(t, tally.Met, "expired but not yet swept")
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_PENDING, proposalStatus(t, f, p.ProposalId))
	_, err = f.srv.Execute(at(p.ExpiresAt), &types.MsgExecute{Signer: alice, ProposalId: p.ProposalId})
	require.ErrorIs(t, err, types.ErrProposalExpired)

	list, err := q.ProposalsByVault(f.ctx, &types.QueryProposalsByVaultRequest{VaultId: v.VaultId})
	require.NoError(t, err)
	require.Len(t, list.Proposals, 1)
	list, err = q.ProposalsByVault(f.ctx, &types.QueryProposalsByVaultRequest{VaultId: v.VaultId, Status: types.ProposalStatus_PROPOSAL_STATUS_EXECUTED})
	require.NoError(t, err)
	require.Empty(t, list.Proposals)

	bs, err := q.VaultsBySigner(f.ctx, &types.QueryVaultsBySignerRequest{Address: carol})
	require.NoError(t, err)
	require.Len(t, bs.Vaults, 1)
	bs, err = q.VaultsBySigner(f.ctx, &types.QueryVaultsBySignerRequest{Address: dave})
	require.NoError(t, err)
	require.Empty(t, bs.Vaults)
}

// Theme 6: a vault proposal skips the ante, so it may not carry a commons
// MsgSubmitProposal (fee floor is ante-only) nor mix gnovm VM messages with
// other messages (the gnovm ante's no-mix rule).
func TestInnerMsgAnteOnlyRules(t *testing.T) {
	f := initFixture(t)
	v := create2of3(t, f)
	submit := &codectypes.Any{TypeUrl: "/sparkdream.commons.v1.MsgSubmitProposal"}
	_, err := f.srv.Propose(f.ctx, &types.MsgPropose{Signer: alice, VaultId: v.VaultId, Messages: []*codectypes.Any{submit}})
	require.ErrorIs(t, err, types.ErrInnerMsgDenied)

	gno := &codectypes.Any{TypeUrl: "/gnovm.gnovm.v1.MsgCall"}
	_, err = f.srv.Propose(f.ctx, &types.MsgPropose{Signer: alice, VaultId: v.VaultId, Messages: []*codectypes.Any{gno, anySend(t, v.Address, sink, 1)}})
	require.ErrorIs(t, err, types.ErrInnerMsgDenied)
}
