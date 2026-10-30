package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgRemoveCollaborator delivers MsgRemoveCollaborator for an
// existing collaborator, signed by the collaborator itself (always allowed,
// even on an immutable collection), the owner, or an ADMIN removing a
// non-ADMIN (both only on a mutable collection). A non-member's inviter
// stake is released (part burned when the collection is HIDDEN).
func SimulateMsgRemoveCollaborator(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgRemoveCollaborator{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRemoveCollaborator{}
		type removal struct {
			collab  types.Collaborator
			remover simtypes.Account
		}
		// With no collaborator anywhere, add one first (a real MsgAddCollaborator).
		n, err := e.countCollaborators()
		if err != nil {
			return noOpErr(msg, err)
		}
		if n == 0 {
			if _, err := e.ensureCollaborator(); err != nil {
				return noOpErr(msg, err)
			}
		}
		var removals []removal
		err = e.k.Collaborator.Walk(e.ctx, nil, func(_ string, collab types.Collaborator) (bool, error) {
			coll, err := e.k.Collection.Get(e.ctx, collab.CollectionId)
			if err != nil {
				return true, err
			}
			for _, a := range e.accs {
				addr := a.Address.String()
				switch {
				case addr == collab.Address:
					removals = append(removals, removal{collab, a})
				case coll.Immutable:
				case addr == coll.Owner:
					removals = append(removals, removal{collab, a})
				default:
					if mine, ok := e.collaborator(coll.Id, addr); ok &&
						mine.Role == types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN &&
						collab.Role != types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN {
						removals = append(removals, removal{collab, a})
					}
				}
			}
			return false, nil
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		rm, ok := pick(e.r, removals)
		if !ok {
			return noOp(msg, "no collaborator a sim account may remove")
		}
		msg.Creator, msg.CollectionId, msg.Address = rm.remover.Address.String(), rm.collab.CollectionId, rm.collab.Address
		return e.submit(rm.remover, msg, math.ZeroInt())
	})
}
