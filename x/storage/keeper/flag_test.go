package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	servicetypes "sparkdream/x/service/types"
	"sparkdream/x/storage/types"
)

func (f *fixture) unflag(authority string, contents ...string) error {
	hashes := make([]string, 0, len(contents))
	for _, c := range contents {
		hashes = append(hashes, hashOf(c))
	}
	_, err := f.srv.UnflagRemoved(f.ctx, &types.MsgUnflagRemoved{Authority: authority, Hashes: hashes})
	return err
}

func (f *fixture) flagErr(scanner, content string) error {
	_, err := f.srv.FlagRemoved(f.ctx, &types.MsgFlagRemoved{Scanner: scanner, Sha256: hashOf(content), AttestationHash: hashOf("v" + content)})
	return err
}

// A flag before a claim refunds in full.
func TestFlagPendingRefunds(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	f.flag(w.scanner, "photo")
	r := f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_REJECTED, r.Status)
	require.Equal(t, types.ClosedReason_CLOSED_REASON_FLAGGED, r.ClosedReason)
	require.Zero(t, r.ForfeitAt)
	require.Equal(t, int64(funds)-reqFee(), f.bal(w.requester))
	f.checkInvariants()

	// Lifting the flag does not bring a refunded request back.
	require.NoError(t, f.unflag(w.council, "photo"))
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_REJECTED, f.get(id).Status)
	f.request(w.requester, "photo")
}

// Only an approved, active scanner flags.
func TestFlagNeedsAnApprovedScanner(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	stranger := addr("stranger")
	f.register(stranger, types.ContentScannerServiceType, w.council)
	require.ErrorIs(t, f.flagErr(stranger, "photo"), types.ErrScannerNotApproved)
	require.ErrorIs(t, f.flagErr(addr("nobody"), "photo"), types.ErrScannerNotApproved)

	require.NoError(t, f.flagErr(w.scanner, "photo"))
	require.ErrorIs(t, f.flagErr(w.scanner, "photo"), types.ErrAlreadyFlagged)
}

// A scanner flagging in bulk hits its cap; a hash holds a bounded number of flags.
func TestFlagCaps(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setParams(func(p *types.Params) { p.MaxFlagsPerScannerPerWindow = 2; p.MaxFlagsPerHash = 1 })
	require.NoError(t, f.flagErr(w.scanner, "a"))
	second := f.scanner("scanner2", w.council)
	require.ErrorIs(t, f.flagErr(second, "a"), types.ErrTooManyFlags)
	require.NoError(t, f.flagErr(w.scanner, "b"))
	require.ErrorIs(t, f.flagErr(w.scanner, "c"), types.ErrRateLimited)
}

// A flag after completion: the fee is held, then burned, and never paid to
// the council.
func TestFlagCompletedHoldsThenBurns(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	p := f.params()
	id := f.completed(w, "photo")
	f.flag(w.scanner, "photo")

	r := f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_REJECTED, r.Status)
	require.Equal(t, f.ctx.BlockHeight()+p.FlagHoldBlocks, r.ForfeitAt)
	require.Equal(t, copyFee(), r.CopyFee.Int64())
	require.Equal(t, copyFee(), f.escrow())
	rec, err := f.keeper.RemovedHashes.Get(f.ctx, hashOf("photo"))
	require.NoError(t, err)
	require.Equal(t, []uint64{id}, rec.Held)

	// Closed beyond the reach of a dispute or a void.
	require.ErrorIs(t, f.dispute(w.requester, id), types.ErrWrongStatus)
	_, err = f.srv.VoidCompletion(f.ctx, &types.MsgVoidCompletion{Authority: f.gov, RequestId: id})
	require.ErrorIs(t, err, types.ErrWrongStatus)

	burned := f.bank.burned
	f.runTo(r.ForfeitAt + 1)
	r = f.get(id)
	require.Zero(t, r.ForfeitAt)
	require.True(t, r.CopyFee.IsZero())
	require.Equal(t, copyFee(), f.bank.burned.Sub(burned).Int64())
	require.Equal(t, int64(funds), f.bal(w.council))
	require.Equal(t, int64(funds)-reqFee()-copyFee(), f.bal(w.requester))
	rec, err = f.keeper.RemovedHashes.Get(f.ctx, hashOf("photo"))
	require.NoError(t, err)
	require.Empty(t, rec.Held)

	// A fee already burned is gone: a later lift reopens nothing.
	require.NoError(t, f.unflag(w.council, "photo"))
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_REJECTED, f.get(id).Status)

	// Pruned after its retention.
	f.runTo(r.ClosedAt + p.TerminalRetentionBlocks + 2)
	require.False(t, f.exists(id))
}

// The same flag lifted first: the request is reopened as COMPLETED with a
// fresh settle window. No refund.
func TestFlagLiftedReopensTheCompletion(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	p := f.params()
	id := f.completed(w, "photo")
	loc := f.get(id).Locator
	f.flag(w.scanner, "photo")
	f.run(5)

	require.NoError(t, f.unflag(w.council, "photo"))
	r := f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_COMPLETED, r.Status)
	require.Equal(t, f.ctx.BlockHeight()+p.SettleWindowBlocks, r.SettleAt)
	require.Equal(t, f.ctx.BlockHeight(), r.OpenedAt)
	require.Zero(t, r.ForfeitAt)
	require.Zero(t, r.ClosedAt)
	require.Equal(t, loc, r.Locator)
	require.Equal(t, int64(funds)-reqFee()-copyFee(), f.bal(w.requester))
	has, err := f.keeper.RemovedHashes.Has(f.ctx, hashOf("photo"))
	require.NoError(t, err)
	require.False(t, has)
	f.checkInvariants()

	f.runTo(r.SettleAt + 1)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_SETTLED, f.get(id).Status)
	require.Equal(t, int64(funds)+copyFee(), f.bal(w.council))
}

// Flags can hold a completion for longer than any window (every
// later flag restarts the hold), so a lift does not restore the completion
// of an operator that began to unbond meanwhile: its bond could be gone
// before the completion can be voided. The request goes back to PENDING.
func TestFlagLiftedDropsTheCompletionOfAnUnbondingOperator(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status servicetypes.OperatorStatus
		kept   bool
	}{
		{"underfunded", servicetypes.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED, true},
		{"unbonding", servicetypes.OperatorStatus_OPERATOR_STATUS_UNBONDING, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			w := f.world()
			p := f.params()
			second := f.scanner("second", w.council)
			id := f.completed(w, "photo")
			f.flag(w.scanner, "photo")
			// A second flag late in the hold carries it past the first.
			f.run(p.FlagHoldBlocks - 2)
			f.flag(second, "photo")
			f.run(p.FlagHoldBlocks - 2)
			f.setStatus(w.operator, types.ArweaveServiceType, tc.status)

			require.NoError(t, f.unflag(w.council, "photo"))
			r := f.get(id)
			require.Equal(t, f.ctx.BlockHeight(), r.OpenedAt)
			require.Zero(t, r.ForfeitAt)
			if tc.kept {
				require.Equal(t, types.RequestStatus_REQUEST_STATUS_COMPLETED, r.Status)
				require.Equal(t, w.operator, r.Operator)
				return
			}
			require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, r.Status)
			require.Empty(t, r.Operator)
			require.Empty(t, r.Locator)
			require.Zero(t, r.CompletedAt)
			require.Equal(t, f.ctx.BlockHeight()+p.RequestTtlBlocks, r.Deadline)
			// The fee is still in escrow: neither refunded nor paid.
			require.Equal(t, copyFee(), f.escrow())
			f.checkInvariants()

			// Another operator makes the copy and its council is paid.
			other := f.operator("other", w.council)
			f.claim(other, id)
			f.complete(other, id)
			f.runTo(f.get(id).SettleAt + 1)
			require.Equal(t, types.RequestStatus_REQUEST_STATUS_SETTLED, f.get(id).Status)
		})
	}
}

// A council that controls a scanner and an operator must not be
// able to flag and lift its way past a dispute. A reopened completion can be
// disputed again, for the full window.
func TestFlagAndLiftDoesNotEraseADispute(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	p := f.params()
	id := f.completed(w, "photo")
	require.NoError(t, f.dispute(w.requester, id))

	f.flag(w.scanner, "photo")
	require.NoError(t, f.unflag(w.council, "photo"))

	r := f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_COMPLETED, r.Status)
	require.False(t, r.Disputed)
	require.NoError(t, f.dispute(w.requester, id))
	r = f.get(id)
	require.Equal(t, f.ctx.BlockHeight()+p.DisputeWindowBlocks, r.SettleAt)

	// Governance still has the dispute window to void.
	f.run(p.SettleWindowBlocks + 2)
	_, err := f.srv.VoidCompletion(f.ctx, &types.MsgVoidCompletion{Authority: f.gov, RequestId: id})
	require.NoError(t, err)
	require.Equal(t, int64(funds), f.bal(w.council))
}

// A flag on a claimed request holds the fee too; lifted first, the request
// returns to PENDING with the former claimant free to claim again.
func TestFlagClaimed(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	p := f.params()
	id := f.request(w.requester, "photo")
	f.claim(w.operator, id)
	f.flag(w.scanner, "photo")
	r := f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_REJECTED, r.Status)
	require.NotZero(t, r.ForfeitAt)
	require.Equal(t, copyFee(), f.escrow())
	// The claim no longer counts against the operator.
	_, err := f.srv.CompleteRequest(f.ctx, completeMsg(w.operator, id))
	require.ErrorIs(t, err, types.ErrNotClaimant)

	require.NoError(t, f.unflag(w.council, "photo"))
	r = f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, r.Status)
	require.Equal(t, f.ctx.BlockHeight()+p.RequestTtlBlocks, r.Deadline)
	require.Empty(t, r.Operator)
	require.Empty(t, r.Barred)
	f.checkInvariants()
	f.claim(w.operator, id)
	f.complete(w.operator, id)
}

func TestFlagClaimedBurnsWhenLeft(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	f.claim(w.operator, id)
	f.flag(w.scanner, "photo")
	burned := f.bank.burned
	f.runTo(f.get(id).ForfeitAt + 1)
	require.Equal(t, copyFee(), f.bank.burned.Sub(burned).Int64())
	require.Equal(t, int64(0), f.escrow())
}

// A flag treats a request still COMPLETED past settle_at as COMPLETED: it
// has not been paid.
func TestFlagBeatsALateSettlement(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.completed(w, "photo")
	f.ctx = f.ctx.WithBlockHeight(f.get(id).SettleAt + 3) // the EndBlocker is behind
	f.flag(w.scanner, "photo")
	require.NotZero(t, f.get(id).ForfeitAt)
	f.run(2)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_REJECTED, f.get(id).Status)
	require.Equal(t, int64(funds), f.bal(w.council))
}

// Two scanners, one hash: a later flag restarts the hold; nothing reopens
// until the last flag is lifted; a council lifts only its own scanners'.
func TestSeveralFlags(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	p := f.params()
	otherCouncil := f.council("other-council")
	second := f.scanner("scanner2", otherCouncil)
	id := f.completed(w, "photo")

	f.flag(w.scanner, "photo")
	first := f.get(id).ForfeitAt
	f.run(p.FlagHoldBlocks - 2)
	f.flag(second, "photo")
	r := f.get(id)
	require.Equal(t, f.ctx.BlockHeight()+p.FlagHoldBlocks, r.ForfeitAt)

	// The first hold's height passes without a burn.
	f.runTo(first + 2)
	require.Equal(t, copyFee(), f.get(id).CopyFee.Int64())

	// One council lifts its own flag: the hash stays flagged.
	require.NoError(t, f.unflag(w.council, "photo"))
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_REJECTED, f.get(id).Status)
	require.ErrorIs(t, f.unflag(w.council, "photo"), types.ErrUnauthorized)

	require.NoError(t, f.unflag(otherCouncil, "photo"))
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_COMPLETED, f.get(id).Status)
}

func TestUnflagRules(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	other := f.council("other-council")
	f.flag(w.scanner, "a")
	f.flag(w.scanner, "b")

	require.ErrorIs(t, f.unflag(addr("someone"), "a"), types.ErrUnauthorized)
	require.ErrorIs(t, f.unflag(other, "a"), types.ErrUnauthorized)
	require.ErrorIs(t, f.unflag(w.council, "never-flagged"), types.ErrNotFlagged)
	require.ErrorIs(t, f.unflag(w.council), types.ErrTooManyHashes)
	require.ErrorIs(t, f.unflag(w.council, "a", "a"), types.ErrTooManyHashes)

	// A batch naming one hash that is refused fails as a whole: the handler
	// returns the error and the transaction's writes are discarded.
	live := f.ctx
	f.ctx, _ = live.CacheContext()
	require.ErrorIs(t, f.unflag(w.council, "a", "never-flagged"), types.ErrNotFlagged)
	f.ctx = live
	has, err := f.keeper.RemovedHashes.Has(f.ctx, hashOf("a"))
	require.NoError(t, err)
	require.True(t, has)

	require.NoError(t, f.unflag(w.council, "a", "b"))

	// Governance lifts any flag, including one whose scanner is gone.
	f.flag(w.scanner, "c")
	delete(f.service.ops, opKey(w.scanner, types.ContentScannerServiceType))
	require.ErrorIs(t, f.unflag(w.council, "c"), types.ErrUnauthorized)
	require.NoError(t, f.unflag(f.gov, "c"))
}

// A flag's cost does not grow with the number of closed requests for the hash.
func TestFlagIgnoresClosedRequests(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setParams(func(p *types.Params) { p.MaxOpenRequestsPerRequester = 3 })
	var closed []uint64
	for i := 0; i < 5; i++ {
		id := f.request(w.requester, "photo")
		_, err := f.srv.CancelRequest(f.ctx, &types.MsgCancelRequest{Requester: w.requester, RequestId: id})
		require.NoError(t, err)
		closed = append(closed, id)
	}
	live := f.request(w.requester, "photo")
	f.flag(w.scanner, "photo")
	require.Equal(t, types.ClosedReason_CLOSED_REASON_FLAGGED, f.get(live).ClosedReason)
	for _, id := range closed {
		require.Equal(t, types.ClosedReason_CLOSED_REASON_CANCELLED, f.get(id).ClosedReason)
	}
}

// A settled copy stays as a record when its hash is flagged; queries stop
// returning where it is.
func TestFlagSettledWithholdsLocation(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.completed(w, "photo")
	f.runTo(f.get(id).SettleAt + 1)
	f.flag(w.scanner, "photo")
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_SETTLED, f.get(id).Status)

	q := keeperQuery(f)
	res, err := q.Request(f.ctx, &types.QueryRequestRequest{Id: id})
	require.NoError(t, err)
	require.True(t, res.Withheld)
	require.Empty(t, res.Request.Locator)
	require.Empty(t, res.Request.VerdictsLocator)
	require.Empty(t, res.Request.Sources[0].Url)

	loc, err := q.RequestLocation(f.ctx, &types.QueryRequestLocationRequest{Id: id})
	require.NoError(t, err)
	require.NotEmpty(t, loc.Locator)
	require.NotEmpty(t, loc.Sources[0].Url)
}
