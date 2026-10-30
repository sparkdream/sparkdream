package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgModerateContent delivers MsgModerateContent hiding a record
// from a Commons Operations Committee member: hiding is the fast brake a
// single member may pull. Rejecting and restoring need x/gov or a policy
// address and are never simulated. A NoOp when no sim account holds a seat
// or no record can be hidden (terminal UNRESOLVED/SUPERSEDED, already
// hidden, or mid-dispute).
func SimulateMsgModerateContent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgModerateContent{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgModerateContent{}
		signer, ok := pick(e.r, e.opsCommittee())
		if !ok {
			return noOp(msg, "no sim account is a Commons Operations Committee member")
		}
		contents, err := e.contentsWhere(func(c types.FederatedContent) bool {
			switch c.Status {
			case types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_UNRESOLVED,
				types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_SUPERSEDED,
				types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN:
				return false
			}
			if inDispute(c) || e.escalated(c.Id) {
				return false
			}
			rec, has := e.record(c.Id)
			return !has || rec.PendingVerifierVerdict == types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_UNSPECIFIED
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		c, ok := pick(e.r, contents)
		if !ok {
			return noOp(msg, "no content record that can be hidden")
		}
		msg.Authority, msg.ContentId = signer.Address.String(), c.Id
		msg.NewStatus = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN
		msg.Reason = "simulation moderation"
		return e.submit(signer, msg, math.ZeroInt())
	})
}
