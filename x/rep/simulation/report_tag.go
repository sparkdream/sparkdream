package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// reportTagCandidates returns MsgReportTag candidates, one per member that
// can lock the tag-report bond, each on a random registered tag.
func (e *simEnv) reportTagCandidates() ([]candidate, error) {
	tags, err := e.tags()
	if err != nil || len(tags) == 0 {
		return nil, err
	}
	ms, err := e.members(func(ma memberAcc) bool { return unlocked(ma.m).QuoRaw(2).GTE(types.DefaultTagReportBond) })
	if err != nil {
		return nil, err
	}
	cands := make([]candidate, 0, len(ms))
	for _, ma := range ms {
		cands = append(cands, candidate{acc: ma.acc, msg: &types.MsgReportTag{
			Creator: ma.addr(), TagName: tags[e.r.Intn(len(tags))], Reason: randomReason(e.r),
		}, spent: math.ZeroInt()})
	}
	return cands, nil
}

// SimulateMsgReportTag has an active member lock the tag-report bond to flag
// a registered tag (opening a report or joining an open one).
func SimulateMsgReportTag(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgReportTag{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		cands, err := e.reportTagCandidates()
		if err != nil {
			return noOpErr(label, err)
		}
		return e.firstValid(label, cands, 4, "no member can report a registered tag")
	})
}
