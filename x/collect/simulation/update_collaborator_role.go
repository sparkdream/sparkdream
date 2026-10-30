package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgUpdateCollaboratorRole delivers MsgUpdateCollaboratorRole on a
// mutable collection: the owner may set EDITOR or ADMIN on anyone (ADMIN
// never on a stake-bearing non-member collaborator); an ADMIN may only set
// EDITOR on a non-ADMIN collaborator.
func SimulateMsgUpdateCollaboratorRole(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUpdateCollaboratorRole{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUpdateCollaboratorRole{}
		type update struct {
			collab  types.Collaborator
			updater simtypes.Account
			owner   bool
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
		var updates []update
		err = e.k.Collaborator.Walk(e.ctx, nil, func(_ string, collab types.Collaborator) (bool, error) {
			coll, err := e.k.Collection.Get(e.ctx, collab.CollectionId)
			if err != nil {
				return true, err
			}
			if coll.Immutable {
				return false, nil
			}
			for _, a := range e.accs {
				addr := a.Address.String()
				if addr == coll.Owner {
					updates = append(updates, update{collab, a, true})
				} else if mine, ok := e.collaborator(coll.Id, addr); ok &&
					mine.Role == types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN &&
					collab.Role != types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN {
					updates = append(updates, update{collab, a, false})
				}
			}
			return false, nil
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		u, ok := pick(e.r, updates)
		if !ok {
			return noOp(msg, "no collaborator role a sim account may change")
		}
		msg.Creator, msg.CollectionId, msg.Address = u.updater.Address.String(), u.collab.CollectionId, u.collab.Address
		msg.Role = types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR
		stakeBearing := !u.collab.DreamStake.IsNil() && u.collab.DreamStake.IsPositive()
		if u.owner && !stakeBearing && e.r.Intn(2) == 0 {
			msg.Role = types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN
		}
		return e.submit(u.updater, msg, math.ZeroInt())
	})
}
