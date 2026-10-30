package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgDeleteTitle deletes a sim-created title no member has unlocked
// or displays through an executed x/commons proposal (council.go), one step
// per call. DeleteTitle does not clean member profiles, so the sim never
// deletes a title a profile still holds.
func SimulateMsgDeleteTitle(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return authorityOp(ak, bk, k, txGen, &types.MsgDeleteTitle{}, decodeAs[types.MsgDeleteTitle](),
		func(e *simEnv, policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			ids, err := allTitles(e.ctx, e.k)
			if err != nil {
				return nil, "", err
			}
			skip := pendingIDs(taken, titleIDOf)
			if err := e.k.MemberProfile.Walk(e.ctx, nil, func(_ string, p types.MemberProfile) (bool, error) {
				skip[p.DisplayTitle] = true
				for _, t := range p.UnlockedTitles {
					skip[t] = true
				}
				for _, t := range p.ArchivedTitles {
					skip[t] = true
				}
				return false, nil
			}); err != nil {
				return nil, "", err
			}
			id, ok := pick(e.r, without(simOwned(ids), skip))
			if !ok {
				return nil, "no unheld sim title to delete", nil
			}
			return &types.MsgDeleteTitle{Authority: policy, TitleId: id}, "", nil
		})
}
