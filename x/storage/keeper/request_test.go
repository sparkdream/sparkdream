package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/storage/types"
)

// The whole ordinary life of a request: fee burned and escrowed, claim,
// completion, settlement paying the council that approved the operator.
func TestRequestToSettlement(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	p := f.params()

	id := f.request(w.requester, "photo")
	require.Equal(t, uint64(1), id)
	r := f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, r.Status)
	require.Equal(t, copyFee(), r.CopyFee.Int64())
	require.True(t, r.Scanned)
	require.Equal(t, uint32(reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED), r.RequesterTrustLevel)
	require.Equal(t, f.ctx.BlockHeight()+p.RequestTtlBlocks, r.Deadline)
	require.Equal(t, int64(funds)-copyFee()-reqFee(), f.bal(w.requester))
	require.Equal(t, reqFee(), f.bank.burned.Int64())
	require.Equal(t, copyFee(), f.escrow())

	f.claim(w.operator, id)
	r = f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_CLAIMED, r.Status)
	require.Equal(t, w.council, r.Controller)
	require.Equal(t, uint32(1), r.RequiredAttestations)

	f.complete(w.operator, id)
	r = f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_COMPLETED, r.Status)
	require.Equal(t, f.ctx.BlockHeight()+p.SettleWindowBlocks, r.SettleAt)

	f.runTo(r.SettleAt) // the EndBlocker of settle_at - 1 has run
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_COMPLETED, f.get(id).Status)
	f.run(1)
	r = f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_SETTLED, r.Status)
	require.Equal(t, w.council, r.PaidTo)
	require.Equal(t, copyFee(), r.PaidAmount.Int64())
	require.True(t, r.CopyFee.IsZero())
	require.Equal(t, int64(funds)+copyFee(), f.bal(w.council))
	require.Equal(t, int64(0), f.escrow())
	// The operator gains nothing from a completion.
	require.Equal(t, int64(funds), f.bal(w.operator))
}

func TestRequestValidation(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f *fixture, w world)
		mut   func(m *types.MsgRequestPermanentCopy)
		err   error
	}{
		{"module disabled", func(f *fixture, _ world) { f.setParams(func(p *types.Params) { p.Enabled = false }) }, nil, types.ErrModuleDisabled},
		{"backend disabled", func(f *fixture, _ world) { f.setBackend(func(b *types.Backend) { b.Enabled = false }) }, nil, types.ErrBackendDisabled},
		{"unknown backend", nil, func(m *types.MsgRequestPermanentCopy) { m.Backend = "nowhere" }, types.ErrBackendNotFound},
		{"not a member", nil, func(m *types.MsgRequestPermanentCopy) { m.Requester = addr("stranger") }, types.ErrRequesterNotPermitted},
		{"trust too low", func(f *fixture, _ world) { f.member("newbie", reptypes.TrustLevel_TRUST_LEVEL_NEW) },
			func(m *types.MsgRequestPermanentCopy) { m.Requester = addr("newbie") }, types.ErrRequesterNotPermitted},
		{"bad hash", nil, func(m *types.MsgRequestPermanentCopy) { m.Sha256 = "ABC" }, types.ErrInvalidHash},
		{"uppercase hash", nil, func(m *types.MsgRequestPermanentCopy) { m.Sha256 = "AB" + m.Sha256[2:] }, types.ErrInvalidHash},
		{"flagged hash", func(f *fixture, w world) { f.flag(w.scanner, "photo") }, nil, types.ErrHashRemoved},
		{"type not listed", nil, func(m *types.MsgRequestPermanentCopy) { m.ContentType = "image/gif" }, types.ErrContentTypeNotAllowed},
		{"zero size", nil, func(m *types.MsgRequestPermanentCopy) { m.SizeBytes = 0 }, types.ErrInvalidSize},
		{"too large", nil, func(m *types.MsgRequestPermanentCopy) { m.SizeBytes = 10<<20 + 1 }, types.ErrFileTooLarge},
		{"http address", nil, func(m *types.MsgRequestPermanentCopy) { m.Url = "http://files.example/a" }, types.ErrInvalidURL},
		{"address with a quote", nil, func(m *types.MsgRequestPermanentCopy) { m.Url = "https://files.example/a\"b" }, types.ErrInvalidURL},
		{"address with an angle bracket", nil, func(m *types.MsgRequestPermanentCopy) { m.Url = "https://files.example/<a>" }, types.ErrInvalidURL},
		{"address with a user part", nil, func(m *types.MsgRequestPermanentCopy) { m.Url = "https://me:pw@files.example/a" }, types.ErrInvalidURL},
		{"address with a fragment", nil, func(m *types.MsgRequestPermanentCopy) { m.Url = "https://files.example/a#b" }, types.ErrInvalidURL},
		{"max fee too low", nil, func(m *types.MsgRequestPermanentCopy) { m.MaxFee = math.NewInt(copyFee()) }, types.ErrFeeExceedsMax},
		{"scanner quorum 0", func(f *fixture, _ world) {
			c := f.service.configs[types.ContentScannerServiceType]
			c.AttestationQuorum = 0
			f.service.configs[types.ContentScannerServiceType] = c
		}, nil, types.ErrNoScannerQuorum},
		{"fewer scanners than the quorum", func(f *fixture, _ world) {
			c := f.service.configs[types.ContentScannerServiceType]
			c.AttestationQuorum = 2
			f.service.configs[types.ContentScannerServiceType] = c
		}, nil, types.ErrNoScannerQuorum},
		{"no operator that could claim", func(f *fixture, w world) {
			f.rep.active[w.operator] = false
		}, nil, types.ErrNoActiveOperator},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			w := f.world()
			if tc.setup != nil {
				tc.setup(f, w)
			}
			m := reqMsg(w.requester, "photo")
			if tc.mut != nil {
				tc.mut(m)
			}
			burned := f.bank.burned
			_, err := f.srv.RequestPermanentCopy(f.ctx, m)
			require.ErrorIs(t, err, tc.err)
			// A refused request burns nothing.
			require.True(t, burned.Equal(f.bank.burned))
		})
	}
}

// An operator that is registered in x/service but not approved here does not
// count as able to take the request.
func TestRequestNeedsAnApprovedOperator(t *testing.T) {
	f := initFixture(t)
	c := f.council("council")
	f.scanner("scanner", c)
	requester := f.member("requester", reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	op := f.member("operator", reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	f.register(op, types.ArweaveServiceType, c)

	_, err := f.srv.RequestPermanentCopy(f.ctx, reqMsg(requester, "photo"))
	require.ErrorIs(t, err, types.ErrNoActiveOperator)
}

func TestOneRequestPerFile(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	other := f.member("other", reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED)

	id := f.request(w.requester, "photo")
	_, err := f.srv.RequestPermanentCopy(f.ctx, reqMsg(other, "photo"))
	require.ErrorIs(t, err, types.ErrAlreadyRequested)

	f.claim(w.operator, id)
	f.complete(w.operator, id)
	f.runTo(f.get(id).SettleAt + 1)
	_, err = f.srv.RequestPermanentCopy(f.ctx, reqMsg(other, "photo"))
	require.ErrorIs(t, err, types.ErrAlreadyStored)

	// A different file is unaffected.
	f.request(other, "another")
}

func TestOpenRequestCap(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setParams(func(p *types.Params) { p.MaxOpenRequestsPerRequester = 2 })
	f.request(w.requester, "a")
	id := f.request(w.requester, "b")
	_, err := f.srv.RequestPermanentCopy(f.ctx, reqMsg(w.requester, "c"))
	require.ErrorIs(t, err, types.ErrTooManyOpenRequests)

	// A closed request frees its slot.
	_, err = f.srv.CancelRequest(f.ctx, &types.MsgCancelRequest{Requester: w.requester, RequestId: id})
	require.NoError(t, err)
	f.request(w.requester, "c")
}

// Request then cancel: the copy fee comes back, the request fee does not.
func TestCancel(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")

	_, err := f.srv.CancelRequest(f.ctx, &types.MsgCancelRequest{Requester: addr("someone"), RequestId: id})
	require.ErrorIs(t, err, types.ErrUnauthorized)

	_, err = f.srv.CancelRequest(f.ctx, &types.MsgCancelRequest{Requester: w.requester, RequestId: id})
	require.NoError(t, err)
	r := f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_CANCELLED, r.Status)
	require.Equal(t, types.ClosedReason_CLOSED_REASON_CANCELLED, r.ClosedReason)
	require.Equal(t, f.ctx.BlockHeight(), r.ClosedAt)
	require.Equal(t, int64(funds)-reqFee(), f.bal(w.requester))
	require.Equal(t, int64(0), f.escrow())
	f.checkInvariants()

	// The hash is free again.
	f.request(w.requester, "photo")
}

// Refused while claimed or completed: an operator may be paying for the file.
func TestCancelRefusedOnceClaimed(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	f.claim(w.operator, id)
	_, err := f.srv.CancelRequest(f.ctx, &types.MsgCancelRequest{Requester: w.requester, RequestId: id})
	require.ErrorIs(t, err, types.ErrWrongStatus)
	f.complete(w.operator, id)
	_, err = f.srv.CancelRequest(f.ctx, &types.MsgCancelRequest{Requester: w.requester, RequestId: id})
	require.ErrorIs(t, err, types.ErrWrongStatus)
}

// A migrated requester cancels from the new address and is refunded where
// the bank sends it; the tombstoned address can do nothing.
func TestCancelAfterMigration(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	moved := f.member("requester-new", reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	f.rep.canonicalOf[w.requester] = moved

	_, err := f.srv.CancelRequest(f.ctx, &types.MsgCancelRequest{Requester: w.requester, RequestId: id})
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, err = f.srv.CancelRequest(f.ctx, &types.MsgCancelRequest{Requester: moved, RequestId: id})
	require.NoError(t, err)
}

func TestExpiry(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	r := f.get(id)

	f.runTo(r.Deadline)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, f.get(id).Status)
	// At the deadline height a claim is still allowed; the block's EndBlocker expires it.
	f.run(1)
	r = f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_EXPIRED, r.Status)
	require.Equal(t, int64(funds)-reqFee(), f.bal(w.requester))

	_, err := f.srv.ClaimRequest(f.ctx, &types.MsgClaimRequest{Operator: w.operator, RequestId: id})
	require.ErrorIs(t, err, types.ErrWrongStatus)

	// Pruned after the retention.
	f.runTo(r.ClosedAt + f.params().TerminalRetentionBlocks + 1)
	require.False(t, f.exists(id))
}

// A closed request is still pruned when terminal_retention_blocks is raised
// while it waits: the timer written at the old time moves to the new one.
func TestPruneAfterRetentionRaised(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	f.runTo(f.get(id).Deadline + 1)
	r := f.get(id)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_EXPIRED, r.Status)

	old := f.params().TerminalRetentionBlocks
	f.setParams(func(p *types.Params) { p.TerminalRetentionBlocks = 2 * old })
	// Past the old time: kept, and the invariants (checked every block)
	// still find its timer.
	f.runTo(r.ClosedAt + old + 2)
	require.True(t, f.exists(id))
	f.runTo(r.ClosedAt + 2*old + 1)
	require.False(t, f.exists(id))
}

// A refund that cannot be delivered goes to the community pool; the block
// continues.
func TestRefundRedirected(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	f.bank.blocked[w.requester] = true
	f.runTo(f.get(id).Deadline + 1)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_EXPIRED, f.get(id).Status)
	require.Equal(t, copyFee(), f.distr.pool.Int64())
	require.Equal(t, int64(0), f.escrow())
}

func TestAddSource(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	helper := f.member("helper", reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	id := f.request(w.requester, "photo")
	add := func(member, url string) error {
		_, err := f.srv.AddSource(f.ctx, &types.MsgAddSource{Member: member, RequestId: id, Url: url, MaxFee: math.NewInt(funds)})
		return err
	}

	require.ErrorIs(t, add(addr("stranger"), "https://mirror.example/photo"), types.ErrRequesterNotPermitted)
	require.ErrorIs(t, add(helper, "https://files.example/photo"), types.ErrSourceExists)       // already listed
	require.ErrorIs(t, add(w.requester, "https://mirror.example/photo"), types.ErrSourceExists) // one per member
	require.ErrorIs(t, add(helper, "ftp://mirror.example/photo"), types.ErrInvalidURL)

	burned := f.bank.burned
	require.NoError(t, add(helper, "https://mirror.example/photo"))
	require.Equal(t, reqFee(), f.bank.burned.Sub(burned).Int64())
	require.Len(t, f.get(id).Sources, 2)
	require.ErrorIs(t, add(helper, "https://third.example/photo"), types.ErrSourceExists)

	// No fee is burned on a request nobody can take.
	another := f.member("another", reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	f.rep.active[w.operator] = false
	burned = f.bank.burned
	require.ErrorIs(t, add(another, "https://third.example/photo"), types.ErrNoActiveOperator)
	require.True(t, burned.Equal(f.bank.burned))
	f.rep.active[w.operator] = true

	// The slots are bounded.
	f.setParams(func(p *types.Params) { p.MaxSourcesPerRequest = 2 })
	require.ErrorIs(t, add(another, "https://third.example/photo"), types.ErrTooManySources)

	// Added sources end with the request: a settled one keeps the requester's.
	f.claim(w.operator, id)
	f.complete(w.operator, id)
	f.runTo(f.get(id).SettleAt + 1)
	r := f.get(id)
	require.Len(t, r.Sources, 1)
	require.Equal(t, w.requester, r.Sources[0].AddedBy)
	require.ErrorIs(t, add(another, "https://third.example/photo"), types.ErrWrongStatus)
}

func TestMaxFeeProtectsAgainstAPriceChange(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	m := reqMsg(w.requester, "photo")
	m.MaxFee = math.NewInt(copyFee() + reqFee())
	f.setBackend(func(b *types.Backend) { b.MinFee = b.MinFee.AddRaw(1) })
	_, err := f.srv.RequestPermanentCopy(f.ctx, m)
	require.ErrorIs(t, err, types.ErrFeeExceedsMax)
}

// A price change never touches an open request.
func TestFeeFixedAtCreation(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	f.setBackend(func(b *types.Backend) { b.MinFee = b.MinFee.MulRaw(5) })
	require.Equal(t, copyFee(), f.get(id).CopyFee.Int64())
}
