package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgFundInitiativeBounty has a member unaffiliated with an OPEN or
// ASSIGNED initiative escrow DREAM as a bounty for its assignee. With no
// such initiative one is created first.
func SimulateMsgFundInitiativeBounty(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgFundInitiativeBounty{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		fundable := func() ([]types.Initiative, error) {
			return e.initiatives(func(in types.Initiative) bool {
				return in.Status == types.InitiativeStatus_INITIATIVE_STATUS_OPEN ||
					in.Status == types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED
			})
		}
		ins, err := fundable()
		if err != nil {
			return noOpErr(label, err)
		}
		if len(ins) == 0 {
			in, ok, err := e.openInitiative(nil)
			if err != nil {
				return noOpErr(label, err)
			}
			if !ok {
				return noOp(label, "no OPEN or ASSIGNED initiative and none could be created")
			}
			ins = []types.Initiative{in}
		}
		floor := e.params.MinInitiativeBountyContribution
		if floor.IsNil() || !floor.IsPositive() {
			floor = math.OneInt()
		}
		funders, err := e.members(func(ma memberAcc) bool { return unlocked(ma.m).QuoRaw(4).GTE(floor) })
		if err != nil {
			return noOpErr(label, err)
		}
		var cands []candidate
		for _, in := range ins {
			ma, ok := pick(e.r, funders)
			if !ok {
				break
			}
			amt := randBetween(e.r, floor, math.MinInt(floor.MulRaw(5), unlocked(ma.m).QuoRaw(4)))
			cands = append(cands, candidate{acc: ma.acc, msg: &types.MsgFundInitiativeBounty{
				Funder: ma.addr(), InitiativeId: in.Id, Amount: amt,
			}, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 6, "no member can fund a bounty")
	})
}
