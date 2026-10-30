package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgFlagContent delivers MsgFlagContent from an active member under
// max_flags_per_day on a public, active collection or non-hidden item it has
// not flagged yet and whose flaggers are below max_flaggers_per_target.
// Reason OTHER carries a short reason text; every other reason carries none.
func SimulateMsgFlagContent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgFlagContent{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgFlagContent{}
		p := e.params
		targets, err := e.publicTargets(false)
		if err != nil {
			return noOpErr(msg, err)
		}
		t, ok := pick(e.r, targets)
		if !ok {
			return noOp(msg, "no public active target")
		}
		flag, err := e.k.Flag.Get(e.ctx, keeper.FlagCompositeKey(t.typ, t.id))
		hasFlag := err == nil
		if hasFlag && commontypes.Saturate[uint32](len(flag.FlagRecords)) >= p.MaxFlaggersPerTarget {
			return noOp(msg, "max flaggers per target")
		}
		flaggers := accountsWhere(e.accs, func(a simtypes.Account) bool {
			addr := a.Address.String()
			if !e.isActiveMember(addr) || e.dailyCount(addr, "flag") >= p.MaxFlagsPerDay {
				return false
			}
			for _, rec := range flag.FlagRecords {
				if rec.Flagger == addr {
					return false
				}
			}
			return true
		})
		flagger, ok := pick(e.r, flaggers)
		if !ok {
			return noOp(msg, "no member may flag the target")
		}
		msg.Creator, msg.TargetId, msg.TargetType = flagger.Address.String(), t.id, t.typ
		msg.Reason = randomModerationReason(e.r)
		if e.r.Intn(5) == 0 && p.MaxFlagReasonLength > 0 {
			msg.Reason = commontypes.ModerationReason_MODERATION_REASON_OTHER
			msg.ReasonText = "off-topic for aurora"
			if commontypes.Saturate[uint32](len(msg.ReasonText)) > p.MaxFlagReasonLength {
				msg.ReasonText = msg.ReasonText[:p.MaxFlagReasonLength]
			}
		}
		return e.submit(flagger, msg, math.ZeroInt())
	})
}
