package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgAddCollaborator delivers MsgAddCollaborator from the owner or
// an ADMIN of a mutable collection below max_collaborators, adding another
// sim account (usually a member) that is not yet a collaborator. Only the
// owner grants ADMIN. A non-member collaborator is EDITOR only and needs an
// inviter at min_sponsor_trust_level, a free non-member slot and the
// inviter's non_member_collab_dream_stake (locked in x/rep).
func SimulateMsgAddCollaborator(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgAddCollaborator{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		granter, msg, reason, err := e.newCollaboratorMsg()
		if err != nil {
			return noOpErr(&types.MsgAddCollaborator{}, err)
		}
		if reason != "" {
			return noOp(msg, reason)
		}
		return e.submit(granter, msg, math.ZeroInt())
	})
}

// newCollaboratorMsg builds a MsgAddCollaborator passing the handler's local
// checks, with its signer. When no sim account administers a collection, a
// random account first creates one. A non-empty reason means none could be
// built.
func (e *simEnv) newCollaboratorMsg() (simtypes.Account, *types.MsgAddCollaborator, string, error) {
	msg := &types.MsgAddCollaborator{}
	p := e.params
	type grant struct {
		coll    types.Collection
		granter simtypes.Account
	}
	list := func() ([]grant, error) {
		colls, err := e.collectionsWhere(func(c types.Collection) bool {
			return !c.Immutable && c.CollaboratorCount < p.MaxCollaboratorsPerCollection
		})
		if err != nil {
			return nil, err
		}
		var grants []grant
		for _, c := range colls {
			for _, a := range e.accs {
				addr := a.Address.String()
				if addr == c.Owner {
					grants = append(grants, grant{c, a})
				} else if collab, ok := e.collaborator(c.Id, addr); ok && collab.Role == types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN {
					grants = append(grants, grant{c, a})
				}
			}
		}
		return grants, nil
	}
	grants, err := list()
	if err != nil {
		return simtypes.Account{}, msg, "", err
	}
	if len(grants) == 0 {
		if ok, err := e.ensureAnyCollection(); err != nil || !ok {
			return simtypes.Account{}, msg, "no collection a sim account administers", err
		}
		if grants, err = list(); err != nil {
			return simtypes.Account{}, msg, "", err
		}
	}
	g, ok := pick(e.r, grants)
	if !ok {
		return simtypes.Account{}, msg, "no collection a sim account administers", nil
	}
	granter := g.granter.Address.String()
	free := func(a simtypes.Account) bool {
		addr := a.Address.String()
		_, already := e.collaborator(g.coll.Id, addr)
		return addr != g.coll.Owner && addr != granter && !already
	}
	candidates := accountsWhere(e.accs, func(a simtypes.Account) bool { return free(a) && e.isActiveMember(a.Address.String()) })
	if len(candidates) == 0 || e.r.Intn(4) == 0 {
		candidates = accountsWhere(e.accs, free)
	}
	target, ok := pick(e.r, candidates)
	if !ok {
		return simtypes.Account{}, msg, "no account to add", nil
	}
	addr := target.Address.String()
	if e.tombstoned(addr) {
		return simtypes.Account{}, msg, "collaborator address is tombstoned", nil
	}
	msg.Creator, msg.CollectionId, msg.Address = granter, g.coll.Id, addr
	msg.Role = types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR
	if granter == g.coll.Owner && e.r.Intn(3) == 0 {
		msg.Role = types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN
	}
	if !e.isActiveMember(addr) {
		msg.Role = types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR
		if !e.trustAtLeastName(granter, p.MinSponsorTrustLevel) {
			return simtypes.Account{}, msg, "non-member collaborator needs an inviter at min_sponsor_trust_level", nil
		}
		if g.coll.NonMemberCollaboratorCount >= p.MaxNonMemberCollaboratorsPerCollection {
			return simtypes.Account{}, msg, "no non-member collaborator slot", nil
		}
		if e.unlockedDream(granter).LT(p.NonMemberCollabDreamStake) {
			return simtypes.Account{}, msg, "inviter lacks the non-member collaborator DREAM stake", nil
		}
	}
	return g.granter, msg, "", nil
}

// ensureCollaborator adds a collaborator by delivering a real
// MsgAddCollaborator. It returns false when none could be added.
func (e *simEnv) ensureCollaborator() (bool, error) {
	granter, msg, reason, err := e.newCollaboratorMsg()
	if err != nil || reason != "" {
		return false, err
	}
	return e.ensure(granter, msg, math.ZeroInt())
}
