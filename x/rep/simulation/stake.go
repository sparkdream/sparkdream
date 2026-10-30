package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgStake has a member stake DREAM on an initiative that is still
// in progress, an ACTIVE project, another member or a registered tag.
func SimulateMsgStake(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgStake{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		stakers, err := e.members(func(ma memberAcc) bool { _, ok := e.stakeAmount(ma); return ok })
		if err != nil {
			return noOpErr(label, err)
		}
		if len(stakers) == 0 {
			return noOp(label, "no member has DREAM to stake")
		}
		ins, err := e.initiatives(func(in types.Initiative) bool {
			switch in.Status {
			case types.InitiativeStatus_INITIATIVE_STATUS_OPEN,
				types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED,
				types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED,
				types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW:
				return true
			}
			return false
		})
		if err != nil {
			return noOpErr(label, err)
		}
		ps, err := e.activeProjects()
		if err != nil {
			return noOpErr(label, err)
		}
		tags, err := e.tags()
		if err != nil {
			return noOpErr(label, err)
		}
		var cands []candidate
		for _, ma := range stakers {
			amt, _ := e.stakeAmount(ma)
			msg := &types.MsgStake{Staker: ma.addr(), Amount: PtrInt(amt)}
			switch x := e.r.Intn(10); {
			case x < 5 && len(ins) > 0:
				in := ins[e.r.Intn(len(ins))]
				msg.TargetType, msg.TargetId = types.StakeTargetType_STAKE_TARGET_INITIATIVE, in.Id
			case x < 7 && len(ps) > 0:
				p := ps[e.r.Intn(len(ps))]
				msg.TargetType, msg.TargetId = types.StakeTargetType_STAKE_TARGET_PROJECT, p.Id
			case x < 9:
				other, ok := pick(e.r, stakers)
				if !ok || other.addr() == ma.addr() {
					continue
				}
				msg.TargetType, msg.TargetIdentifier = types.StakeTargetType_STAKE_TARGET_MEMBER, other.addr()
			default:
				tag, ok := pick(e.r, tags)
				if !ok {
					continue
				}
				msg.TargetType, msg.TargetIdentifier = types.StakeTargetType_STAKE_TARGET_TAG, tag
			}
			cands = append(cands, candidate{acc: ma.acc, msg: msg, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 6, "no stake target accepts a stake")
	})
}
