package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgEscalateChallenge delivers MsgEscalateChallenge for CHALLENGED
// or DISPUTED content not yet escalated, from its verifier or challenger,
// who escrows the escalation fee; once the arbiters stashed a verdict, only
// before the escalation deadline. A NoOp when no dispute exists or neither
// party is a live sim account (the escalation is rare by design: the jury
// resolution is policy-only and never simulated).
func SimulateMsgEscalateChallenge(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgEscalateChallenge{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgEscalateChallenge{}
		disputed, err := e.contentsWhere(func(c types.FederatedContent) bool {
			if !inDispute(c) || e.escalated(c.Id) {
				return false
			}
			rec, ok := e.record(c.Id)
			if !ok {
				return false
			}
			if rec.PendingVerifierVerdict != types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_UNSPECIFIED &&
				rec.ArbiterEscalationDeadline != 0 && e.now() > rec.ArbiterEscalationDeadline {
				return false
			}
			return true
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		c, ok := pick(e.r, disputed)
		if !ok {
			return noOp(msg, "no CHALLENGED or DISPUTED content to escalate")
		}
		if e.r.Intn(3) != 0 {
			return noOp(msg, "leaving the dispute to the arbiters")
		}
		rec, _ := e.record(c.Id)
		var parties []string
		for _, a := range []string{rec.Verifier, rec.Challenger} {
			if a != "" {
				parties = append(parties, a)
			}
		}
		addr, ok := pick(e.r, parties)
		if !ok {
			return noOp(msg, "dispute has no party")
		}
		acc, ok := e.account(addr)
		if !ok {
			return noOp(msg, "dispute party is not a live sim account")
		}
		msg.Creator, msg.ContentId = addr, c.Id
		return e.submit(acc, msg, e.params.EscalationFeeAmount)
	})
}
