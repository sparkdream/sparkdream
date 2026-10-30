package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgSetNextSeasonInfo sets the next season's name and theme
// through an executed x/commons proposal (council.go), one step per call;
// one proposal is open at a time. It only records the info a future
// transition reads.
func SimulateMsgSetNextSeasonInfo(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return authorityOp(ak, bk, k, txGen, &types.MsgSetNextSeasonInfo{}, decodeAs[types.MsgSetNextSeasonInfo](),
		func(e *simEnv, policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			if len(taken) > 0 {
				return nil, "next season info is already proposed", nil
			}
			themes := []string{"Renewal", "Convergence", "Open Waters", "First Light", "Long Harvest"}
			return &types.MsgSetNextSeasonInfo{
				Authority: policy,
				Name:      "Season of " + pickName(e),
				Theme:     themes[e.r.Intn(len(themes))],
			}, "", nil
		})
}
