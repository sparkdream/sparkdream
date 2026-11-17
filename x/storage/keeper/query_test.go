package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	reptypes "sparkdream/x/rep/types"
	shieldtypes "sparkdream/x/shield/types"
	"sparkdream/x/storage/keeper"
	"sparkdream/x/storage/types"
)

func TestQueries(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	q := keeperQuery(f)
	f.setParams(func(p *types.Params) { p.MaxOpenRequestsPerRequester = 20 })

	pending := f.request(w.requester, "pending")
	claimed := f.request(w.requester, "claimed")
	f.claim(w.operator, claimed)
	done := f.completed(w, "done")

	b, err := q.Backend(f.ctx, &types.QueryBackendRequest{Name: types.ArweaveBackend})
	require.NoError(t, err)
	require.Equal(t, types.ArweaveServiceType, b.Backend.ServiceType)
	_, err = q.Backend(f.ctx, &types.QueryBackendRequest{Name: "nowhere"})
	require.Equal(t, codes.NotFound, status.Code(err))
	bs, err := q.Backends(f.ctx, &types.QueryBackendsRequest{})
	require.NoError(t, err)
	require.Len(t, bs.Backends, 1)

	// An approval whose controller x/service no longer names is marked stale.
	ops, err := q.ApprovedOperators(f.ctx, &types.QueryApprovedOperatorsRequest{Backend: types.ArweaveBackend})
	require.NoError(t, err)
	require.Len(t, ops.Operators, 1)
	require.False(t, ops.Operators[0].Stale)
	scs, err := q.ApprovedScanners(f.ctx, &types.QueryApprovedScannersRequest{})
	require.NoError(t, err)
	require.Len(t, scs.Scanners, 1)
	require.False(t, scs.Scanners[0].Stale)
	other := f.council("other-council")
	f.register(w.operator, types.ArweaveServiceType, other)
	f.register(w.scanner, types.ContentScannerServiceType, other)
	ops, err = q.ApprovedOperators(f.ctx, &types.QueryApprovedOperatorsRequest{Backend: types.ArweaveBackend})
	require.NoError(t, err)
	require.True(t, ops.Operators[0].Stale)
	scs, err = q.ApprovedScanners(f.ctx, &types.QueryApprovedScannersRequest{})
	require.NoError(t, err)
	require.True(t, scs.Scanners[0].Stale)
	st, err := q.OperatorStanding(f.ctx, &types.QueryOperatorStandingRequest{Backend: types.ArweaveBackend, Operator: w.operator})
	require.NoError(t, err)
	require.False(t, st.Approved)
	require.False(t, st.CanClaim)
	require.NotEmpty(t, st.Reason)
	f.register(w.operator, types.ArweaveServiceType, w.council)
	f.register(w.scanner, types.ContentScannerServiceType, w.council)

	st, err = q.OperatorStanding(f.ctx, &types.QueryOperatorStandingRequest{Backend: types.ArweaveBackend, Operator: w.operator})
	require.NoError(t, err)
	require.True(t, st.Approved)
	require.True(t, st.CanClaim)
	require.Equal(t, uint32(1), st.OpenClaims)
	require.Equal(t, uint64(1), st.Completions.Count)
	require.Zero(t, st.Lapses.Count)

	r, err := q.Request(f.ctx, &types.QueryRequestRequest{Id: done})
	require.NoError(t, err)
	require.False(t, r.Withheld)
	require.NotEmpty(t, r.Request.Locator)
	_, err = q.Request(f.ctx, &types.QueryRequestRequest{Id: 999})
	require.Equal(t, codes.NotFound, status.Code(err))

	bh, err := q.RequestByHash(f.ctx, &types.QueryRequestByHashRequest{Backend: types.ArweaveBackend, Sha256: hashOf("claimed")})
	require.NoError(t, err)
	require.Equal(t, claimed, bh.Request.Id)
	_, err = q.RequestByHash(f.ctx, &types.QueryRequestByHashRequest{Backend: types.ArweaveBackend, Sha256: hashOf("unknown")})
	require.Equal(t, codes.NotFound, status.Code(err))

	for status, want := range map[types.RequestStatus]uint64{
		types.RequestStatus_REQUEST_STATUS_PENDING:   pending,
		types.RequestStatus_REQUEST_STATUS_CLAIMED:   claimed,
		types.RequestStatus_REQUEST_STATUS_COMPLETED: done,
	} {
		res, err := q.RequestsByStatus(f.ctx, &types.QueryRequestsByStatusRequest{Backend: types.ArweaveBackend, Status: status})
		require.NoError(t, err)
		require.Len(t, res.Requests, 1, status.String())
		require.Equal(t, want, res.Requests[0].Id)
	}
	none, err := q.RequestsByStatus(f.ctx, &types.QueryRequestsByStatusRequest{Backend: types.ArweaveBackend, Status: types.RequestStatus_REQUEST_STATUS_SETTLED})
	require.NoError(t, err)
	require.Empty(t, none.Requests)

	byReq, err := q.RequestsByRequester(f.ctx, &types.QueryRequestsByRequesterRequest{Requester: w.requester, Pagination: &query.PageRequest{Limit: 2}})
	require.NoError(t, err)
	require.Len(t, byReq.Requests, 2)
	require.NotEmpty(t, byReq.Pagination.NextKey)

	byOp, err := q.RequestsByOperator(f.ctx, &types.QueryRequestsByOperatorRequest{Operator: w.operator})
	require.NoError(t, err)
	require.Len(t, byOp.Requests, 2) // the claim and the unsettled completion
	require.Empty(t, byOp.WithheldIds)

	quote, err := q.QuoteFee(f.ctx, &types.QueryQuoteFeeRequest{Backend: types.ArweaveBackend, ContentType: "image/png", SizeBytes: 1 << 20})
	require.NoError(t, err)
	require.Equal(t, int64(10*(1<<20)), quote.CopyFee.Int64())
	require.Equal(t, quote.CopyFee.Add(quote.RequestFee), quote.Total)
	_, err = q.QuoteFee(f.ctx, &types.QueryQuoteFeeRequest{Backend: types.ArweaveBackend, ContentType: "image/gif", SizeBytes: 10})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = q.QuoteFee(f.ctx, &types.QueryQuoteFeeRequest{Backend: types.ArweaveBackend, ContentType: "image/png", SizeBytes: 10<<20 + 1})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = q.RemovedHash(f.ctx, &types.QueryRemovedHashRequest{Sha256: hashOf("done")})
	require.Equal(t, codes.NotFound, status.Code(err))
	f.flag(w.scanner, "pending")
	rh, err := q.RemovedHash(f.ctx, &types.QueryRemovedHashRequest{Sha256: hashOf("pending")})
	require.NoError(t, err)
	require.Len(t, rh.RemovedHash.Flags, 1)
	require.Equal(t, w.scanner, rh.RemovedHash.Flags[0].Scanner)

	// A list query withholds too.
	rej, err := q.RequestsByStatus(f.ctx, &types.QueryRequestsByStatusRequest{Backend: types.ArweaveBackend, Status: types.RequestStatus_REQUEST_STATUS_REJECTED})
	require.NoError(t, err)
	require.Len(t, rej.Requests, 1)
	require.Equal(t, []uint64{pending}, rej.WithheldIds)
	require.Empty(t, rej.Requests[0].Sources[0].Url)
	// The stored record is untouched.
	require.NotEmpty(t, f.get(pending).Sources[0].Url)

	for _, call := range []func() error{
		func() error { _, err := q.Backend(f.ctx, nil); return err },
		func() error { _, err := q.Request(f.ctx, nil); return err },
		func() error { _, err := q.QuoteFee(f.ctx, nil); return err },
		func() error { _, err := q.RequestLocation(f.ctx, nil); return err },
	} {
		require.Equal(t, codes.InvalidArgument, status.Code(call()))
	}
}

// RequestsByRequester reads every address a migrated member has used.
func TestRequestsByRequesterAfterMigration(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	q := keeperQuery(f)
	a := f.request(w.requester, "a")
	moved := f.member("requester-new", reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	f.rep.canonicalOf[w.requester] = moved
	b := f.request(moved, "b")

	res, err := q.RequestsByRequester(f.ctx, &types.QueryRequestsByRequesterRequest{Requester: moved})
	require.NoError(t, err)
	var ids []uint64
	for _, r := range res.Requests {
		ids = append(ids, r.Id)
	}
	require.ElementsMatch(t, []uint64{a, b}, ids)
	require.Equal(t, uint64(2), res.Pagination.Total)

	// The open-request cap counts the member, not the address.
	f.setParams(func(p *types.Params) { p.MaxOpenRequestsPerRequester = 2 })
	_, err = f.srv.RequestPermanentCopy(f.ctx, reqMsg(moved, "c"))
	require.ErrorIs(t, err, types.ErrTooManyOpenRequests)
}

// No x/storage message is shield-compatible: a request ties an address to a
// file hash in public and for good, so it must never be routed anonymously.
// The module's msg server and keeper do not implement ShieldAware, which is
// what x/shield requires of a target module.
func TestNoMessageIsShieldCompatible(t *testing.T) {
	f := initFixture(t)
	_, aware := any(f.srv).(shieldtypes.ShieldAware)
	require.False(t, aware, "the x/storage msg server must not implement ShieldAware")
	_, aware = any(f.keeper).(shieldtypes.ShieldAware)
	require.False(t, aware, "the x/storage keeper must not implement ShieldAware")
	_, aware = any(&f.keeper).(shieldtypes.ShieldAware)
	require.False(t, aware)
	_ = keeper.Keeper{}
}

// A message cannot run before app.go has wired the keepers it reads.
func TestUnwiredKeeperRefusesMessages(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.keeper.SetServiceKeeper(nil)
	_, err := f.srv.RequestPermanentCopy(f.ctx, reqMsg(w.requester, "photo"))
	require.ErrorIs(t, err, types.ErrNotWired)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
}
