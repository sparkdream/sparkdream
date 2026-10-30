package simulation

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// opsCommitteeName is the committee whose members manage categories.
const opsCommitteeName = "Commons Operations Committee"

// maxSimCategories bounds the live categories the simulation creates.
const maxSimCategories = 12

// SimulateMsgCreateCategory creates a category, signed by a live member of
// the Commons Operations Committee (IsCouncilAuthorized accepts committee
// members directly).
func SimulateMsgCreateCategory(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgCreateCategory{}
	return operation(ak, bk, k, txGen, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		count, err := e.categoryCount()
		if err != nil {
			return noOpErr(label, err)
		}
		if count >= maxSimCategories {
			return noOp(label, "category cap reached")
		}
		signer, ok, err := e.opsMember()
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no live Commons Operations Committee member")
		}
		name := randomFictional(e.r)
		return e.submit(signer, &types.MsgCreateCategory{
			Creator:          signer.Address.String(),
			Title:            fmt.Sprintf("%s %s", name, randomLowerAlnum(e.r, 4)),
			Description:      "Simulated category for " + name,
			MembersOnlyWrite: e.r.Intn(2) == 0,
			AdminOnlyWrite:   e.r.Intn(5) == 0,
		})
	})
}

// opsMember returns a live member of the Commons Operations Committee.
func (e *simEnv) opsMember() (simtypes.Account, bool, error) {
	seats, err := e.liveSeatAccounts(opsCommitteeName)
	if err != nil {
		return simtypes.Account{}, false, err
	}
	acc, ok := pick(e.r, seats)
	return acc, ok, nil
}

// categoryCount returns the number of categories.
func (e *simEnv) categoryCount() (int, error) {
	n := 0
	err := e.k.Category.Walk(e.ctx, nil, func(uint64, types.Category) (bool, error) {
		n++
		return false, nil
	})
	return n, err
}
