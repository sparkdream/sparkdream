package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// SimulateMsgResolveDispute resolves an active dispute through an executed
// Commons Council proposal (council.go), one step per call. The resolution
// dismisses the dispute or, when the dispute was contested or its contest
// window has closed, upholds it at random (transferring the name to the
// claimant). When nothing is disputed a member files a dispute first (a
// real MsgFileDispute).
func SimulateMsgResolveDispute(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgResolveDispute{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return e.advance(label, decodeResolve, e.buildResolve)
	})
}

// decodeResolve decodes a proposal message carrying a MsgResolveDispute.
func decodeResolve(a *codectypes.Any) (sdk.Msg, bool) {
	m := &types.MsgResolveDispute{}
	if a == nil || m.Unmarshal(a.Value) != nil {
		return nil, false
	}
	return m, true
}

// buildResolve builds a MsgResolveDispute signed by policy for an active
// dispute no open proposal already resolves.
func (e *simEnv) buildResolve(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
	covered := map[string]bool{}
	for _, m := range taken {
		if rd, ok := m.(*types.MsgResolveDispute); ok {
			covered[normalize(rd.Name)] = true
		}
	}
	keep := func(d types.Dispute) bool { return !covered[d.Name] }
	open, err := e.activeDisputes(keep)
	if err != nil {
		return nil, "", err
	}
	d, ok := pick(e.r, open)
	if !ok {
		file, claimant, reason, err := e.newFileDispute(anyone)
		if err != nil || reason != "" {
			return nil, "no active dispute: " + reason, err
		}
		if ok, err := e.ensure(claimant, file, zero()); err != nil || !ok {
			return nil, "no active dispute and none could be filed", err
		}
		found := false
		if d, found, err = e.dispute(file.Name); err != nil || !found || !d.Active {
			return nil, "the filed dispute is not active", err
		}
	}
	msg := &types.MsgResolveDispute{Authority: policy, Name: d.Name}
	// An uncontested dispute can be upheld only after the owner's contest
	// window closes.
	if (d.ContestChallengeId != "" || e.ctx.BlockHeight() > e.contestDeadline(d)) && e.r.Intn(2) == 0 {
		msg.TransferApproved = true
		if e.r.Intn(2) == 0 {
			msg.NewOwner = d.Claimant
		}
	}
	return msg, "", nil
}
