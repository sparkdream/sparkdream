package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgVerifyContent delivers MsgVerifyContent from a bonded
// FEDERATION_VERIFIER (bonding one first through x/rep's MsgBondRole when
// none can act) for PENDING_VERIFICATION content inside its verification
// window (submitting one first when none exists). Mostly the verifier
// reports the anchored hash (VERIFIED); sometimes a different one
// (DISPUTED, which opens arbitration). The verifier is never the submitting
// operator's member. The handler reserves verifier_slash_amount of the bond
// in x/rep (DREAM, no SPARK).
func SimulateMsgVerifyContent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgVerifyContent{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgVerifyContent{}
		c, ok, err := e.ensurePendingContent(func(types.FederatedContent) bool { return true })
		if err != nil {
			return noOpErr(msg, err)
		}
		if !ok {
			return noOp(msg, "no pending content (needs a working bridge on an ACTIVE peer)")
		}
		vs, err := e.ensureVerifiers(func(a simtypes.Account) bool { return !e.sameMember(a.Address.String(), c.SubmittedBy) })
		if err != nil {
			return noOpErr(msg, err)
		}
		v, ok := pick(e.r, vs)
		if !ok {
			return noOp(msg, "no bonded verifier other than the submitting operator")
		}
		return e.submit(v, e.verifyMsg(v, c), math.ZeroInt())
	})
}
