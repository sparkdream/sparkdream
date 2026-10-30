package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgUnhideContent delivers MsgUnhideContent on the sentinel
// self-correct path: the sentinel who hid the content walks back its own
// unresolved, unappealed hide within sentinel_unhide_window_blocks while
// the target still exists. The council path (any hide, any time, by a
// Commons Operations Committee member) is not simulated here.
func SimulateMsgUnhideContent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUnhideContent{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUnhideContent{}
		height := e.ctx.BlockHeight()
		var records []types.HideRecord
		if err := e.k.HideRecord.Walk(e.ctx, nil, func(_ uint64, hr types.HideRecord) (bool, error) {
			if hr.Resolved || hr.Appealed || hr.Sentinel == "" || height-hr.HiddenAt > e.params.SentinelUnhideWindowBlocks {
				return false, nil
			}
			if _, ok := getAccountForAddress(hr.Sentinel, e.accs); !ok || !e.hideTargetExists(hr) {
				return false, nil
			}
			records = append(records, hr)
			return false, nil
		}); err != nil {
			return noOpErr(msg, err)
		}
		hr, ok := pick(e.r, records)
		if !ok {
			return noOp(msg, "no hide a sim sentinel can self-correct")
		}
		sentinel, _ := getAccountForAddress(hr.Sentinel, e.accs)
		msg.Creator, msg.HideRecordId = hr.Sentinel, hr.Id
		return e.submit(sentinel, msg, math.ZeroInt())
	})
}
