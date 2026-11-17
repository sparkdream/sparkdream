package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	servicetypes "sparkdream/x/service/types"
	"sparkdream/x/storage/types"
)

func TestCompletionRules(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	f.claim(w.operator, id)
	try := func(mut func(m *types.MsgCompleteRequest)) error {
		m := completeMsg(w.operator, id)
		mut(m)
		_, err := f.srv.CompleteRequest(f.ctx, m)
		return err
	}
	require.ErrorIs(t, try(func(m *types.MsgCompleteRequest) { m.Operator = addr("someone") }), types.ErrNotClaimant)
	require.ErrorIs(t, try(func(m *types.MsgCompleteRequest) { m.Locator = "short" }), types.ErrInvalidLocator)
	require.ErrorIs(t, try(func(m *types.MsgCompleteRequest) { m.Attestations = nil }), types.ErrInvalidAttestations)
	require.ErrorIs(t, try(func(m *types.MsgCompleteRequest) { m.Attestations = []string{"zz"} }), types.ErrInvalidAttestations)
	require.ErrorIs(t, try(func(m *types.MsgCompleteRequest) { m.Attestations = append(m.Attestations, m.Attestations[0]) }), types.ErrInvalidAttestations)
	require.ErrorIs(t, try(func(m *types.MsgCompleteRequest) { m.VerdictsLocator = "" }), types.ErrInvalidAttestations)
	require.ErrorIs(t, try(func(m *types.MsgCompleteRequest) { m.VerdictsLocator = m.Locator }), types.ErrInvalidAttestations)
	require.ErrorIs(t, try(func(m *types.MsgCompleteRequest) {
		for i := 0; i < 9; i++ {
			m.Attestations = append(m.Attestations, hashOf(string(rune('a'+i))))
		}
	}), types.ErrInvalidAttestations)
	require.NoError(t, try(func(*types.MsgCompleteRequest) {}))
	require.ErrorIs(t, try(func(*types.MsgCompleteRequest) {}), types.ErrNotClaimant) // already completed
}

// A type listed with scanned = false completes with no verdicts, and must
// carry none.
func TestUnscannedCompletion(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setBackend(func(b *types.Backend) {
		b.Types = append(b.Types, types.BackendType{ContentType: "text/plain", Scanned: false, MaxSize: 4096})
	})
	m := reqMsg(w.requester, "notes")
	m.ContentType = "text/plain"
	res, err := f.srv.RequestPermanentCopy(f.ctx, m)
	require.NoError(t, err)
	f.claim(w.operator, res.RequestId)
	require.Equal(t, uint32(0), f.get(res.RequestId).RequiredAttestations)

	_, err = f.srv.CompleteRequest(f.ctx, completeMsg(w.operator, res.RequestId))
	require.ErrorIs(t, err, types.ErrInvalidAttestations)
	_, err = f.srv.CompleteRequest(f.ctx, &types.MsgCompleteRequest{Operator: w.operator, RequestId: res.RequestId, Locator: locator("notes")})
	require.NoError(t, err)
}

// A stolen operator key completing in a loop hits the per-window cap.
func TestCompletionCap(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setParams(func(p *types.Params) { p.MaxCompletionsPerOperatorPerWindow = 2 })
	ids := []uint64{f.request(w.requester, "a"), f.request(w.requester, "b"), f.request(w.requester, "c")}
	for _, id := range ids {
		f.claim(w.operator, id)
	}
	f.complete(w.operator, ids[0])
	f.complete(w.operator, ids[1])
	_, err := f.srv.CompleteRequest(f.ctx, completeMsg(w.operator, ids[2]))
	require.ErrorIs(t, err, types.ErrRateLimited)
}

// An operator whose bond is still held may finish what it claimed; one with
// no live record may not.
func TestCompletionByADepartingOperator(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	a, b := f.request(w.requester, "a"), f.request(w.requester, "b")
	f.claim(w.operator, a)
	f.claim(w.operator, b)

	f.setStatus(w.operator, types.ArweaveServiceType, servicetypes.OperatorStatus_OPERATOR_STATUS_UNBONDING)
	f.complete(w.operator, a)

	delete(f.service.ops, opKey(w.operator, types.ArweaveServiceType))
	_, err := f.srv.CompleteRequest(f.ctx, completeMsg(w.operator, b))
	require.ErrorIs(t, err, types.ErrOperatorNotActive)

	// The completion by an operator that merely left settles as usual.
	f.runTo(f.get(a).SettleAt + 1)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_SETTLED, f.get(a).Status)
}

// A completion by an operator slashed out of x/service before settlement is
// voided, not paid.
func TestSettlementVoidsASlashedOperator(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.completed(w, "photo")
	delete(f.service.ops, opKey(w.operator, types.ArweaveServiceType))
	f.service.slashed[opKey(w.operator, types.ArweaveServiceType)] = true

	settleAt := f.get(id).SettleAt
	f.runTo(settleAt + 1)
	r := f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, r.Status)
	require.Equal(t, []string{w.operator}, r.Barred)
	require.Empty(t, r.Locator)
	require.Equal(t, settleAt, r.OpenedAt)
	require.Equal(t, copyFee(), f.escrow())
	require.Equal(t, int64(funds), f.bal(w.council))
}

// A payout to a controller that is no longer a council goes to the
// community pool; the block continues.
func TestPayoutRedirected(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.completed(w, "photo")
	f.commons.groups[w.council] = false
	f.runTo(f.get(id).SettleAt + 1)
	r := f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_SETTLED, r.Status)
	require.Empty(t, r.PaidTo)
	require.Equal(t, copyFee(), r.PaidAmount.Int64())
	require.Equal(t, copyFee(), f.distr.pool.Int64())

	// No council can return that fee: only governance may void.
	_, err := f.srv.VoidCompletion(f.ctx, &types.MsgVoidCompletion{Authority: w.council, RequestId: id})
	require.ErrorIs(t, err, types.ErrUnauthorized)
}

func (f *fixture) dispute(member string, id uint64) error {
	_, err := f.srv.DisputeCompletion(f.ctx, &types.MsgDisputeCompletion{Member: member, RequestId: id, MaxFee: math.NewInt(funds)})
	return err
}

// A bogus locator disputed by the requester and voided by governance while
// the controller does nothing.
func TestDisputeThenGovernanceVoid(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	p := f.params()
	id := f.completed(w, "photo")
	settleAt := f.get(id).SettleAt

	before := f.bal(w.requester)
	require.NoError(t, f.dispute(w.requester, id))
	require.Equal(t, before, f.bal(w.requester)) // the requester disputes for nothing
	r := f.get(id)
	require.True(t, r.Disputed)
	require.Equal(t, f.ctx.BlockHeight()+p.DisputeWindowBlocks, r.SettleAt)
	require.ErrorIs(t, f.dispute(w.requester, id), types.ErrAlreadyDisputed)

	// The first settle time passes without a payout.
	f.runTo(settleAt + 2)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_COMPLETED, f.get(id).Status)

	_, err := f.srv.VoidCompletion(f.ctx, &types.MsgVoidCompletion{Authority: addr("someone"), RequestId: id})
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, err = f.srv.VoidCompletion(f.ctx, &types.MsgVoidCompletion{Authority: f.gov, RequestId: id, Reason: "locator serves nothing"})
	require.NoError(t, err)
	require.True(t, f.hasEvent("storage.completion_voided"))

	r = f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, r.Status)
	require.Equal(t, f.ctx.BlockHeight(), r.OpenedAt)
	require.Equal(t, f.ctx.BlockHeight()+p.RequestTtlBlocks, r.Deadline)
	require.Equal(t, []string{w.operator}, r.Barred)
	require.False(t, r.Disputed)
	require.Empty(t, r.Locator)
	require.Empty(t, r.Attestations)
	require.Empty(t, r.Controller)
	require.Zero(t, r.RequiredAttestations)
	require.Equal(t, copyFee(), f.escrow())
	f.checkInvariants()

	// The request starts again with another operator, and the new completion
	// can be disputed in its turn.
	second := f.operator("second", w.council)
	f.claim(second, id)
	f.complete(second, id)
	require.NoError(t, f.dispute(w.requester, id))
}

// The requester may be the party that gains from a false completion, so any
// eligible member can dispute, for the request fee.
func TestDisputeByAnotherMember(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	other := f.member("other", reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	id := f.completed(w, "photo")

	require.ErrorIs(t, f.dispute(addr("stranger"), id), types.ErrRequesterNotPermitted)
	_, err := f.srv.DisputeCompletion(f.ctx, &types.MsgDisputeCompletion{Member: other, RequestId: id, MaxFee: math.ZeroInt()})
	require.ErrorIs(t, err, types.ErrFeeExceedsMax)

	burned := f.bank.burned
	require.NoError(t, f.dispute(other, id))
	require.Equal(t, reqFee(), f.bank.burned.Sub(burned).Int64())
	require.Equal(t, int64(funds)-reqFee(), f.bal(other))
}

func TestDisputeAfterSettleTimeRefused(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.completed(w, "photo")
	// At settle_at, before the EndBlocker has reached the entry.
	f.ctx = f.ctx.WithBlockHeight(f.get(id).SettleAt)
	require.ErrorIs(t, f.dispute(w.requester, id), types.ErrSettleTimePassed)

	pending := f.request(w.requester, "other")
	require.ErrorIs(t, f.dispute(w.requester, pending), types.ErrWrongStatus)
}

// A dispute nobody answers settles to the council at the new time.
func TestUnansweredDisputeSettles(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.completed(w, "photo")
	require.NoError(t, f.dispute(w.requester, id))
	f.runTo(f.get(id).SettleAt + 1)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_SETTLED, f.get(id).Status)
}

func TestVoidByTheController(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.completed(w, "photo")
	other := f.council("other-council")
	_, err := f.srv.VoidCompletion(f.ctx, &types.MsgVoidCompletion{Authority: other, RequestId: id})
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, err = f.srv.VoidCompletion(f.ctx, &types.MsgVoidCompletion{Authority: w.council, RequestId: id, Reason: string(make([]byte, 300))})
	require.ErrorIs(t, err, types.ErrInvalidReason)
	_, err = f.srv.VoidCompletion(f.ctx, &types.MsgVoidCompletion{Authority: w.council, RequestId: id, Reason: "wrong bytes"})
	require.NoError(t, err)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, f.get(id).Status)

	// A pending request has no completion to void.
	_, err = f.srv.VoidCompletion(f.ctx, &types.MsgVoidCompletion{Authority: f.gov, RequestId: id})
	require.ErrorIs(t, err, types.ErrWrongStatus)
}

// A void clears a barred list filled by earlier lapses, so the request is
// claimable again; only the voided operator stays barred.
func TestVoidClearsTheLists(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	second := f.operator("second", w.council)
	id := f.request(w.requester, "photo")
	f.claim(second, id)
	f.runTo(f.get(id).ClaimExpiresAt + 1) // second lapses and is barred
	f.claim(w.operator, id)
	f.complete(w.operator, id)

	_, err := f.srv.VoidCompletion(f.ctx, &types.MsgVoidCompletion{Authority: w.council, RequestId: id})
	require.NoError(t, err)
	require.Equal(t, []string{w.operator}, f.get(id).Barred)
	require.NoError(t, f.claimErr(second, id))
}

// A council that voids its own settled copy returns the fee: the request is
// funded again and refundable.
func TestCouncilVoidsItsSettledCopy(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.completed(w, "photo")
	f.runTo(f.get(id).SettleAt + 1)
	require.Equal(t, int64(funds)+copyFee(), f.bal(w.council))

	_, err := f.srv.VoidCompletion(f.ctx, &types.MsgVoidCompletion{Authority: w.council, RequestId: id, Reason: "copy never confirmed"})
	require.NoError(t, err)
	r := f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, r.Status)
	require.Equal(t, copyFee(), r.CopyFee.Int64())
	require.Empty(t, r.PaidTo)
	require.True(t, r.PaidAmount.IsZero())
	require.Equal(t, int64(funds), f.bal(w.council))
	require.Equal(t, copyFee(), f.escrow())
	f.checkInvariants()

	_, err = f.srv.CancelRequest(f.ctx, &types.MsgCancelRequest{Requester: w.requester, RequestId: id})
	require.NoError(t, err)
	require.Equal(t, int64(funds)-reqFee(), f.bal(w.requester))
}

// A governance void of a settled copy closes the request, frees the hash and
// records what the council owes. The fee stays where it was paid.
func TestGovernanceVoidsASettledCopy(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.completed(w, "photo")
	f.runTo(f.get(id).SettleAt + 1)

	_, err := f.srv.VoidCompletion(f.ctx, &types.MsgVoidCompletion{Authority: f.gov, RequestId: id, Reason: "council would not act"})
	require.NoError(t, err)
	require.True(t, f.hasEvent("storage.settled_copy_voided"))
	r := f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_REJECTED, r.Status)
	require.Equal(t, types.ClosedReason_CLOSED_REASON_VOIDED, r.ClosedReason)
	require.Equal(t, w.council, r.PaidTo)
	require.Equal(t, int64(funds)+copyFee(), f.bal(w.council))
	f.checkInvariants()

	// The file can be requested again.
	f.request(w.requester, "photo")
}

// A flagged hash is never voided back to life.
func TestVoidRefusedWhileFlagged(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.completed(w, "photo")
	f.runTo(f.get(id).SettleAt + 1)
	f.flag(w.scanner, "photo")
	for _, authority := range []string{w.council, f.gov} {
		_, err := f.srv.VoidCompletion(f.ctx, &types.MsgVoidCompletion{Authority: authority, RequestId: id})
		require.ErrorIs(t, err, types.ErrHashRemoved)
	}
}
