package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgResolveTagReport has a Commons Operations Committee seat dismiss
// an open tag report (a member files one first when none is open) (the reporters' bonds are forfeited). Removing or
// reserving the tag is not simulated: the other modules' simulations draw
// from the seeded tags and would lose them.
func SimulateMsgResolveTagReport(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgResolveTagReport{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		seats := e.liveSeats(commonsOpsCommittee)
		if len(seats) == 0 {
			return noOp(label, "no live Commons Operations Committee seat")
		}
		open := false
		if err := e.k.TagReport.Walk(e.ctx, nil, func(string, types.TagReport) (bool, error) {
			open = true
			return true, nil
		}); err != nil {
			return noOpErr(label, err)
		}
		if !open {
			// Prerequisite: a member reports a tag.
			cands, err := e.reportTagCandidates()
			if err != nil {
				return noOpErr(label, err)
			}
			opMsg, fops, err := e.firstValid(&types.MsgReportTag{}, cands, 4, "no member can report a tag")
			if err != nil || !opMsg.OK {
				return opMsg, fops, err
			}
		}
		var cands []candidate
		err := e.k.TagReport.Walk(e.ctx, nil, func(name string, _ types.TagReport) (bool, error) {
			by := seats[e.r.Intn(len(seats))]
			cands = append(cands, candidate{acc: by, msg: &types.MsgResolveTagReport{
				Creator: by.Address.String(), TagName: name, Action: 0,
			}, spent: math.ZeroInt()})
			return false, nil
		})
		if err != nil {
			return noOpErr(label, err)
		}
		return e.firstValid(label, cands, 4, "no open tag report")
	})
}
