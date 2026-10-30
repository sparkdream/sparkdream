package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgFinalizeControllerTransfer applies an x/rep jury verdict to an
// open controller-transfer case. While the jury is undecided its seated
// live sim jurors vote first (real MsgSubmitJurorVote txs); once it has
// decided, a Commons Operations Committee member relays the matching
// verdict: ACCEPT (moving the operator to the proposed controller) after
// an upheld challenge, REJECT after a rejected or inconclusive one.
func SimulateMsgFinalizeControllerTransfer(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgFinalizeControllerTransfer{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		cases, err := e.transferCases()
		if err != nil {
			return noOpErr(label, err)
		}
		c, ok := pick(e.r, cases)
		if !ok {
			return noOp(label, "no open controller-transfer case")
		}
		verdict, vote, why, err := e.juryVerdict(c.JuryCaseId)
		if err != nil {
			return noOpErr(label, err)
		}
		if verdict == reptypes.Verdict_VERDICT_PENDING {
			if vote.OK {
				return relabel(vote, sdk.MsgTypeURL(label), "jurors vote on the case behind"), nil, nil
			}
			return noOp(label, "jury undecided: "+why)
		}
		relayer, ok := pick(e.r, e.verdictRelayers())
		if !ok {
			return noOp(label, "no live Commons Operations Committee member to relay the verdict")
		}
		msg := &types.MsgFinalizeControllerTransfer{
			JuryAuthority: relayer.Address.String(),
			JuryCaseId:    c.JuryCaseId,
			Verdict:       types.TransferVerdict_TRANSFER_VERDICT_REJECT,
		}
		if verdict == reptypes.Verdict_VERDICT_UPHOLD_CHALLENGE {
			msg.Verdict = types.TransferVerdict_TRANSFER_VERDICT_ACCEPT
			msg.NewController = c.ProposedNewController
		}
		return e.submit(relayer, msg, math.ZeroInt())
	})
}
