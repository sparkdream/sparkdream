package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/storage/keeper"
	"sparkdream/x/storage/types"
)

// SimulateMsgClaimRequest has an approved operator take a pending request.
func SimulateMsgClaimRequest(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgClaimRequest{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		reqs, err := e.requests(20, types.RequestStatus_REQUEST_STATUS_PENDING)
		if err != nil {
			return noOpErr(label, err)
		}
		reason := "no pending request"
		for _, i := range e.r.Perm(len(reqs)) {
			ops := e.operators(reqs[i].Backend)
			reason = "no approved operator among the sim accounts"
			for _, j := range e.r.Perm(len(ops)) {
				msg := &types.MsgClaimRequest{Operator: ops[j].Address.String(), RequestId: reqs[i].Id}
				if err := e.dryRun(msg); err != nil {
					reason = "handler would reject: " + err.Error()
					continue
				}
				return e.submitSmall(ops[j], msg)
			}
		}
		return noOp(label, reason)
	})
}

// claimed picks a CLAIMED request whose claimant is a live sim account.
func (e *simEnv) claimed() (types.Request, simtypes.Account, bool, error) {
	reqs, err := e.requests(20, types.RequestStatus_REQUEST_STATUS_CLAIMED)
	if err != nil {
		return types.Request{}, simtypes.Account{}, false, err
	}
	for _, i := range e.r.Perm(len(reqs)) {
		if acc, ok := e.account(reqs[i].Operator); ok {
			return reqs[i], acc, true, nil
		}
	}
	return types.Request{}, simtypes.Account{}, false, nil
}

// SimulateMsgExtendClaim has a claimant extend its claim.
func SimulateMsgExtendClaim(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgExtendClaim{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		r, acc, ok, err := e.claimed()
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no claim held by a live sim account")
		}
		return e.submitSmall(acc, &types.MsgExtendClaim{Operator: r.Operator, RequestId: r.Id})
	})
}

// SimulateMsgCompleteRequest has a claimant record a copy, with as many
// attestation hashes as the claim fixed for a scanned request.
func SimulateMsgCompleteRequest(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgCompleteRequest{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		r, acc, ok, err := e.claimed()
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no claim held by a live sim account")
		}
		msg := &types.MsgCompleteRequest{Operator: r.Operator, RequestId: r.Id, Locator: randomLocator(e.r)}
		if r.Scanned {
			for i := uint32(0); i < r.RequiredAttestations; i++ {
				msg.Attestations = append(msg.Attestations, randomHash(e.r))
			}
			msg.VerdictsLocator = randomLocator(e.r)
		}
		return e.submitSmall(acc, msg)
	})
}

// SimulateMsgDeclineRequest has an approved operator decline a pending
// request, or a claimant decline the one it holds.
func SimulateMsgDeclineRequest(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgDeclineRequest{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		reason := types.DeclineReason_DECLINE_REASON_MISMATCH
		if e.r.Intn(2) == 0 {
			reason = types.DeclineReason_DECLINE_REASON_UNREACHABLE
		}
		if e.r.Intn(3) == 0 {
			r, acc, ok, err := e.claimed()
			if err != nil {
				return noOpErr(label, err)
			}
			if ok {
				return e.submitSmall(acc, &types.MsgDeclineRequest{Operator: r.Operator, RequestId: r.Id, Reason: reason})
			}
		}
		reqs, err := e.requests(20, types.RequestStatus_REQUEST_STATUS_PENDING)
		if err != nil {
			return noOpErr(label, err)
		}
		why := "no pending request"
		for _, i := range e.r.Perm(len(reqs)) {
			ops := e.operators(reqs[i].Backend)
			why = "no approved operator among the sim accounts"
			for _, j := range e.r.Perm(len(ops)) {
				msg := &types.MsgDeclineRequest{Operator: ops[j].Address.String(), RequestId: reqs[i].Id, Reason: reason}
				if err := e.dryRun(msg); err != nil {
					why = "handler would reject: " + err.Error()
					continue
				}
				return e.submitSmall(ops[j], msg)
			}
		}
		return noOp(label, why)
	})
}

// SimulateMsgFlagRemoved has an approved scanner flag the hash of a stored
// request (in any status), or now and then a hash nobody requested.
func SimulateMsgFlagRemoved(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgFlagRemoved{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		scanner, ok := pick(e.r, e.scanners())
		if !ok {
			return noOp(label, "no approved scanner among the sim accounts")
		}
		hash := randomHash(e.r)
		if e.r.Intn(5) != 0 {
			reqs, err := e.requests(20,
				types.RequestStatus_REQUEST_STATUS_PENDING, types.RequestStatus_REQUEST_STATUS_CLAIMED,
				types.RequestStatus_REQUEST_STATUS_COMPLETED, types.RequestStatus_REQUEST_STATUS_SETTLED)
			if err != nil {
				return noOpErr(label, err)
			}
			if r, ok := pick(e.r, reqs); ok {
				hash = r.Sha256
			}
		}
		msg := &types.MsgFlagRemoved{Scanner: scanner.Address.String(), Sha256: hash, AttestationHash: randomHash(e.r)}
		return e.submitSmall(scanner, msg)
	})
}
