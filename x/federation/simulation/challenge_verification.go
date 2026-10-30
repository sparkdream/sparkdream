package simulation

import (
	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"
)

// SimulateMsgChallengeVerification delivers MsgChallengeVerification for
// VERIFIED content inside its challenge window (verifying a pending record
// first when none exists). The challenger is a member at
// min_verifier_trust_level (ESTABLISHED by default, which sim members
// rarely reach), neither the verifier nor the submitting operator, and pays
// the challenge fee (doubled per prior rejected challenge) into escrow.
func SimulateMsgChallengeVerification(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgChallengeVerification{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgChallengeVerification{}
		level := reptypes.TrustLevel(commontypes.Saturate[int32](e.params.MinVerifierTrustLevel))
		challengers := accountsWhere(e.live(), func(a simtypes.Account) bool { return e.trustAtLeast(a.Address.String(), level) })
		if len(challengers) == 0 {
			return noOp(msg, "no member at min_verifier_trust_level")
		}
		c, rec, ok, err := e.ensureVerifiedContent(func(c types.FederatedContent, rec types.VerificationRecord) bool {
			return e.challengeable(c, rec)
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		if !ok {
			return noOp(msg, "no VERIFIED content inside its challenge window")
		}
		acc, ok := pick(e.r, accountsWhere(challengers, func(a simtypes.Account) bool {
			addr := a.Address.String()
			return !e.sameMember(addr, rec.Verifier) && !e.sameMember(addr, c.SubmittedBy)
		}))
		if !ok {
			return noOp(msg, "no eligible challenger other than the parties")
		}
		fee := e.challengeFee(rec)
		msg.Creator, msg.ContentId = acc.Address.String(), c.Id
		msg.ContentHash = contentHash(e.r, "challenge")
		msg.Evidence = "simulation challenge: the fetched source differs"
		return e.submit(acc, msg, fee)
	})
}

// challengeable mirrors ChallengeVerification's record gates: challenge
// window open, no jury escalation or leftover arbitration round, and the
// post-rejection cooldown passed.
func (e *simEnv) challengeable(_ types.FederatedContent, rec types.VerificationRecord) bool {
	if e.now() > rec.ChallengeWindowEnds || e.escalated(rec.ContentId) || e.hasArbiterData(rec.ContentId) {
		return false
	}
	if rec.PriorRejectedChallenges > 0 && rec.LastChallengeResolvedAt > 0 &&
		e.now() < rec.LastChallengeResolvedAt+int64(e.params.ChallengeCooldown.Seconds()) {
		return false
	}
	return true
}

// hasArbiterData reports whether an arbitration round of contentID is still
// stored (the keeper's hasArbiterData).
func (e *simEnv) hasArbiterData(contentID uint64) bool {
	found := false
	rng := collections.NewPrefixedPairRange[uint64, string](contentID)
	if err := e.k.ArbiterSubmissions.Walk(e.ctx, rng, func(collections.Pair[uint64, string], types.ArbiterHashSubmission) (bool, error) {
		found = true
		return true, nil
	}); err != nil {
		return true
	}
	if found {
		return true
	}
	if err := e.k.ArbiterHashCounts.Walk(e.ctx, rng, func(collections.Pair[uint64, string], uint32) (bool, error) {
		found = true
		return true, nil
	}); err != nil {
		return true
	}
	return found
}
