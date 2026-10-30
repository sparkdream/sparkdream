package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgCompleteInterim settles an ADJUDICATION interim (opened when a
// challenge escalates to the committee for want of jurors) with an UPHOLD or
// REJECT decision, through an executed x/commons proposal of the Commons
// Operations Committee (council.go), one step per call. Other interim types
// are completed by MsgApproveInterim.
func SimulateMsgCompleteInterim(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgCompleteInterim{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		bodies := e.groupsAllowing(sdk.MsgTypeURL(label))
		if len(bodies) == 0 {
			return noOp(label, "no x/commons group may execute MsgCompleteInterim")
		}
		return e.advance(label, bodies, decoder(func() *types.MsgCompleteInterim { return &types.MsgCompleteInterim{} }),
			func(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
				covered := takenIDs(taken, func(m *types.MsgCompleteInterim) uint64 { return m.InterimId })
				its, err := e.interims(func(it types.Interim) bool {
					return it.Type == types.InterimType_INTERIM_TYPE_ADJUDICATION && !covered[it.Id] &&
						(it.Status == types.InterimStatus_INTERIM_STATUS_PENDING || it.Status == types.InterimStatus_INTERIM_STATUS_IN_PROGRESS)
				})
				if err != nil {
					return nil, "", err
				}
				for _, i := range e.r.Perm(len(its)) {
					decision := types.AdjudicationDecision_ADJUDICATION_DECISION_UPHOLD
					if e.r.Intn(2) == 0 {
						decision = types.AdjudicationDecision_ADJUDICATION_DECISION_REJECT
					}
					msg := &types.MsgCompleteInterim{
						Creator: policy, InterimId: its[i].Id, Decision: decision, CompletionNotes: "simulation adjudication",
					}
					if e.dryRun(msg) == nil {
						return msg, "", nil
					}
				}
				return nil, "no open ADJUDICATION interim (no challenge escalated to the committee)", nil
			})
	})
}
