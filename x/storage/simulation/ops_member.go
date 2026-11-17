package simulation

import (
	"fmt"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/storage/keeper"
	"sparkdream/x/storage/types"
)

// SimulateMsgRequestPermanentCopy has an eligible member request a copy of a
// random file of a type the backend lists.
func SimulateMsgRequestPermanentCopy(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgRequestPermanentCopy{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		backends, err := e.backends()
		if err != nil {
			return noOpErr(label, err)
		}
		b, ok := pick(e.r, backends)
		if !ok || len(b.Types) == 0 {
			return noOp(label, "no enabled backend with a content type")
		}
		t := b.Types[e.r.Intn(len(b.Types))]
		size := uint64(1) + commontypes.Saturate[uint64](e.r.Int63n(commontypes.Saturate[int64](min(t.MaxSize, 1<<20))))
		total := b.CopyFee(size).Add(e.params.RequestFee)
		requester, ok := pick(e.r, e.live(func(a simtypes.Account) bool {
			return e.eligible(a.Address.String()) && e.spendable(a).GT(total)
		}))
		if !ok {
			return noOp(label, "no eligible member can pay for a request")
		}
		hash := randomHash(e.r)
		msg := &types.MsgRequestPermanentCopy{
			Requester:   requester.Address.String(),
			Backend:     b.Name,
			Sha256:      hash,
			SizeBytes:   size,
			ContentType: t.ContentType,
			Url:         "https://files.example/" + hash[:16],
			MaxFee:      total,
		}
		return e.submit(requester, msg, total)
	})
}

// SimulateMsgAddSource has another eligible member add a source to a request
// that is still waiting or claimed.
func SimulateMsgAddSource(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgAddSource{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		reqs, err := e.requests(20, types.RequestStatus_REQUEST_STATUS_PENDING, types.RequestStatus_REQUEST_STATUS_CLAIMED)
		if err != nil {
			return noOpErr(label, err)
		}
		r, ok := pick(e.r, reqs)
		if !ok {
			return noOp(label, "no request to add a source to")
		}
		member, ok := pick(e.r, e.live(func(a simtypes.Account) bool {
			return e.eligible(a.Address.String()) && e.spendable(a).GT(e.params.RequestFee)
		}))
		if !ok {
			return noOp(label, "no eligible member can pay the request fee")
		}
		msg := &types.MsgAddSource{
			Member:    member.Address.String(),
			RequestId: r.Id,
			Url:       fmt.Sprintf("https://mirror%d.example/%s", e.r.Intn(1000), r.Sha256[:16]),
			MaxFee:    e.params.RequestFee,
		}
		return e.submit(member, msg, e.params.RequestFee)
	})
}

// SimulateMsgCancelRequest has a requester withdraw a pending request.
func SimulateMsgCancelRequest(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgCancelRequest{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		reqs, err := e.requests(20, types.RequestStatus_REQUEST_STATUS_PENDING)
		if err != nil {
			return noOpErr(label, err)
		}
		for _, i := range e.r.Perm(len(reqs)) {
			acc, ok := e.account(reqs[i].Requester)
			if !ok {
				continue
			}
			return e.submitSmall(acc, &types.MsgCancelRequest{Requester: reqs[i].Requester, RequestId: reqs[i].Id})
		}
		return noOp(label, "no pending request of a live sim account")
	})
}

// SimulateMsgDisputeCompletion has the requester (for nothing) or another
// eligible member (for the request fee) hold a completion's settlement.
func SimulateMsgDisputeCompletion(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgDisputeCompletion{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		reqs, err := e.requests(20, types.RequestStatus_REQUEST_STATUS_COMPLETED)
		if err != nil {
			return noOpErr(label, err)
		}
		r, ok := pick(e.r, reqs)
		if !ok {
			return noOp(label, "no completion to dispute")
		}
		if acc, ok := e.account(r.Requester); ok && e.r.Intn(2) == 0 {
			return e.submitSmall(acc, &types.MsgDisputeCompletion{Member: r.Requester, RequestId: r.Id, MaxFee: math.ZeroInt()})
		}
		member, ok := pick(e.r, e.live(func(a simtypes.Account) bool {
			return a.Address.String() != r.Requester && e.eligible(a.Address.String()) && e.spendable(a).GT(e.params.RequestFee)
		}))
		if !ok {
			return noOp(label, "no other eligible member can pay the request fee")
		}
		msg := &types.MsgDisputeCompletion{Member: member.Address.String(), RequestId: r.Id, MaxFee: e.params.RequestFee}
		return e.submit(member, msg, e.params.RequestFee)
	})
}
