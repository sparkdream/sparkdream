package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// minCategoriesKept is how many categories DeleteCategory leaves for the
// forum simulation to post in.
const minCategoriesKept = 3

// SimulateMsgDeleteCategory deletes a category without forum posts, signed
// by a live member of the Commons Operations Committee. The dry-run applies
// the handler's x/forum emptiness check.
func SimulateMsgDeleteCategory(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgDeleteCategory{}
	return operation(ak, bk, k, txGen, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		var ids []uint64
		if err := e.k.Category.Walk(e.ctx, nil, func(id uint64, _ types.Category) (bool, error) {
			ids = append(ids, id)
			return false, nil
		}); err != nil {
			return noOpErr(label, err)
		}
		if len(ids) <= minCategoriesKept {
			return noOp(label, "too few categories to delete one")
		}
		signer, ok, err := e.opsMember()
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no live Commons Operations Committee member")
		}
		for _, i := range e.r.Perm(len(ids)) {
			msg := &types.MsgDeleteCategory{Creator: signer.Address.String(), CategoryId: ids[i]}
			if e.dryRun(msg) == nil {
				return e.submit(signer, msg)
			}
		}
		return noOp(label, "every category still has forum posts")
	})
}
