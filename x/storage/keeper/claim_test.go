package keeper_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	servicetypes "sparkdream/x/service/types"
	"sparkdream/x/storage/types"
)

func (f *fixture) claimErr(operator string, id uint64) error {
	_, err := f.srv.ClaimRequest(f.ctx, &types.MsgClaimRequest{Operator: operator, RequestId: id})
	return err
}

func TestClaimRules(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")

	// Registered in x/service but not approved here.
	unapproved := f.member("unapproved", reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	f.register(unapproved, types.ArweaveServiceType, w.council)
	require.ErrorIs(t, f.claimErr(unapproved, id), types.ErrOperatorNotApproved)

	f.setStatus(w.operator, types.ArweaveServiceType, servicetypes.OperatorStatus_OPERATOR_STATUS_UNBONDING)
	require.ErrorIs(t, f.claimErr(w.operator, id), types.ErrOperatorNotActive)
	f.setStatus(w.operator, types.ArweaveServiceType, servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE)

	f.rep.active[w.operator] = false
	require.ErrorIs(t, f.claimErr(w.operator, id), types.ErrOperatorNotMember)
	f.rep.active[w.operator] = true

	require.NoError(t, f.claimErr(w.operator, id))
	require.ErrorIs(t, f.claimErr(w.operator, id), types.ErrWrongStatus)
}

// One account requesting and completing its own request is refused.
func TestClaimSelfDealing(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.operator, "photo") // the operator is a member too
	require.ErrorIs(t, f.claimErr(w.operator, id), types.ErrSelfDealing)

	// Also through an earlier address of the same member.
	f.rep.canonicalOf[w.requester] = w.operator
	id2 := f.request(w.operator, "other")
	r := f.get(id2)
	r.Requester = w.requester
	require.NoError(t, f.keeper.Requests.Set(f.ctx, id2, r))
	require.ErrorIs(t, f.claimErr(w.operator, id2), types.ErrSelfDealing)
}

// An approval counts only while x/service still names the controller that
// gave it.
func TestApprovalVoidAfterControllerTransfer(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	other := f.council("other-council")
	f.register(w.operator, types.ArweaveServiceType, other)
	require.ErrorIs(t, f.claimErr(w.operator, id), types.ErrOperatorNotApproved)

	// The new controller approves again.
	_, err := f.srv.ApproveOperator(f.ctx, &types.MsgApproveOperator{Authority: other, Backend: types.ArweaveBackend, Operator: w.operator})
	require.NoError(t, err)
	require.NoError(t, f.claimErr(w.operator, id))
	require.Equal(t, other, f.get(id).Controller)
}

// The quorum and the controller are fixed at the claim.
func TestClaimFixesQuorumAndController(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	f.claim(w.operator, id)

	// Governance raises the quorum and the controller changes: the
	// completion needs the recorded quorum and pays the recorded council.
	c := f.service.configs[types.ContentScannerServiceType]
	c.AttestationQuorum = 3
	f.service.configs[types.ContentScannerServiceType] = c
	other := f.council("other-council")
	f.register(w.operator, types.ArweaveServiceType, other)

	f.complete(w.operator, id)
	f.runTo(f.get(id).SettleAt + 1)
	r := f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_SETTLED, r.Status)
	require.Equal(t, w.council, r.PaidTo)
	require.Equal(t, int64(funds), f.bal(other))
}

// NEW and PROVISIONAL requesters need the elevated quorum when one is set.
func TestElevatedQuorum(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.scanner("scanner2", w.council)
	c := f.service.configs[types.ContentScannerServiceType]
	c.ElevatedAttestationQuorum = 2
	f.service.configs[types.ContentScannerServiceType] = c

	low := f.member("provisional", reptypes.TrustLevel_TRUST_LEVEL_PROVISIONAL)
	a := f.request(low, "a")
	b := f.request(w.requester, "b")
	f.claim(w.operator, a)
	f.claim(w.operator, b)
	require.Equal(t, uint32(2), f.get(a).RequiredAttestations)
	require.Equal(t, uint32(1), f.get(b).RequiredAttestations)

	_, err := f.srv.CompleteRequest(f.ctx, completeMsg(w.operator, a)) // lists one verdict
	require.ErrorIs(t, err, types.ErrInvalidAttestations)
}

func TestClaimRefusedWithoutAScannerQuorum(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	// The one approved scanner stops being active.
	f.setStatus(w.scanner, types.ContentScannerServiceType, servicetypes.OperatorStatus_OPERATOR_STATUS_UNBONDING)
	require.ErrorIs(t, f.claimErr(w.operator, id), types.ErrNoScannerQuorum)
}

func TestOpenClaimsCap(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setParams(func(p *types.Params) { p.MaxOpenClaimsPerOperator = 2 })
	a, b, c := f.request(w.requester, "a"), f.request(w.requester, "b"), f.request(w.requester, "c")
	f.claim(w.operator, a)
	f.claim(w.operator, b)
	require.ErrorIs(t, f.claimErr(w.operator, c), types.ErrTooManyClaims)
	// A completion frees the slot: only CLAIMED requests count.
	f.complete(w.operator, a)
	require.NoError(t, f.claimErr(w.operator, c))
}

func TestExtendClaimOnce(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	p := f.params()
	id := f.request(w.requester, "photo")
	f.claim(w.operator, id)
	before := f.get(id).ClaimExpiresAt
	ext := func(op string) error {
		_, err := f.srv.ExtendClaim(f.ctx, &types.MsgExtendClaim{Operator: op, RequestId: id})
		return err
	}
	require.ErrorIs(t, ext(addr("someone")), types.ErrNotClaimant)
	require.NoError(t, ext(w.operator))
	require.Equal(t, before+p.ClaimWindowBlocks, f.get(id).ClaimExpiresAt)
	require.ErrorIs(t, ext(w.operator), types.ErrClaimAlreadyExtended)

	// The old timer is gone: the claim survives its first expiry height.
	f.runTo(before + 1)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_CLAIMED, f.get(id).Status)
	f.runTo(before + p.ClaimWindowBlocks + 1)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, f.get(id).Status)
}

func TestExtendAfterExpiryRefused(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	f.claim(w.operator, id)
	// Past the height, before the EndBlocker has reached the entry.
	f.ctx = f.ctx.WithBlockHeight(f.get(id).ClaimExpiresAt + 1)
	_, err := f.srv.ExtendClaim(f.ctx, &types.MsgExtendClaim{Operator: w.operator, RequestId: id})
	require.ErrorIs(t, err, types.ErrClaimExpired)
	_, err = f.srv.CompleteRequest(f.ctx, completeMsg(w.operator, id))
	require.ErrorIs(t, err, types.ErrClaimExpired)
	_, err = f.srv.DeclineRequest(f.ctx, &types.MsgDeclineRequest{Operator: w.operator, RequestId: id, Reason: types.DeclineReason_DECLINE_REASON_MISMATCH})
	require.ErrorIs(t, err, types.ErrClaimExpired)
}

// Claim squatting: a lapse bars the operator and never moves the deadline.
func TestLapseBarsAndKeepsTheDeadline(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	deadline := f.get(id).Deadline
	f.claim(w.operator, id)
	f.runTo(f.get(id).ClaimExpiresAt + 1)

	r := f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, r.Status)
	require.Equal(t, deadline, r.Deadline)
	require.Equal(t, []string{w.operator}, r.Barred)
	require.Empty(t, r.Operator)
	require.ErrorIs(t, f.claimErr(w.operator, id), types.ErrOperatorBarred)

	// A migration does not lift the bar.
	moved := f.operator("operator-new", w.council)
	f.rep.canonicalOf[w.operator] = moved
	require.ErrorIs(t, f.claimErr(moved, id), types.ErrOperatorBarred)

	// Another operator can still take it.
	second := f.operator("second", w.council)
	require.NoError(t, f.claimErr(second, id))
}

// A claim pauses the deadline: no refund mid-upload. A lapse past the
// deadline expires at once.
func TestClaimOutlivesTheDeadline(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setParams(func(p *types.Params) { p.RequestTtlBlocks = p.ClaimWindowBlocks + 2 })
	id := f.request(w.requester, "photo")
	deadline := f.get(id).Deadline
	f.runTo(deadline - 1)
	f.claim(w.operator, id)
	claimEnd := f.get(id).ClaimExpiresAt
	require.Greater(t, claimEnd, deadline)

	f.runTo(deadline + 2)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_CLAIMED, f.get(id).Status)
	require.Equal(t, copyFee(), f.escrow())

	f.runTo(claimEnd + 1)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_EXPIRED, f.get(id).Status)
	require.Equal(t, int64(funds)-reqFee(), f.bal(w.requester))
}

func TestClaimAfterDeadlineRefused(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	f.ctx = f.ctx.WithBlockHeight(f.get(id).Deadline + 1) // the EndBlocker has not run
	require.ErrorIs(t, f.claimErr(w.operator, id), types.ErrDeadlinePassed)
}

// More lapses than the limit stop the operator claiming and file one report.
func TestLapseLimit(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setParams(func(p *types.Params) { p.MaxLapsesPerWindow = 2; p.CounterWindowBlocks = 20 })
	var ids []uint64
	for _, c := range []string{"a", "b", "c", "d"} {
		ids = append(ids, f.request(w.requester, c))
	}
	for _, id := range ids[:3] {
		f.claim(w.operator, id)
	}
	f.runTo(f.get(ids[0]).ClaimExpiresAt + 1)
	require.ErrorIs(t, f.claimErr(w.operator, ids[3]), types.ErrLapseLimit)
	require.Len(t, f.service.reports, 1)
	require.Equal(t, w.operator, f.service.reports[0].operator)

	// The window rolls.
	f.ctx = f.ctx.WithBlockHeight(f.ctx.BlockHeight() + f.params().CounterWindowBlocks)
	require.NoError(t, f.claimErr(w.operator, ids[3]))
}

// A failing report never undoes the lapse.
func TestLapseReportFailureIsNotFatal(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setParams(func(p *types.Params) { p.MaxLapsesPerWindow = 1 })
	f.service.reportErr = errors.New("rate limited")
	a, b := f.request(w.requester, "a"), f.request(w.requester, "b")
	f.claim(w.operator, a)
	f.claim(w.operator, b)
	f.runTo(f.get(a).ClaimExpiresAt + 1)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, f.get(a).Status)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, f.get(b).Status)
}

// An operator with no live record leaves its claims to lapse; no lapse is
// counted against it.
func TestLapseOfADepartedOperator(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	f.claim(w.operator, id)
	delete(f.service.ops, opKey(w.operator, types.ArweaveServiceType))
	f.runTo(f.get(id).ClaimExpiresAt + 1)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, f.get(id).Status)
	has, err := f.keeper.Counters.Has(f.ctx, pairKey(w.operator, types.CounterKind_COUNTER_KIND_LAPSE))
	require.NoError(t, err)
	require.False(t, has)
}

// Once the barred list is full nobody may claim; the request waits out its
// deadline.
func TestBarredListFull(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setParams(func(p *types.Params) { p.MaxBarredPerRequest = 1 })
	id := f.request(w.requester, "photo")
	f.claim(w.operator, id)
	f.runTo(f.get(id).ClaimExpiresAt + 1)
	second := f.operator("second", w.council)
	require.ErrorIs(t, f.claimErr(second, id), types.ErrBarredListFull)
	_, err := f.srv.DeclineRequest(f.ctx, &types.MsgDeclineRequest{Operator: second, RequestId: id, Reason: types.DeclineReason_DECLINE_REASON_MISMATCH})
	require.ErrorIs(t, err, types.ErrBarredListFull)
}
