package simulation

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// SimulateMsgOpenPoll opens a non-binding poll on a group with a live term,
// created by one of its live sim seats. A quarter of the polls ask for a
// secret ballot; when the handler refuses one (members without registered
// ZK keys, unequal weights) the poll is opened as an open ballot instead.
// Polls last up to three days, so the EndBlocker closes them within the run.
func SimulateMsgOpenPoll(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgOpenPoll{}
	return operation(ak, bk, k, txGen, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		groups, err := e.groups(func(_ string, g types.Group) bool { return !e.termExpired(g) })
		if err != nil {
			return noOpErr(label, err)
		}
		for _, i := range e.r.Perm(len(groups)) {
			seats, err := e.liveSeatAccounts(groups[i].name)
			if err != nil {
				return noOpErr(label, err)
			}
			creator, ok := pick(e.r, seats)
			if !ok {
				continue
			}
			n := keeper.MinPollOptions + e.r.Intn(4)
			options := make([]string, n)
			for j := range options {
				options[j] = fmt.Sprintf("%s %d", randomFictional(e.r), j+1)
			}
			minDur := int64(keeper.MinPollDuration.Seconds())
			msg := &types.MsgOpenPoll{
				Creator:         creator.Address.String(),
				CouncilName:     groups[i].name,
				Question:        fmt.Sprintf("Should the %s initiative go ahead?", randomFictional(e.r)),
				Options:         options,
				DurationSeconds: minDur + e.r.Int63n(3*day),
				SecretBallot:    e.r.Intn(4) == 0,
			}
			if msg.SecretBallot && e.dryRun(msg) != nil {
				msg.SecretBallot = false
			}
			if e.dryRun(msg) != nil {
				continue // creator or group at its open-poll cap
			}
			return e.submit(creator, msg)
		}
		return noOp(label, "no live group seat can open a poll")
	})
}
