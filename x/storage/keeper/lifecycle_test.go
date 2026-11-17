package keeper_test

import (
	"errors"
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/x/storage/keeper"
	"sparkdream/x/storage/types"
)

// A genesis round trip with a request in every status rebuilds the same
// indexes and timers, and the invariants hold.
func TestGenesisRoundTrip(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setParams(func(p *types.Params) { p.MaxOpenRequestsPerRequester = 20 })
	second := f.scanner("scanner2", w.council)
	_ = second

	pending := f.request(w.requester, "pending")
	claimed := f.request(w.requester, "claimed")
	f.claim(w.operator, claimed)
	settled := f.completed(w, "settled")
	f.runTo(f.get(settled).SettleAt + 1)
	completed := f.completed(w, "completed")
	cancelled := f.request(w.requester, "cancelled")
	_, err := f.srv.CancelRequest(f.ctx, &types.MsgCancelRequest{Requester: w.requester, RequestId: cancelled})
	require.NoError(t, err)
	flaggedHeld := f.completed(w, "flagged-held")
	f.flag(w.scanner, "flagged-held")
	flaggedPending := f.request(w.requester, "flagged-pending")
	f.flag(w.scanner, "flagged-pending")
	declined := f.request(w.requester, "declined")
	require.NoError(t, f.decline(w.operator, declined, mismatch))
	f.checkInvariants()

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, exported.Validate())
	require.Len(t, exported.Requests, 8)
	require.Len(t, exported.ApprovedOperators, 1)
	require.Len(t, exported.ApprovedScanners, 2)
	require.Len(t, exported.RemovedHashes, 2)
	require.NotEmpty(t, exported.Counters)

	g := initFixture(t)
	g.ctx = g.ctx.WithBlockHeight(f.ctx.BlockHeight())
	// The importing chain's bank holds the escrow.
	g.bank.balances[moduleAddr().String()] = f.bank.balance(moduleAddr().String())
	g.commons.groups[w.council] = true
	g.service.ops = f.service.ops
	g.rep.active = f.rep.active
	g.rep.trust = f.rep.trust
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))
	g.checkInvariants()

	again, err := g.keeper.ExportGenesis(g.ctx)
	require.NoError(t, err)
	require.Equal(t, exported, again)

	// The rebuilt timers fire: the completed request settles, the held fee burns.
	g.runTo(g.get(flaggedHeld).ForfeitAt + 1)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_SETTLED, g.get(completed).Status)
	require.True(t, g.get(flaggedHeld).CopyFee.IsZero())
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, g.get(pending).Status)
	_ = flaggedPending

	// A module account that holds less than the escrow is refused; one that
	// holds more is fine.
	h := initFixture(t)
	require.Error(t, h.keeper.InitGenesis(h.ctx, *exported))
	h = initFixture(t)
	h.bank.balances[moduleAddr().String()] = f.bank.balance(moduleAddr().String()).AddRaw(7)
	require.NoError(t, h.keeper.InitGenesis(h.ctx, *exported))
	h.checkInvariants()
}

func TestGenesisValidate(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.completed(w, "photo")
	f.flag(w.scanner, "photo")
	other := f.request(w.requester, "other")
	base := func() *types.GenesisState {
		g, err := f.keeper.ExportGenesis(f.ctx)
		require.NoError(t, err)
		return g
	}
	require.NoError(t, base().Validate())

	cases := map[string]func(g *types.GenesisState){
		"id above the sequence": func(g *types.GenesisState) { g.RequestSeq = 1 },
		"unknown backend":       func(g *types.GenesisState) { g.Requests[0].Backend = "nowhere" },
		"two live requests, one file": func(g *types.GenesisState) {
			g.Requests[1].Sha256 = hashOf("x")
			g.Requests = append(g.Requests, g.Requests[1])
			g.Requests[2].Id = 3
			g.RequestSeq = 3
		},
		"open request, flagged hash": func(g *types.GenesisState) {
			g.RemovedHashes = append(g.RemovedHashes, types.RemovedHash{Sha256: hashOf("other"), Flags: g.RemovedHashes[0].Flags})
		},
		"held id not flagged":       func(g *types.GenesisState) { g.RemovedHashes[0].Held = []uint64{other} },
		"held fee on no list":       func(g *types.GenesisState) { g.RemovedHashes[0].Held = nil },
		"flag record without flags": func(g *types.GenesisState) { g.RemovedHashes[0].Flags = nil },
		"closed request with a fee": func(g *types.GenesisState) { g.Requests[0].ForfeitAt = 0 },
		"approval for no backend":   func(g *types.GenesisState) { g.ApprovedOperators[0].Backend = "nowhere" },
		"too many scanners": func(g *types.GenesisState) {
			g.Params.MaxApprovedScanners = 1
			g.ApprovedScanners = append(g.ApprovedScanners, types.ApprovedScanner{Scanner: addr("x"), Controller: addr("c")})
		},
		"backend listed twice": func(g *types.GenesisState) { g.Backends = append(g.Backends, g.Backends[0]) },
		"bad params":           func(g *types.GenesisState) { g.Params.ClaimWindowBlocks = 0 },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			g := base()
			mut(g)
			require.Error(t, g.Validate())
		})
	}
	_ = id
}

// Under a backlog larger than max_deadlines_per_block the rest carry to
// later blocks, and nothing is lost.
func TestEndBlockerBacklog(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	f.setParams(func(p *types.Params) { p.MaxDeadlinesPerBlock = 2; p.MaxOpenRequestsPerRequester = 20 })
	var ids []uint64
	for _, c := range []string{"a", "b", "c", "d", "e"} {
		ids = append(ids, f.request(w.requester, c))
	}
	deadline := f.get(ids[0]).Deadline
	f.ctx = f.ctx.WithBlockHeight(deadline) // every expiry is due at once
	count := func() (n int) {
		for _, id := range ids {
			if f.get(id).Status == types.RequestStatus_REQUEST_STATUS_EXPIRED {
				n++
			}
		}
		return n
	}
	f.run(1)
	require.Equal(t, 2, count())
	// A message goes by height, not by whether the EndBlocker has reached
	// the entry: the unprocessed requests are past their deadline.
	require.ErrorIs(t, f.claimErr(w.operator, ids[4]), types.ErrDeadlinePassed)
	f.run(1)
	require.Equal(t, 4, count())
	f.run(1)
	require.Equal(t, 5, count())
	require.Equal(t, int64(0), f.escrow())
}

// A failing timer is retried, never dropped: a dropped settlement would
// leave the request COMPLETED for ever with its fee in escrow.
func TestFailedDeadlineIsRetried(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	p := f.params()
	id := f.completed(w, "photo")
	// The council cannot receive and the community pool refuses too.
	f.commons.groups[w.council] = false
	f.distr.err = errors.New("pool unavailable")

	settleAt := f.get(id).SettleAt
	f.runTo(settleAt + 1)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_COMPLETED, f.get(id).Status)
	require.Equal(t, copyFee(), f.escrow())
	has, err := f.keeper.Deadlines.Has(f.ctx, collections.Join3(settleAt+p.DeadlineRetryBlocks, id, types.DeadlineSettle))
	require.NoError(t, err)
	require.True(t, has)

	f.distr.err = nil
	f.runTo(settleAt + p.DeadlineRetryBlocks + 1)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_SETTLED, f.get(id).Status)
	require.Equal(t, int64(0), f.escrow())
}

// A timer left behind for a request that moved on is dropped as stale.
func TestStaleDeadlineIsDropped(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	h := f.ctx.BlockHeight()
	require.NoError(t, f.keeper.Deadlines.Set(f.ctx, collections.Join3(h, id, types.DeadlineSettle)))
	require.NoError(t, f.keeper.Deadlines.Set(f.ctx, collections.Join3(h, id, types.DeadlinePrune)))
	require.NoError(t, f.keeper.Deadlines.Set(f.ctx, collections.Join3(h, uint64(999), types.DeadlineExpiry)))
	f.run(1)
	require.Equal(t, types.RequestStatus_REQUEST_STATUS_PENDING, f.get(id).Status)
	n := 0
	require.NoError(t, f.keeper.Deadlines.Walk(f.ctx, nil, func(collections.Triple[int64, uint64, uint32]) (bool, error) {
		n++
		return false, nil
	}))
	require.Equal(t, 1, n) // only the request's own expiry timer
}

// The invariants catch a broken escrow and a dangling index.
func TestInvariantsDetectCorruption(t *testing.T) {
	f := initFixture(t)
	w := f.world()
	id := f.request(w.requester, "photo")
	f.checkInvariants()

	f.bank.balances[moduleAddr().String()] = f.bank.balance(moduleAddr().String()).SubRaw(1)
	_, broken := keeper.EscrowInvariant(f.keeper)(f.ctx)
	require.True(t, broken)
	f.bank.balances[moduleAddr().String()] = f.bank.balance(moduleAddr().String()).AddRaw(5) // a stray coin is fine
	_, broken = keeper.EscrowInvariant(f.keeper)(f.ctx)
	require.False(t, broken)

	require.NoError(t, f.keeper.LiveByHash.Remove(f.ctx, collections.Join(types.ArweaveBackend, hashOf("photo"))))
	_, broken = keeper.IndexInvariant(f.keeper)(f.ctx)
	require.True(t, broken)
	require.NoError(t, f.keeper.LiveByHash.Set(f.ctx, collections.Join(types.ArweaveBackend, hashOf("photo")), id))

	require.NoError(t, f.keeper.RemovedHashes.Set(f.ctx, hashOf("photo"), types.RemovedHash{Sha256: hashOf("photo"), Flags: []types.Flag{{Scanner: w.scanner}}}))
	_, broken = keeper.FlagInvariant(f.keeper)(f.ctx)
	require.True(t, broken)
}
