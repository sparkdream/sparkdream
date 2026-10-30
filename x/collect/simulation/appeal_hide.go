package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
)

// SimulateMsgAppealHide delivers MsgAppealHide from the content owner (the
// collection owner, or for an item its adder or the collection owner) of an
// unresolved, unappealed hide once appeal_cooldown_blocks have passed and
// before the appeal deadline. x/rep charges its appeal bond and seats a
// jury (dry-run). With the default 600-block cooldown, a hide only becomes
// appealable in runs longer than that.
func SimulateMsgAppealHide(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgAppealHide{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAppealHide{}
		height := e.ctx.BlockHeight()
		type appeal struct {
			hr        types.HideRecord
			appellant simtypes.Account
		}
		var appeals []appeal
		if err := e.k.HideRecord.Walk(e.ctx, nil, func(_ uint64, hr types.HideRecord) (bool, error) {
			if hr.Resolved || hr.Appealed || height < hr.HiddenAt+e.params.AppealCooldownBlocks || height >= hr.AppealDeadline {
				return false, nil
			}
			for _, o := range e.contentOwners(hr) {
				if acc, ok := getAccountForAddress(o, e.accs); ok {
					appeals = append(appeals, appeal{hr, acc})
				}
			}
			return false, nil
		}); err != nil {
			return noOpErr(msg, err)
		}
		a, ok := pick(e.r, appeals)
		if !ok {
			return noOp(msg, "no appealable hide owned by a sim account")
		}
		msg.Creator, msg.HideRecordId = a.appellant.Address.String(), a.hr.Id
		return e.submit(a.appellant, msg, math.NewInt(reptypes.DefaultAppealBondAmount)) // x/rep charges the SPARK appeal bond
	})
}

// contentOwners returns the addresses that may appeal a hide: the collection
// owner, and for an item also its adder. A deleted target has none.
func (e *simEnv) contentOwners(hr types.HideRecord) []string {
	var out []string
	switch hr.TargetType {
	case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
		if c, err := e.k.Collection.Get(e.ctx, hr.TargetId); err == nil {
			out = append(out, c.Owner)
		}
	case types.FlagTargetType_FLAG_TARGET_TYPE_ITEM:
		if it, err := e.k.Item.Get(e.ctx, hr.TargetId); err == nil {
			out = append(out, it.AddedBy)
			if c, err := e.k.Collection.Get(e.ctx, it.CollectionId); err == nil {
				out = append(out, c.Owner)
			}
		}
	}
	return out
}
