package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	servicetypes "sparkdream/x/service/types"
	"sparkdream/x/storage/types"
)

const (
	mismatch    = types.DeclineReason_DECLINE_REASON_MISMATCH
	unreachable = types.DeclineReason_DECLINE_REASON_UNREACHABLE
)

func (f *fixture) decline(operator string, id uint64, reason types.DeclineReason) error {
	_, err := f.srv.DeclineRequest(f.ctx, &types.MsgDeclineRequest{Operator: operator, RequestId: id, Reason: reason})
	return err
}

// With declines_to_close = 1 (one operator), a single decline closes the
// request and refunds it in full.
func TestDeclineClosesWithOneOperator(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	require.NoError(t, f.decline(w.operator, id, mismatch))
	r := f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_REJECTED, r.Status)
	require.Equal(t, types.ClosedReason_CLOSED_REASON_DECLINED, r.ClosedReason)
	require.Equal(t, int64(funds)-reqFee(), f.bal(w.requester))
	f.checkInvariants()
	// The file can be requested again, correctly declared this time.
	f.request(w.requester, "photo")
}

// No single operator closes a request once the quorum is 2: declines must
// come from different members.
func TestDeclineQuorum(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setBackend(func(b *types.Backend) { b.DeclinesToClose = 2 })
	second := f.operator("second", w.council)
	id := f.request(w.requester, "photo")

	require.NoError(t, f.decline(w.operator, id, mismatch))
	r := f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, r.Status)
	require.Equal(t, []string{w.operator}, r.Barred)
	// One decline per operator per request.
	require.ErrorIs(t, f.decline(w.operator, id, mismatch), types.ErrOperatorBarred)
	require.ErrorIs(t, f.claimErr(w.operator, id), types.ErrOperatorBarred)

	require.NoError(t, f.decline(second, id, mismatch))
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_REJECTED, f.get(id).Status)
}

// Two operators of one member count once.
func TestDeclineCountsMembersNotAddresses(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setBackend(func(b *types.Backend) { b.DeclinesToClose = 2 })
	alt := f.operator("operator-alt", w.council)
	id := f.request(w.requester, "photo")

	require.NoError(t, f.decline(w.operator, id, mismatch))
	// The member migrates between its two declines.
	f.rep.canonicalOf[w.operator] = alt
	// The barred check reads earlier addresses, so the same member cannot
	// decline again through its new address.
	require.ErrorIs(t, f.decline(alt, id, mismatch), types.ErrOperatorBarred)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, f.get(id).Status)

	third := f.operator("third", w.council)
	require.NoError(t, f.decline(third, id, mismatch))
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_REJECTED, f.get(id).Status)
}

func TestDeclineRules(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	p := f.params()
	id := f.request(w.requester, "photo")

	require.ErrorIs(t, f.decline(w.operator, id, types.DeclineReason_DECLINE_REASON_UNSPECIFIED), types.ErrInvalidDecline)
	require.ErrorIs(t, f.decline(addr("stranger"), id, mismatch), types.ErrOperatorNotApproved)
	f.setStatus(w.operator, types.ArweaveServiceType, servicetypes.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED)
	require.ErrorIs(t, f.decline(w.operator, id, mismatch), types.ErrOperatorNotActive)
	f.setStatus(w.operator, types.ArweaveServiceType, servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE)

	// Unreachable only after the retry period from the newest source.
	require.ErrorIs(t, f.decline(w.operator, id, unreachable), types.ErrDeclineTooEarly)
	f.run(p.MinUnreachableBlocks)
	require.NoError(t, f.decline(w.operator, id, unreachable))

	// Not once completed.
	done := f.request(w.requester, "done")
	second := f.operator("second", w.council)
	f.claim(second, done)
	f.complete(second, done)
	require.ErrorIs(t, f.decline(w.operator, done, mismatch), types.ErrInvalidDecline)
}

// The claimant's decline releases the claim without a lapse and bars it.
func TestDeclineByTheClaimant(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setBackend(func(b *types.Backend) { b.DeclinesToClose = 2 })
	second := f.operator("second", w.council)
	id := f.request(w.requester, "photo")
	f.claim(w.operator, id)

	require.ErrorIs(t, f.decline(second, id, mismatch), types.ErrNotClaimant)
	require.NoError(t, f.decline(w.operator, id, mismatch))
	r := f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, r.Status)
	require.Empty(t, r.Operator)
	require.Equal(t, []string{w.operator}, r.Barred)
	has, err := f.keeper.Counters.Has(f.ctx, pairKey(w.operator, types.CounterKind_COUNTER_KIND_LAPSE))
	require.NoError(t, err)
	require.False(t, has)
	f.checkInvariants()
	f.claim(second, id)
}

// An operator barred by a lapse may not decline the same request.
func TestBarredByLapseCannotDecline(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setBackend(func(b *types.Backend) { b.DeclinesToClose = 2 })
	id := f.request(w.requester, "photo")
	f.claim(w.operator, id)
	f.runTo(f.get(id).ClaimExpiresAt + 1)
	require.ErrorIs(t, f.decline(w.operator, id, mismatch), types.ErrOperatorBarred)
}

// Dead-address requests in bulk use up the unreachable cap and leave the
// mismatch declines available.
func TestDeclineCapsAreSeparate(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setParams(func(p *types.Params) {
		p.MaxUnreachableDeclinesPerOperatorPerWindow = 2
		p.MaxMismatchDeclinesPerOperatorPerWindow = 1
		p.CounterWindowBlocks = 20
	})
	ids := []uint64{f.request(w.requester, "a"), f.request(w.requester, "b"), f.request(w.requester, "c"), f.request(w.requester, "d"), f.request(w.requester, "e")}
	f.run(f.params().MinUnreachableBlocks)
	require.NoError(t, f.decline(w.operator, ids[0], unreachable))
	require.NoError(t, f.decline(w.operator, ids[1], unreachable))
	require.ErrorIs(t, f.decline(w.operator, ids[2], unreachable), types.ErrRateLimited)
	require.NoError(t, f.decline(w.operator, ids[2], mismatch))
	require.ErrorIs(t, f.decline(w.operator, ids[3], mismatch), types.ErrRateLimited)

	// The window rolls for both.
	f.ctx = f.ctx.WithBlockHeight(f.ctx.BlockHeight() + f.params().CounterWindowBlocks)
	require.NoError(t, f.decline(w.operator, ids[3], mismatch))
	require.NoError(t, f.decline(w.operator, ids[4], unreachable))
}

// declines_to_close lowered under an open request closes it at the next
// decline, not before.
func TestDeclinesToCloseChange(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setBackend(func(b *types.Backend) { b.DeclinesToClose = 3 })
	second := f.operator("second", w.council)
	id := f.request(w.requester, "photo")
	require.NoError(t, f.decline(w.operator, id, mismatch))
	f.setBackend(func(b *types.Backend) { b.DeclinesToClose = 1 })
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, f.get(id).Status)
	require.NoError(t, f.decline(second, id, mismatch))
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_REJECTED, f.get(id).Status)
}
