package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgReclaimInitiativeBounty has a funder take back its matured
// contribution (past initiative_bounty_reclaim_delay) to the bounty of an initiative that is still OPEN.
func SimulateMsgReclaimInitiativeBounty(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgReclaimInitiativeBounty{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		var cands []candidate
		err := e.k.InitiativeBounty.Walk(e.ctx, nil, func(id uint64, b types.InitiativeBounty) (bool, error) {
			in, ok := e.initiative(id)
			if !ok || in.Status != types.InitiativeStatus_INITIATIVE_STATUS_OPEN {
				return false, nil
			}
			for _, c := range b.Contributions {
				if e.ctx.BlockHeight() < c.FundedAt+commontypes.Saturate[int64](e.params.InitiativeBountyReclaimDelay) {
					continue
				}
				if acc, ok := e.account(c.Funder); ok {
					cands = append(cands, candidate{acc: acc, msg: &types.MsgReclaimInitiativeBounty{
						Funder: c.Funder, InitiativeId: id,
					}, spent: math.ZeroInt()})
				}
			}
			return false, nil
		})
		if err != nil {
			return noOpErr(label, err)
		}
		return e.firstValid(label, cands, 4, "no live funder of an OPEN initiative's bounty")
	})
}
