package simulation

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgUpdateTitle updates a title (genesis or sim) through an
// executed x/commons proposal (council.go), one step per call. The
// requirement type, season and seasonal flag carry over; the name,
// description, rarity and threshold change.
func SimulateMsgUpdateTitle(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return authorityOp(ak, bk, k, txGen, &types.MsgUpdateTitle{}, decodeAs[types.MsgUpdateTitle](),
		func(e *simEnv, policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			ids, err := allTitles(e.ctx, e.k)
			if err != nil {
				return nil, "", err
			}
			id, ok := pick(e.r, without(ids, pendingIDs(taken, titleIDOf)))
			if !ok {
				return nil, "no title to update", nil
			}
			t, err := e.k.Title.Get(e.ctx, id)
			if err != nil {
				return nil, "", err
			}
			return &types.MsgUpdateTitle{
				Authority:            policy,
				TitleId:              id,
				Name:                 fmt.Sprintf("Warden of %s", pickName(e)),
				Description:          "An updated simulation title",
				Rarity:               randomRarity(e),
				RequirementType:      commontypes.Saturate[uint32](t.RequirementType),
				RequirementThreshold: randomThreshold(e),
				RequirementSeason:    t.RequirementSeason,
				Seasonal:             t.Seasonal,
			}, "", nil
		})
}
