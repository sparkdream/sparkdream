package keeper_test

import (
	"fmt"
	"math/rand"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	servicetypes "sparkdream/x/service/types"
	"sparkdream/x/storage/types"
)

// TestRandomInterleavings drives every message of the module in a random
// order, from random signers, across many blocks, and checks the invariants
// after every step (spec section 13): the escrow covers every held fee, the
// indexes and timers match each request's status, and no open request has a
// flagged hash. A handler may refuse a message; it must never leave the
// state broken, whether it accepts or refuses.
//
// On top of the invariants it checks conservation: nothing is minted, so
// what the accounts, the escrow, the community pool and the burn total hold
// together never changes.
func TestRandomInterleavings(t *testing.T) {
	all := map[string]int{}
	for seed := int64(1); seed <= 12; seed++ {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			for name, n := range runInterleaving(t, seed, 700) {
				all[name] += n
			}
		})
	}
	// Taken together the runs must go through every action, or they prove
	// little about it.
	for _, name := range []string{"request", "add-source", "cancel", "claim", "extend", "complete", "decline",
		"dispute", "flag", "unflag", "void", "approve-operator", "remove-operator"} {
		require.NotZero(t, all[name], "no %s was accepted in any run", name)
	}
}

func runInterleaving(t *testing.T, seed int64, steps int) map[string]int {
	r := rand.New(rand.NewSource(seed))
	f := initFixture(t)
	// Short windows, so the run crosses every timer many times.
	f.setParams(func(p *types.Params) {
		p.RequestTtlBlocks = 30
		p.ClaimWindowBlocks = 5
		p.SettleWindowBlocks = 3
		p.DisputeWindowBlocks = 6
		p.FlagHoldBlocks = 7
		p.MinUnreachableBlocks = 1
		p.TerminalRetentionBlocks = 12
		p.CounterWindowBlocks = 25
		p.DeadlineRetryBlocks = 2
		p.MaxLapsesPerWindow = 3
		p.MaxBarredPerRequest = 3
		p.MaxDeadlinesPerBlock = 4 // a backlog now and then
	})
	f.setBackend(func(b *types.Backend) { b.DeclinesToClose = 2 })

	councils := []string{f.council("council-a"), f.council("council-b")}
	var operators, scanners, members []string
	for i := 0; i < 4; i++ {
		operators = append(operators, f.operator(fmt.Sprintf("operator-%d", i), councils[i%2]))
	}
	for i := 0; i < 3; i++ {
		scanners = append(scanners, f.scanner(fmt.Sprintf("scanner-%d", i), councils[i%2]))
	}
	for i := 0; i < 5; i++ {
		members = append(members, f.member(fmt.Sprintf("member-%d", i), reptypes.TrustLevel(1+i%3)))
	}
	// Operators are members too, so one of them sometimes requests.
	members = append(members, operators[0])
	files := make([]string, 14)
	for i := range files {
		files[i] = fmt.Sprintf("file-%d", i)
	}
	authorities := append([]string{f.gov}, councils...)

	total := func() math.Int {
		sum := f.bank.burned
		for _, v := range f.bank.balances {
			sum = sum.Add(v)
		}
		return sum
	}
	start := total()

	pick := func(list []string) string { return list[r.Intn(len(list))] }
	// anyID returns a request id that is usually real.
	anyID := func() uint64 {
		seq, err := f.keeper.RequestSeq.Peek(f.ctx)
		require.NoError(t, err)
		if seq == 0 {
			return 1
		}
		return 1 + uint64(r.Int63n(int64(seq)+1))
	}
	// idIn returns a request in one of the statuses when there is one (most
	// of the time), so the walk moves requests forward instead of mostly
	// hitting ones in the wrong state; otherwise any id.
	idIn := func(statuses ...types.RequestStatus) uint64 {
		if r.Intn(5) == 0 {
			return anyID()
		}
		var ids []uint64
		require.NoError(t, f.keeper.Requests.Walk(f.ctx, nil, func(id uint64, req types.Request) (bool, error) {
			for _, s := range statuses {
				if req.Status == s {
					ids = append(ids, id)
				}
			}
			return false, nil
		}))
		if len(ids) == 0 {
			return anyID()
		}
		return ids[r.Intn(len(ids))]
	}
	const (
		pending   = types.RequestStatus_REQUEST_STATUS_PENDING
		claimed   = types.RequestStatus_REQUEST_STATUS_CLAIMED
		completed = types.RequestStatus_REQUEST_STATUS_COMPLETED
		settled   = types.RequestStatus_REQUEST_STATUS_SETTLED
	)
	accepted := map[string]int{}
	try := func(name string, err error) {
		if err == nil {
			accepted[name]++
		}
	}

	for step := 0; step < steps; step++ {
		var id uint64
		op := r.Intn(22)
		switch op {
		case 3:
			id = idIn(pending, claimed)
		case 4, 5, 6, 7:
			id = idIn(pending)
		case 8, 9, 10, 11:
			id = idIn(claimed)
		case 12:
			id = idIn(pending, claimed)
		case 13:
			id = idIn(completed)
		case 17:
			id = idIn(completed, settled)
		default:
			id = anyID()
		}
		switch op {
		case 0, 1, 2:
			_, err := f.srv.RequestPermanentCopy(f.ctx, reqMsg(pick(members), pick(files)))
			try("request", err)
		case 3:
			_, err := f.srv.AddSource(f.ctx, &types.MsgAddSource{Member: pick(members), RequestId: id,
				Url: fmt.Sprintf("https://mirror%d.example/x", r.Intn(50)), MaxFee: math.NewInt(funds)})
			try("add-source", err)
		case 4:
			r0, err := f.keeper.Requests.Get(f.ctx, id)
			who := pick(members)
			if err == nil && r.Intn(3) != 0 {
				who = r0.Requester
			}
			_, err = f.srv.CancelRequest(f.ctx, &types.MsgCancelRequest{Requester: who, RequestId: id})
			try("cancel", err)
		case 5, 6, 7:
			try("claim", f.claimErr(pick(operators), id))
		case 8:
			_, err := f.srv.ExtendClaim(f.ctx, &types.MsgExtendClaim{Operator: pick(operators), RequestId: id})
			try("extend", err)
		case 9, 10, 11:
			claimant := pick(operators)
			if r0, err := f.keeper.Requests.Get(f.ctx, id); err == nil && r0.Operator != "" && r.Intn(4) != 0 {
				claimant = r0.Operator
			}
			_, err := f.srv.CompleteRequest(f.ctx, completeMsg(claimant, id))
			try("complete", err)
		case 12:
			reason := mismatch
			if r.Intn(2) == 0 {
				reason = unreachable
			}
			try("decline", f.decline(pick(operators), id, reason))
		case 13:
			who := pick(members)
			if r0, err := f.keeper.Requests.Get(f.ctx, id); err == nil && r.Intn(2) == 0 {
				who = r0.Requester
			}
			try("dispute", f.dispute(who, id))
		case 14, 15:
			try("flag", f.flagErr(pick(scanners), pick(files)))
		case 16:
			try("unflag", f.unflag(pick(authorities), pick(files)))
		case 17:
			authority := pick(authorities)
			if r0, err := f.keeper.Requests.Get(f.ctx, id); err == nil && r0.Controller != "" && r.Intn(2) == 0 {
				authority = r0.Controller
			}
			_, err := f.srv.VoidCompletion(f.ctx, &types.MsgVoidCompletion{Authority: authority, RequestId: id, Reason: "random"})
			try("void", err)
		case 18:
			// An operator or a scanner changes status in x/service, or a
			// member stops being active, and later comes back.
			switch r.Intn(4) {
			case 0:
				statuses := []servicetypes.OperatorStatus{
					servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE,
					servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE,
					servicetypes.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED,
					servicetypes.OperatorStatus_OPERATOR_STATUS_UNBONDING,
				}
				f.setStatus(pick(operators), types.ArweaveServiceType, statuses[r.Intn(len(statuses))])
			case 1:
				op := pick(operators)
				f.rep.active[op] = !f.rep.active[op]
			case 2:
				s := pick(scanners)
				st := servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE
				if r.Intn(3) == 0 {
					st = servicetypes.OperatorStatus_OPERATOR_STATUS_UNBONDING
				}
				f.setStatus(s, types.ContentScannerServiceType, st)
			case 3:
				// A council stops being one for a while: payouts redirect.
				c := pick(councils)
				f.commons.groups[c] = !f.commons.groups[c]
			}
		case 20, 21:
			// Things come back: the usual state is a working cast.
			who := pick(operators)
			f.setStatus(who, types.ArweaveServiceType, servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE)
			f.rep.active[who] = true
			f.setStatus(pick(scanners), types.ContentScannerServiceType, servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE)
		case 19:
			if r.Intn(4) == 0 {
				_, err := f.srv.RemoveOperator(f.ctx, &types.MsgRemoveOperator{Authority: f.gov, Backend: types.ArweaveBackend, Operator: pick(operators)})
				try("remove-operator", err)
			} else {
				_, err := f.srv.ApproveOperator(f.ctx, &types.MsgApproveOperator{Authority: f.gov, Backend: types.ArweaveBackend, Operator: pick(operators)})
				try("approve-operator", err)
			}
		}
		f.checkInvariants()
		require.True(t, start.Equal(total()), "step %d: coins were created or lost", step)

		// Blocks pass at an uneven pace.
		if r.Intn(3) == 0 {
			f.run(1 + int64(r.Intn(3)))
			require.True(t, start.Equal(total()), "step %d: the end blocker created or lost coins", step)
		}
	}

	// Let every timer run out: nothing may be left moving.
	for c := range f.commons.groups {
		f.commons.groups[c] = true
	}
	f.run(120)
	require.True(t, start.Equal(total()))
	require.NoError(t, f.keeper.Requests.Walk(f.ctx, nil, func(id uint64, r types.Request) (bool, error) {
		require.NotEqual(t, types.RequestStatus_REQUEST_STATUS_PENDING, r.Status, "request %d never expired", id)
		require.NotEqual(t, types.RequestStatus_REQUEST_STATUS_CLAIMED, r.Status, "request %d never lapsed", id)
		require.NotEqual(t, types.RequestStatus_REQUEST_STATUS_COMPLETED, r.Status, "request %d never settled", id)
		require.Zero(t, r.ForfeitAt, "request %d still holds a flagged fee", id)
		require.True(t, r.CopyFee.IsZero(), "request %d still holds %s", id, r.CopyFee)
		return false, nil
	}))
	require.Equal(t, int64(0), f.escrow(), "the escrow must be empty once nothing is open")

	// The run must have gone through the whole lifecycle, or it proved little.
	// Every run must at least take requests through to a copy.
	for _, name := range []string{"request", "claim", "complete", "flag"} {
		require.NotZero(t, accepted[name], "seed %d: no %s was accepted", seed, name)
	}
	return accepted
}
