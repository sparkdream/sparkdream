package simulation

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgSubmitArbiterHash delivers MsgSubmitArbiterHash from a working
// bridge operator of the disputed content's peer (registering one first
// when none can arbitrate) for CHALLENGED or DISPUTED content whose
// arbitration is open. A dispute is opened first when none exists, by a
// verifier that reports a different hash. The arbiter is never the
// submitting operator, the verifier or the challenger, and submits once
// per round; it mostly reports the anchored hash.
func SimulateMsgSubmitArbiterHash(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgSubmitArbiterHash{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgSubmitArbiterHash{}
		c, rec, ok, err := e.ensureDisputedContent(func(c types.FederatedContent, rec types.VerificationRecord) bool {
			return e.arbitrationOpen(rec)
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		if !ok {
			return noOp(msg, "no CHALLENGED or DISPUTED content with open arbitration")
		}
		notParty := func(addr string) bool {
			if e.sameMember(addr, c.SubmittedBy) || e.sameMember(addr, rec.Challenger) || e.sameMember(addr, rec.Verifier) {
				return false
			}
			has, err := e.k.ArbiterSubmissions.Has(e.ctx, collections.Join(c.Id, addr))
			return err == nil && !has
		}
		b, ok, err := e.ensureBinding(
			func(b types.BridgeBinding) bool { return b.PeerId == c.PeerId && notParty(b.Address) },
			func(p types.Peer) bool { return p.Id == c.PeerId },
			func(a simtypes.Account) bool { return notParty(a.Address.String()) },
		)
		if err != nil {
			return noOpErr(msg, err)
		}
		if !ok {
			return noOp(msg, "no bridge operator of the peer outside the dispute")
		}
		acc, ok := e.account(b.Address)
		if !ok {
			return noOp(msg, "arbiter is not a live sim account")
		}
		msg.Creator, msg.ContentId, msg.ContentHash = b.Address, c.Id, c.ContentHash
		if e.r.Intn(4) == 0 && len(rec.VerifierHash) > 0 {
			msg.ContentHash = rec.VerifierHash
		}
		return e.submit(acc, msg, math.ZeroInt())
	})
}

// ensureDisputedContent returns CHALLENGED or DISPUTED content with a
// verification record accepted by keep, opening a dispute first when none
// exists: a bonded verifier (not the submitter's member) reports a hash
// that differs from the anchored one on pending content.
func (e *simEnv) ensureDisputedContent(keep func(types.FederatedContent, types.VerificationRecord) bool) (types.FederatedContent, types.VerificationRecord, bool, error) {
	accept := func(c types.FederatedContent) bool {
		if !inDispute(c) {
			return false
		}
		rec, ok := e.record(c.Id)
		return ok && keep(c, rec)
	}
	disputed, err := e.contentsWhere(accept)
	if err != nil {
		return types.FederatedContent{}, types.VerificationRecord{}, false, err
	}
	if c, ok := pick(e.r, disputed); ok {
		rec, _ := e.record(c.Id)
		return c, rec, true, nil
	}
	c, ok, err := e.ensurePendingContent(func(types.FederatedContent) bool { return true })
	if err != nil || !ok {
		return types.FederatedContent{}, types.VerificationRecord{}, false, err
	}
	vs, err := e.ensureVerifiers(func(a simtypes.Account) bool { return !e.sameMember(a.Address.String(), c.SubmittedBy) })
	if err != nil {
		return types.FederatedContent{}, types.VerificationRecord{}, false, err
	}
	v, ok := pick(e.r, vs)
	if !ok {
		return types.FederatedContent{}, types.VerificationRecord{}, false, nil
	}
	msg := &types.MsgVerifyContent{Creator: v.Address.String(), ContentId: c.Id, ContentHash: contentHash(e.r, "disputed")}
	if ok, err := e.ensure(v, msg, math.ZeroInt()); err != nil || !ok {
		return types.FederatedContent{}, types.VerificationRecord{}, false, err
	}
	c, found := e.content(c.Id)
	if !found || !accept(c) {
		return types.FederatedContent{}, types.VerificationRecord{}, false, nil
	}
	rec, _ := e.record(c.Id)
	return c, rec, true, nil
}
