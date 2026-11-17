package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	servicetypes "sparkdream/x/service/types"
	"sparkdream/x/storage/keeper"
	"sparkdream/x/storage/types"
)

// serviceOperators lists the live x/service operators of a service type
// whose controller is policy, read through the x/service query.
func (e *simEnv) serviceOperators(serviceType, policy string) []string {
	res, err := e.serviceQuery.OperatorsByController(e.ctx, &servicetypes.QueryOperatorsByControllerRequest{Controller: policy})
	if err != nil {
		return nil
	}
	var out []string
	for _, op := range res.Operators {
		if op.ServiceType == serviceType {
			out = append(out, op.Address)
		}
	}
	return out
}

// enlist registers a live sim account as an x/service operator of serviceType
// under policy, with a real MsgRegisterOperator, and returns its address. The
// x/service simulation registers and retires operators of every type on its
// own schedule; this keeps a worker for x/storage to approve when it has left
// none. member limits the choice to active x/rep members (storage operators
// must be members; scanners need not be).
func (e *simEnv) enlist(serviceType, policy string, member bool) (string, bool, error) {
	res, err := e.serviceQuery.ServiceType(e.ctx, &servicetypes.QueryServiceTypeRequest{ServiceType: serviceType})
	if err != nil || !res.Config.Enabled {
		return "", false, nil
	}
	bond := res.Config.MinBondAmount
	for _, i := range e.r.Perm(len(e.accs)) {
		acc := e.accs[i]
		addr := acc.Address.String()
		if e.usable(acc, bond) != "" || (member && !e.activeMember(addr)) {
			continue
		}
		msg := &servicetypes.MsgRegisterOperator{
			Creator: addr, ServiceType: serviceType, Controller: policy, BondAmount: bond,
			Metadata: []byte(`{"v":1}`),
		}
		if e.dryRun(msg) != nil {
			continue
		}
		opMsg, _, err := e.deliver(acc, msg, bond)
		if err != nil {
			return "", false, err
		}
		if opMsg.OK {
			return addr, true, nil
		}
	}
	return "", false, nil
}

// SimulateMsgApproveOperator has a controller approve one of its live
// storage operators that is not approved yet.
func SimulateMsgApproveOperator(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgApproveOperator{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		backends, err := e.backends()
		if err != nil {
			return noOpErr(label, err)
		}
		return e.advance(label, councilBodies, decodeAs[types.MsgApproveOperator], func(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			for _, i := range e.r.Perm(len(backends)) {
				b := backends[i]
				approved := map[string]bool{}
				if res, err := e.query.ApprovedOperators(e.ctx, &types.QueryApprovedOperatorsRequest{Backend: b.Name}); err == nil {
					for _, o := range res.Operators {
						approved[o.Operator] = !o.Stale
					}
				}
				for _, m := range taken {
					if t, ok := m.(*types.MsgApproveOperator); ok && t.Backend == b.Name {
						approved[t.Operator] = true
					}
				}
				for _, op := range e.serviceOperators(b.ServiceType, policy) {
					if !approved[op] {
						return &types.MsgApproveOperator{Authority: policy, Backend: b.Name, Operator: op}, "", nil
					}
				}
			}
			for _, i := range e.r.Perm(len(backends)) {
				op, ok, err := e.enlist(backends[i].ServiceType, policy, true)
				if err != nil {
					return nil, "", err
				}
				if ok {
					return &types.MsgApproveOperator{Authority: policy, Backend: backends[i].Name, Operator: op}, "", nil
				}
			}
			return nil, "the body controls no unapproved storage operator and none could be registered", nil
		})
	})
}

// SimulateMsgApproveScanner has a controller approve one of its live content
// scanners that is not approved yet.
func SimulateMsgApproveScanner(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgApproveScanner{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return e.advance(label, councilBodies, decodeAs[types.MsgApproveScanner], func(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			approved := map[string]bool{}
			if res, err := e.query.ApprovedScanners(e.ctx, &types.QueryApprovedScannersRequest{}); err == nil {
				for _, s := range res.Scanners {
					approved[s.Scanner] = !s.Stale
				}
			}
			for _, m := range taken {
				if t, ok := m.(*types.MsgApproveScanner); ok {
					approved[t.Scanner] = true
				}
			}
			for _, s := range e.serviceOperators(types.ContentScannerServiceType, policy) {
				if !approved[s] {
					return &types.MsgApproveScanner{Authority: policy, Scanner: s}, "", nil
				}
			}
			scanner, ok, err := e.enlist(types.ContentScannerServiceType, policy, false)
			if err != nil {
				return nil, "", err
			}
			if ok {
				return &types.MsgApproveScanner{Authority: policy, Scanner: scanner}, "", nil
			}
			return nil, "the body controls no unapproved scanner and none could be registered", nil
		})
	})
}

// SimulateMsgVoidCompletion has a controller void a completion it would be
// paid for, or a settled copy it was paid for (returning the fee).
func SimulateMsgVoidCompletion(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgVoidCompletion{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		reqs, err := e.requests(30, types.RequestStatus_REQUEST_STATUS_COMPLETED, types.RequestStatus_REQUEST_STATUS_SETTLED)
		if err != nil {
			return noOpErr(label, err)
		}
		return e.advance(label, councilBodies, decodeAs[types.MsgVoidCompletion], func(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			busy := map[uint64]bool{}
			for _, m := range taken {
				if t, ok := m.(*types.MsgVoidCompletion); ok {
					busy[t.RequestId] = true
				}
			}
			for _, i := range e.r.Perm(len(reqs)) {
				r := reqs[i]
				mine := (r.Status == types.RequestStatus_REQUEST_STATUS_COMPLETED && r.Controller == policy) ||
					(r.Status == types.RequestStatus_REQUEST_STATUS_SETTLED && r.PaidTo == policy)
				if mine && !busy[r.Id] {
					return &types.MsgVoidCompletion{Authority: policy, RequestId: r.Id, Reason: "simulation: copy not found"}, "", nil
				}
			}
			return nil, "the body has no completion to void", nil
		})
	})
}

// SimulateMsgUnflagRemoved has a controller lift a flag one of its scanners
// placed.
func SimulateMsgUnflagRemoved(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgUnflagRemoved{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		var flagged []types.RemovedHash
		if err := e.k.RemovedHashes.Walk(e.ctx, nil, func(_ string, rec types.RemovedHash) (bool, error) {
			flagged = append(flagged, rec)
			return len(flagged) >= 50, nil
		}); err != nil {
			return noOpErr(label, err)
		}
		return e.advance(label, councilBodies, decodeAs[types.MsgUnflagRemoved], func(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			busy := map[string]bool{}
			for _, m := range taken {
				if t, ok := m.(*types.MsgUnflagRemoved); ok {
					for _, h := range t.Hashes {
						busy[h] = true
					}
				}
			}
			mine := map[string]bool{}
			for _, s := range e.serviceOperators(types.ContentScannerServiceType, policy) {
				mine[s] = true
			}
			for _, i := range e.r.Perm(len(flagged)) {
				rec := flagged[i]
				if busy[rec.Sha256] {
					continue
				}
				for _, f := range rec.Flags {
					if mine[f.Scanner] {
						return &types.MsgUnflagRemoved{Authority: policy, Hashes: []string{rec.Sha256}}, "", nil
					}
				}
			}
			return nil, "no flag by a scanner the body controls", nil
		})
	})
}

// SimulateMsgUpdateOperationalParams has the Commons Operations Committee
// move a backend's prices inside the governance bounds.
func SimulateMsgUpdateOperationalParams(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgUpdateOperationalParams{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		backends, err := e.backends()
		if err != nil {
			return noOpErr(label, err)
		}
		return e.advance(label, []string{"Commons Operations Committee"}, decodeAs[types.MsgUpdateOperationalParams], func(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			b, ok := pick(e.r, backends)
			if !ok || len(taken) > 0 {
				return nil, "no backend, or a price change is already open", nil
			}
			// Stay near the floor so requests remain affordable to sim accounts.
			price := b.MinPricePerByte.AddRaw(int64(e.r.Intn(20)))
			if price.GT(b.MaxPricePerByte) {
				price = b.MaxPricePerByte
			}
			minFee := b.MinMinFee.AddRaw(int64(e.r.Intn(1_000_000)))
			if minFee.GT(b.MaxMinFee) {
				minFee = b.MaxMinFee
			}
			return &types.MsgUpdateOperationalParams{Authority: policy, Backend: b.Name, PricePerByte: price, MinFee: minFee}, "", nil
		})
	})
}

// The removals and the governance-only messages are not simulated: removing
// approvals would starve every other op, and MsgUpdateBackend and
// MsgUpdateParams are signed by x/gov, which no sim account can sign for.

func SimulateMsgRemoveOperator(types.AuthKeeper, types.BankKeeper, keeper.Keeper, client.TxConfig) simtypes.Operation {
	return skipped(&types.MsgRemoveOperator{}, "removals are not simulated")
}

func SimulateMsgRemoveScanner(types.AuthKeeper, types.BankKeeper, keeper.Keeper, client.TxConfig) simtypes.Operation {
	return skipped(&types.MsgRemoveScanner{}, "removals are not simulated")
}

func SimulateMsgUpdateBackend(types.AuthKeeper, types.BankKeeper, keeper.Keeper, client.TxConfig) simtypes.Operation {
	return skipped(&types.MsgUpdateBackend{}, "signed by x/gov only")
}
