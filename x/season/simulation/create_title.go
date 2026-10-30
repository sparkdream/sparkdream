package simulation

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgCreateTitle creates a sim title through an executed x/commons
// proposal (council.go), one step per call, up to simDefMax of them.
func SimulateMsgCreateTitle(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return authorityOp(ak, bk, k, txGen, &types.MsgCreateTitle{}, decodeAs[types.MsgCreateTitle](),
		func(e *simEnv, policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			ids, err := allTitles(e.ctx, e.k)
			if err != nil {
				return nil, "", err
			}
			if len(simOwned(ids))+len(taken) >= simDefMax {
				return nil, "enough sim titles", nil
			}
			id, ok := newDefinitionID(e, "title", ids, pendingIDs(taken, titleIDOf))
			if !ok {
				return nil, "title id already taken", nil
			}
			return &types.MsgCreateTitle{
				Authority:            policy,
				TitleId:              id,
				Name:                 fmt.Sprintf("Keeper of %s", pickName(e)),
				Description:          "A simulation generated title",
				Rarity:               randomRarity(e),
				RequirementType:      randomRequirementType(e),
				RequirementThreshold: randomThreshold(e),
				Seasonal:             e.r.Intn(4) == 0,
			}, "", nil
		})
}
