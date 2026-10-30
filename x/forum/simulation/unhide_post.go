package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgUnhidePost delivers MsgUnhidePost on the sentinel
// self-correction path: the sentinel that hid a post unhides it inside
// sentinel_unhide_window, while the hide is not under appeal and the post's
// category still exists. The council path needs a Commons Operations
// Committee member, which the op uses when one exists among the sim
// accounts.
func SimulateMsgUnhidePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUnhidePost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUnhidePost{}
		if e.params.ModerationPaused {
			return noOp(msg, "moderation is paused")
		}
		window := e.params.SentinelUnhideWindow
		if window == 0 {
			window = types.DefaultSentinelUnhideWindow
		}
		var council []string
		for _, a := range e.opsCommittee() {
			council = append(council, a.Address.String())
		}
		type unhide struct {
			signer string
			post   uint64
		}
		var targets []unhide
		err := e.k.HideRecord.Walk(e.ctx, nil, func(id uint64, hr types.HideRecord) (bool, error) {
			if hr.Appealed {
				return false, nil
			}
			p, ok := e.post(id)
			if !ok || p.Status != types.PostStatus_POST_STATUS_HIDDEN {
				return false, nil
			}
			if _, ok := e.category(p.CategoryId); !ok {
				return false, nil
			}
			if hr.Sentinel != "" && e.now()-hr.HiddenAt <= window {
				if _, ok := e.account(hr.Sentinel); ok {
					targets = append(targets, unhide{hr.Sentinel, id})
				}
			}
			for _, c := range council {
				targets = append(targets, unhide{c, id})
			}
			return false, nil
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		t, ok := pick(e.r, targets)
		if !ok {
			return noOp(msg, "no hide the signer may undo")
		}
		acc, _ := e.account(t.signer)
		msg.Creator, msg.PostId = t.signer, t.post
		return e.submit(acc, msg, math.ZeroInt())
	})
}
