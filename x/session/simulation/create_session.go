package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// buildCreateSession builds a legacy MsgCreateSession (the SESSION_KEY
// facade) from granter to grantee.
func (e *simEnv) buildCreateSession(granter, grantee simtypes.Account) *types.MsgCreateSession {
	return &types.MsgCreateSession{
		Granter:         granter.Address.String(),
		Grantee:         grantee.Address.String(),
		AllowedMsgTypes: e.sessionMsgTypes(false),
		SpendLimit:      e.spendLimit(),
		Expiration:      e.sessionExpiry(),
		MaxExecCount:    e.maxExecCount(),
	}
}

// SimulateMsgCreateSession creates a session key through the legacy
// MsgCreateSession facade between two live sim accounts.
func SimulateMsgCreateSession(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	label := &types.MsgCreateSession{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		reason := "no pair of live sim accounts"
		for attempt := 0; attempt < 3; attempt++ {
			granter, grantee, ok := e.pair(math.NewInt(10_000))
			if !ok {
				break
			}
			msg := e.buildCreateSession(granter, grantee)
			if err := e.dryRun(msg); err != nil {
				reason = "handler would reject: " + err.Error()
				continue
			}
			return e.deliver(granter, msg, math.ZeroInt())
		}
		return noOp(label, reason)
	})
}
