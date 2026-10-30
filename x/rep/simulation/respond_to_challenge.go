package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgRespondToChallenge has the assignee of a challenged initiative
// answer an ACTIVE challenge: usually with a defense, which seats a jury,
// sometimes with an empty response, which concedes it.
func SimulateMsgRespondToChallenge(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgRespondToChallenge{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		active := func() ([]types.Challenge, error) {
			return e.challenges(func(c types.Challenge) bool { return c.Status == types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE })
		}
		chs, err := active()
		if err != nil {
			return noOpErr(label, err)
		}
		if len(chs) == 0 {
			// Prerequisite: a member challenges submitted work.
			cands, reason, err := e.challengeCandidates()
			if err != nil {
				return noOpErr(label, err)
			}
			opMsg, fops, err := e.firstValid(&types.MsgCreateChallenge{}, cands, 4, reason)
			if err != nil || !opMsg.OK {
				return opMsg, fops, err
			}
			if chs, err = active(); err != nil {
				return noOpErr(label, err)
			}
		}
		var cands []candidate
		for _, c := range chs {
			in, ok := e.initiative(c.InitiativeId)
			if !ok {
				continue
			}
			acc, ok := e.account(in.Assignee)
			if !ok {
				continue
			}
			msg := &types.MsgRespondToChallenge{Assignee: in.Assignee, ChallengeId: c.Id}
			if e.r.Intn(6) != 0 {
				msg.Response = randomReason(e.r)
				msg.Evidence = []string{randomURI(e.r)}
			}
			cands = append(cands, candidate{acc: acc, msg: msg, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 4, "no ACTIVE challenge with a live assignee")
	})
}
