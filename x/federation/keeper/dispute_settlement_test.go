package keeper_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"
	servicetypes "sparkdream/x/service/types"
)

// Regression tests for the dispute-settlement findings FED-25..FED-29 and
// FED-31..FED-35. The FED-25 cases are the overlay tests that reproduced
// the double settlement, turned around to assert the fixed behaviour.

type disputeEnv struct {
	t          *testing.T
	f          *fixture
	ms         types.MsgServer
	peerID     string
	operator   string
	arbs       []string
	contentID  uint64
	verifier   string
	challenger string
	hash       []byte // the operator's (true) content hash
	wrong      []byte // the challenger's hash
	params     types.Params
	base       time.Time
}

// newDispute stands up VERIFIED content with a bonded verifier and nArbs
// identified arbiters on an ActivityPub peer, with explicit windows so the
// tests do not depend on the build's default values.
func newDispute(t *testing.T, prefix string, nArbs int) *disputeEnv {
	t.Helper()
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.ArbiterQuorum = 3
	params.ArbiterResolutionWindow = 24 * time.Hour
	params.ArbiterEscalationWindow = 48 * time.Hour
	params.ChallengeJuryDeadline = 14 * 24 * time.Hour
	params.ChallengeCooldown = 0
	params.VerificationWindow = 24 * time.Hour
	params.ChallengeWindow = 30 * 24 * time.Hour
	params.ContentTtl = 365 * 24 * time.Hour
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	e := &disputeEnv{t: t, f: f, ms: ms, peerID: prefix + "-peer", params: params, base: f.sdkCtx().BlockTime()}
	registerTestPeer(t, f, ms, e.peerID)
	e.operator = registerTestBridge(t, f, ms, e.peerID, prefix+"-op")
	for i := 0; i < nArbs; i++ {
		e.arbs = append(e.arbs, registerTestBridge(t, f, ms, e.peerID, fmt.Sprintf("%s-arb%d", prefix, i)))
	}
	h := sha256.Sum256([]byte(prefix + " content"))
	w := sha256.Sum256([]byte(prefix + " other"))
	e.hash, e.wrong = h[:], w[:]
	e.contentID = submitTestContent(t, f, ms, e.operator, e.peerID, e.hash)
	e.verifier = bondTestVerifier(t, f, ms, prefix+"-verifier")
	e.challenger = testAddr(t, f, prefix+"-chal")
	return e
}

// verify has the verifier submit hash (the true one = VERIFIED, any other
// = DISPUTED).
func (e *disputeEnv) verify(hash []byte) {
	e.t.Helper()
	_, err := e.ms.VerifyContent(e.f.ctx, &types.MsgVerifyContent{Creator: e.verifier, ContentId: e.contentID, ContentHash: hash})
	require.NoError(e.t, err)
}

func (e *disputeEnv) challenge(ctx context.Context) {
	e.t.Helper()
	_, err := e.ms.ChallengeVerification(ctx, &types.MsgChallengeVerification{Creator: e.challenger, ContentId: e.contentID, ContentHash: e.wrong})
	require.NoError(e.t, err)
}

func (e *disputeEnv) vote(ctx context.Context, arb string, hash []byte) error {
	_, err := e.ms.SubmitArbiterHash(ctx, &types.MsgSubmitArbiterHash{Creator: arb, ContentId: e.contentID, ContentHash: hash})
	return err
}

// at returns a context at base+d with a fresh event manager.
func (e *disputeEnv) at(d time.Duration) sdk.Context {
	return e.f.sdkCtx().WithBlockTime(e.base.Add(d)).WithEventManager(sdk.NewEventManager())
}

func (e *disputeEnv) endBlock(d time.Duration) sdk.Context {
	e.t.Helper()
	ctx := e.at(d)
	require.NoError(e.t, e.f.keeper.EndBlocker(ctx))
	return ctx
}

func (e *disputeEnv) record() types.VerificationRecord {
	e.t.Helper()
	r, err := e.f.keeper.VerificationRecords.Get(e.f.ctx, e.contentID)
	require.NoError(e.t, err)
	return r
}

func (e *disputeEnv) status() types.FederatedContentStatus {
	e.t.Helper()
	c, err := e.f.keeper.Content.Get(e.f.ctx, e.contentID)
	require.NoError(e.t, err)
	return c.Status
}

func (e *disputeEnv) verifierBond() string {
	br, _ := e.f.repKeeper.GetBondedRole(e.f.ctx, reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, e.verifier)
	return br.CurrentBond
}

func countEvents(ctx sdk.Context, typ string) int {
	n := 0
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type == typ {
			n++
		}
	}
	return n
}

// FED-25 (Kimi's TestKimiDoubleSettlement): an escalation before quorum
// closes arbitration. Further votes are refused, so the automatic verdict
// can never be re-armed, and the jury TIMEOUT is the one and only
// settlement: one half refund, escrow zeroed.
func TestEscalationClosesArbitration(t *testing.T) {
	e := newDispute(t, "d1", 3)
	e.verify(e.hash)
	e.challenge(e.f.ctx)
	_, err := e.ms.EscalateChallenge(e.f.ctx, &types.MsgEscalateChallenge{Creator: e.challenger, ContentId: e.contentID})
	require.NoError(t, err)

	for _, a := range e.arbs {
		require.ErrorIs(t, e.vote(e.f.ctx, a, e.wrong), types.ErrArbitrationClosed)
	}
	require.Equal(t, types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_UNSPECIFIED, e.record().PendingVerifierVerdict)

	// Past the arbiter windows: nothing settles while the jury is open.
	ctx := e.endBlock(e.params.ArbiterResolutionWindow + e.params.ArbiterEscalationWindow + time.Second)
	require.Zero(t, countEvents(ctx, types.EventTypeChallengeUpheld))
	require.Zero(t, countEvents(ctx, types.EventTypeChallengeTimeout))
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED, e.status())

	// The jury deadline settles it, exactly once.
	sends := e.f.bankKeeper.moduleSends
	ctx = e.endBlock(e.params.ChallengeJuryDeadline + time.Second)
	require.Equal(t, 1, countEvents(ctx, types.EventTypeChallengeTimeout))
	require.Zero(t, countEvents(ctx, types.EventTypeChallengeUpheld))
	require.Equal(t, sends+1, e.f.bankKeeper.moduleSends, "one half refund to the challenger")
	require.True(t, e.record().EscrowedChallengeFee.IsZero(), "escrow zeroed once disbursed")
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, e.status())

	ctx = e.endBlock(e.params.ChallengeJuryDeadline + 30*24*time.Hour)
	require.Zero(t, countEvents(ctx, types.EventTypeChallengeTimeout))
	require.Zero(t, countEvents(ctx, types.EventTypeChallengeUpheld))
	require.Equal(t, sends+1, e.f.bankKeeper.moduleSends)
}

// FED-25 (Kimi's TestKimiDoubleSlash): the Operations Committee verdict is
// the only settlement of an escalated dispute. The verifier is slashed
// once, a second verdict is refused, and no automatic verdict follows.
func TestEscalatedVerdictSlashesOnce(t *testing.T) {
	e := newDispute(t, "d2", 3)
	e.verify(e.hash)
	e.challenge(e.f.ctx)
	_, err := e.ms.EscalateChallenge(e.f.ctx, &types.MsgEscalateChallenge{Creator: e.challenger, ContentId: e.contentID})
	require.NoError(t, err)
	for _, a := range e.arbs {
		require.Error(t, e.vote(e.f.ctx, a, e.wrong))
	}
	before := e.verifierBond()

	ctx := e.at(time.Hour)
	_, err = e.ms.ResolveEscalatedChallenge(ctx, &types.MsgResolveEscalatedChallenge{
		Authority: e.f.authority, ContentId: e.contentID, Verdict: types.JuryVerdict_JURY_VERDICT_CHALLENGE_UPHELD,
	})
	require.NoError(t, err)
	require.Equal(t, 1, countEvents(ctx, types.EventTypeChallengeUpheld))
	afterFirst := e.verifierBond()
	require.NotEqual(t, before, afterFirst, "slashed")
	require.True(t, e.record().EscrowedChallengeFee.IsZero())

	_, err = e.ms.ResolveEscalatedChallenge(e.at(2*time.Hour), &types.MsgResolveEscalatedChallenge{
		Authority: e.f.authority, ContentId: e.contentID, Verdict: types.JuryVerdict_JURY_VERDICT_CHALLENGE_UPHELD,
	})
	require.ErrorIs(t, err, types.ErrEscalatedChallengeNotFound)

	ctx = e.endBlock(60 * 24 * time.Hour)
	require.Zero(t, countEvents(ctx, types.EventTypeChallengeUpheld))
	require.Equal(t, afterFirst, e.verifierBond(), "never slashed a second time")
}

// FED-25 (Kimi's TestKimiDoubleSettlementPostQuorum): quorum, then
// escalation. A later vote can no longer re-stash the verdict, the
// stale escalation-queue entry applies nothing, and the jury settles once.
func TestPostQuorumEscalationCannotBeRestashed(t *testing.T) {
	e := newDispute(t, "d3", 4)
	e.verify(e.hash)
	e.challenge(e.f.ctx)
	for _, a := range e.arbs[:3] {
		require.NoError(t, e.vote(e.f.ctx, a, e.hash))
	}
	require.Equal(t, types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_VERIFIER_RIGHT, e.record().PendingVerifierVerdict)

	_, err := e.ms.EscalateChallenge(e.f.ctx, &types.MsgEscalateChallenge{Creator: e.challenger, ContentId: e.contentID})
	require.NoError(t, err)
	require.ErrorIs(t, e.vote(e.f.ctx, e.arbs[3], e.hash), types.ErrArbitrationClosed)
	require.Equal(t, types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_UNSPECIFIED, e.record().PendingVerifierVerdict)

	ctx := e.endBlock(e.params.ArbiterEscalationWindow + time.Second)
	require.Zero(t, countEvents(ctx, types.EventTypeChallengeRejected), "no auto-verdict on an escalated dispute")
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED, e.status())

	ctx = e.at(e.params.ArbiterEscalationWindow + time.Hour)
	_, err = e.ms.ResolveEscalatedChallenge(ctx, &types.MsgResolveEscalatedChallenge{
		Authority: e.f.authority, ContentId: e.contentID, Verdict: types.JuryVerdict_JURY_VERDICT_CHALLENGE_REJECTED,
	})
	require.NoError(t, err)
	require.Equal(t, 1, countEvents(ctx, types.EventTypeChallengeRejected))
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, e.status())
	n := 0
	require.NoError(t, e.f.keeper.ArbiterEscalationQueue.Walk(e.f.ctx, nil, func(collections.Pair[int64, uint64]) (bool, error) { n++; return false, nil }))
	require.Zero(t, n, "the jury drops the round's escalation-queue entry")
}

// FED-25: the automatic verdict settles once; afterwards there is nothing
// left to escalate or resolve.
func TestAutoVerdictSettlesOnce(t *testing.T) {
	e := newDispute(t, "d4", 3)
	e.verify(e.hash)
	e.challenge(e.f.ctx)
	for _, a := range e.arbs {
		require.NoError(t, e.vote(e.f.ctx, a, e.wrong))
	}
	ctx := e.endBlock(e.params.ArbiterEscalationWindow + time.Second)
	require.Equal(t, 1, countEvents(ctx, types.EventTypeChallengeUpheld))
	require.True(t, e.record().EscrowedChallengeFee.IsZero())
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_REJECTED, e.status())

	_, err := e.ms.EscalateChallenge(e.at(e.params.ArbiterEscalationWindow+2*time.Second), &types.MsgEscalateChallenge{Creator: e.challenger, ContentId: e.contentID})
	require.Error(t, err)
	ctx = e.endBlock(30 * 24 * time.Hour)
	require.Zero(t, countEvents(ctx, types.EventTypeChallengeUpheld))
}

// FED-25: the module account must cover every open escrow.
func TestEscrowSolvencyInvariant(t *testing.T) {
	f := initFixture(t)
	inv := keeper.EscrowSolvencyInvariant(f.keeper)
	require.NoError(t, f.keeper.VerificationRecords.Set(f.ctx, 7, types.VerificationRecord{ContentId: 7, EscrowedChallengeFee: math.NewInt(300)}))
	require.NoError(t, f.keeper.EscalatedChallenges.Set(f.ctx, 7, types.EscalatedChallenge{ContentId: 7, EscrowedEscalationFee: math.NewInt(200)}))
	moduleAddr := f.authKeeper.GetModuleAddress(types.ModuleName).String()
	f.bankKeeper.balances[moduleAddr] = sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(499)))
	_, broken := inv(f.sdkCtx())
	require.True(t, broken)
	f.bankKeeper.balances[moduleAddr] = sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(500)))
	_, broken = inv(f.sdkCtx())
	require.False(t, broken)
}

// FED-26: no vote after arbiter_resolution_deadline.
func TestArbiterVoteAfterResolutionDeadline(t *testing.T) {
	e := newDispute(t, "d5", 3)
	e.verify(e.hash)
	e.challenge(e.f.ctx)
	require.Equal(t, e.base.Unix()+int64(e.params.ArbiterResolutionWindow.Seconds()), e.record().ArbiterResolutionDeadline)
	require.NoError(t, e.vote(e.at(e.params.ArbiterResolutionWindow), e.arbs[0], e.wrong))
	require.ErrorIs(t, e.vote(e.at(e.params.ArbiterResolutionWindow+time.Second), e.arbs[1], e.wrong), types.ErrArbitrationClosed)
}

// FED-26: the first quorum is final. Neither a competing quorum nor a
// same-hash vote above quorum can replace the verdict or push its
// escalation deadline back.
func TestFirstArbiterQuorumIsFinal(t *testing.T) {
	e := newDispute(t, "d6", 5)
	e.verify(e.hash)
	e.challenge(e.f.ctx)
	for _, a := range e.arbs[:3] {
		require.NoError(t, e.vote(e.f.ctx, a, e.hash))
	}
	rec := e.record()
	require.Equal(t, types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_VERIFIER_RIGHT, rec.PendingVerifierVerdict)

	later := e.at(time.Hour)
	require.ErrorIs(t, e.vote(later, e.arbs[3], e.wrong), types.ErrArbitrationClosed, "competing hash")
	require.ErrorIs(t, e.vote(later, e.arbs[4], e.hash), types.ErrArbitrationClosed, "same hash above quorum")
	after := e.record()
	require.Equal(t, rec.PendingVerifierVerdict, after.PendingVerifierVerdict)
	require.Equal(t, rec.ArbiterEscalationDeadline, after.ArbiterEscalationDeadline)
	n := 0
	require.NoError(t, e.f.keeper.ArbiterEscalationQueue.Walk(e.f.ctx, nil, func(collections.Pair[int64, uint64]) (bool, error) { n++; return false, nil }))
	require.Equal(t, 1, n)
}

// FED-26: a resolution-queue entry left over from an earlier round of the
// same content does not settle the current round.
func TestStaleResolutionEntryDoesNotSettleNewRound(t *testing.T) {
	e := newDispute(t, "d7", 3)
	e.verify(e.hash)
	e.challenge(e.f.ctx)
	_, err := e.ms.EscalateChallenge(e.f.ctx, &types.MsgEscalateChallenge{Creator: e.challenger, ContentId: e.contentID})
	require.NoError(t, err)
	_, err = e.ms.ResolveEscalatedChallenge(e.at(time.Hour), &types.MsgResolveEscalatedChallenge{
		Authority: e.f.authority, ContentId: e.contentID, Verdict: types.JuryVerdict_JURY_VERDICT_CHALLENGE_REJECTED,
	})
	require.NoError(t, err)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, e.status())

	// Round two starts two hours in.
	e.challenge(e.at(2 * time.Hour))

	// Round one's resolution deadline passes: round two is untouched.
	ctx := e.endBlock(e.params.ArbiterResolutionWindow + time.Second)
	require.Zero(t, countEvents(ctx, types.EventTypeArbiterResolutionExpired))
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED, e.status())
	require.NoError(t, e.vote(e.at(e.params.ArbiterResolutionWindow+time.Minute), e.arbs[0], e.wrong))

	// Round two's own deadline settles it (no quorum).
	ctx = e.endBlock(2*time.Hour + e.params.ArbiterResolutionWindow + time.Second)
	require.Equal(t, 1, countEvents(ctx, types.EventTypeArbiterResolutionExpired))
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, e.status())
}

// FED-26: a new round waits until the previous round's arbiter data is
// gone, so old votes never count in it.
func TestNewRoundWaitsForArbiterCleanup(t *testing.T) {
	e := newDispute(t, "d8", 3)
	e.verify(e.hash)
	e.challenge(e.f.ctx)
	require.NoError(t, e.vote(e.f.ctx, e.arbs[0], e.wrong))
	_, err := e.ms.EscalateChallenge(e.f.ctx, &types.MsgEscalateChallenge{Creator: e.challenger, ContentId: e.contentID})
	require.NoError(t, err)
	_, err = e.ms.ResolveEscalatedChallenge(e.at(time.Hour), &types.MsgResolveEscalatedChallenge{
		Authority: e.f.authority, ContentId: e.contentID, Verdict: types.JuryVerdict_JURY_VERDICT_CHALLENGE_REJECTED,
	})
	require.NoError(t, err)

	_, err = e.ms.ChallengeVerification(e.at(2*time.Hour), &types.MsgChallengeVerification{Creator: e.challenger, ContentId: e.contentID, ContentHash: e.wrong})
	require.ErrorIs(t, err, types.ErrDisputeInProgress)

	e.endBlock(e.params.ArbiterResolutionWindow + time.Second)
	e.challenge(e.at(e.params.ArbiterResolutionWindow + 2*time.Second))
}

// FED-34: arbiter-data cleanup is charged to max_prune_per_block and
// resumes across blocks; the settlement itself happens once.
func TestArbiterCleanupIsBudgeted(t *testing.T) {
	e := newDispute(t, "d9", 3)
	e.verify(e.hash)
	e.challenge(e.f.ctx)
	// Two votes on two hashes: 2 submissions + 2 tallies, no quorum.
	require.NoError(t, e.vote(e.f.ctx, e.arbs[0], e.wrong))
	require.NoError(t, e.vote(e.f.ctx, e.arbs[1], e.hash))
	params := e.params
	params.MaxPrunePerBlock = 2
	require.NoError(t, e.f.keeper.Params.Set(e.f.ctx, params))

	expired, blocks := 0, 0
	for d := e.params.ArbiterResolutionWindow + time.Second; blocks < 10; d += time.Second {
		ctx := e.endBlock(d)
		blocks++
		expired += countEvents(ctx, types.EventTypeArbiterResolutionExpired)
		n := 0
		require.NoError(t, e.f.keeper.ArbiterResolutionQueue.Walk(e.f.ctx, nil, func(collections.Pair[int64, uint64]) (bool, error) { n++; return false, nil }))
		if n == 0 {
			break
		}
	}
	require.Greater(t, blocks, 1, "a budget of 2 cannot clear 4 entries in one block")
	require.Equal(t, 1, expired, "settled exactly once")
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, e.status())
}

// FED-27: require_review lands IBC content HIDDEN.
func TestRequireReviewIBCContent(t *testing.T) {
	f := initFixture(t)
	ibcPeer(t, f, "phoenix-1", "channel-7")
	require.NoError(t, f.keeper.OnRecvContentPacket(f.ctx, types.PortID, "channel-7", contentPacket(1)))
	c, err := f.keeper.Content.Get(f.ctx, 0)
	require.NoError(t, err)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_ACTIVE, c.Status)

	policy, err := f.keeper.PeerPolicies.Get(f.ctx, "phoenix-1")
	require.NoError(t, err)
	policy.RequireReview = true
	require.NoError(t, f.keeper.PeerPolicies.Set(f.ctx, "phoenix-1", policy))
	require.NoError(t, f.keeper.OnRecvContentPacket(f.ctx, types.PortID, "channel-7", contentPacket(2)))
	c, err = f.keeper.Content.Get(f.ctx, 1)
	require.NoError(t, err)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN, c.Status)
	require.True(t, hasEvent(f.sdkCtx(), types.EventTypeContentAwaitingReview, map[string]string{types.AttributeKeyContentID: "1"}))

	// The review: an Operations Committee restore publishes it.
	ms := keeper.NewMsgServerImpl(f.keeper)
	_, err = ms.ModerateContent(f.ctx, &types.MsgModerateContent{
		Authority: f.authority, ContentId: 1, NewStatus: types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_ACTIVE,
	})
	require.NoError(t, err)
	c, _ = f.keeper.Content.Get(f.ctx, 1)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_ACTIVE, c.Status)
}

// FED-27: require_review lands verified bridge content HIDDEN; the review
// restores it to VERIFIED because its verification stands.
func TestRequireReviewBridgeContent(t *testing.T) {
	e := newDispute(t, "r1", 0)
	policy, err := e.f.keeper.PeerPolicies.Get(e.f.ctx, e.peerID)
	require.NoError(t, err)
	policy.RequireReview = true
	require.NoError(t, e.f.keeper.PeerPolicies.Set(e.f.ctx, e.peerID, policy))

	e.verify(e.hash)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN, e.status())
	_, err = e.ms.ModerateContent(e.f.ctx, &types.MsgModerateContent{
		Authority: e.f.authority, ContentId: e.contentID, NewStatus: types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_ACTIVE,
	})
	require.NoError(t, err)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, e.status())
}

// activeServiceKeeper wires a mock x/service whose operators are all ACTIVE.
func activeServiceKeeper(t *testing.T, f *fixture) *mockServiceKeeper {
	sk := wireServiceKeeper(t, f)
	sk.GetOperatorFn = func(context.Context, []byte, string) (servicetypes.Operator, bool) {
		return servicetypes.Operator{Status: servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE}, true
	}
	return sk
}

// FED-29: escalating files no report against the submitting operator,
// whose hash may well have been confirmed.
func TestEscalationFilesNoOperatorReport(t *testing.T) {
	e := newDispute(t, "s1", 0)
	sk := activeServiceKeeper(t, e.f)
	e.verify(e.hash)
	e.challenge(e.f.ctx)
	_, err := e.ms.EscalateChallenge(e.f.ctx, &types.MsgEscalateChallenge{Creator: e.challenger, ContentId: e.contentID})
	require.NoError(t, err)
	require.Empty(t, sk.ReportCalls)

	// A jury REJECTED verdict on CHALLENGED content confirms the operator.
	_, err = e.ms.ResolveEscalatedChallenge(e.at(time.Hour), &types.MsgResolveEscalatedChallenge{
		Authority: e.f.authority, ContentId: e.contentID, Verdict: types.JuryVerdict_JURY_VERDICT_CHALLENGE_REJECTED,
	})
	require.NoError(t, err)
	require.Empty(t, sk.ReportCalls)
}

// FED-29: a report x/service refuses under its per-caller rate limit is
// queued and re-filed with the same dedupe key, not dropped.
func TestRateLimitedOperatorReportIsRetried(t *testing.T) {
	e := newDispute(t, "s2", 3)
	sk := activeServiceKeeper(t, e.f)
	limited := true
	sk.OpenSystemReportFn = func(context.Context, sdk.AccAddress, sdk.AccAddress, string, uint32, string, []byte) (uint64, bool, error) {
		if limited {
			return 0, false, servicetypes.ErrSystemReportRateLimited
		}
		return 77, false, nil
	}
	e.verify(e.hash)
	e.challenge(e.f.ctx)
	for _, a := range e.arbs {
		require.NoError(t, e.vote(e.f.ctx, a, e.wrong)) // quorum hash differs from the content hash
	}
	rec := e.record()
	require.NotEmpty(t, rec.PendingChallengeReport)
	has, _ := e.f.keeper.PendingChallengeReports.Has(e.f.ctx, e.contentID)
	require.True(t, has)

	e.endBlock(time.Minute) // still limited
	require.NotEmpty(t, e.record().PendingChallengeReport)

	limited = false
	ctx := e.endBlock(2 * time.Minute)
	require.Equal(t, 1, countEvents(ctx, types.EventTypeChallengeReportFiled))
	rec = e.record()
	require.Empty(t, rec.PendingChallengeReport)
	require.Equal(t, uint64(77), rec.ChallengeReportId)
	has, _ = e.f.keeper.PendingChallengeReports.Has(e.f.ctx, e.contentID)
	require.False(t, has)
	require.Len(t, sk.ReportCalls, 3)
	require.Equal(t, sk.ReportCalls[0].DedupeKey, sk.ReportCalls[2].DedupeKey, "retries reuse the dedupe key")
}

// FED-29: on DISPUTED content the verdict blames the party it found wrong.
// Verifier right means the operator's hash was false (REJECTED, operator
// counted and reported); verifier wrong means the operator's hash stands
// (VERIFIED, operator untouched).
func TestDisputedVerdictBlamesTheRightParty(t *testing.T) {
	for _, tc := range []struct {
		name     string
		verdict  types.JuryVerdict
		status   types.FederatedContentStatus
		rejected uint64
		reports  int
	}{
		{"verifier right", types.JuryVerdict_JURY_VERDICT_CHALLENGE_REJECTED, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_REJECTED, 1, 1},
		{"verifier wrong", types.JuryVerdict_JURY_VERDICT_CHALLENGE_UPHELD, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newDispute(t, "s3", 0)
			sk := activeServiceKeeper(t, e.f)
			e.verify(e.wrong) // mismatch → DISPUTED
			require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_DISPUTED, e.status())
			_, err := e.ms.EscalateChallenge(e.f.ctx, &types.MsgEscalateChallenge{Creator: e.verifier, ContentId: e.contentID})
			require.NoError(t, err)
			_, err = e.ms.ResolveEscalatedChallenge(e.at(time.Hour), &types.MsgResolveEscalatedChallenge{
				Authority: e.f.authority, ContentId: e.contentID, Verdict: tc.verdict,
			})
			require.NoError(t, err)
			require.Equal(t, tc.status, e.status())
			b, err := e.f.keeper.BridgeBindings.Get(e.f.ctx, collections.Join(e.operator, e.peerID))
			require.NoError(t, err)
			require.Equal(t, tc.rejected, b.ContentRejected)
			require.Len(t, sk.ReportCalls, tc.reports)
		})
	}
}

// FED-29: the jury tx fails (and rolls back) on a failed side effect
// instead of persisting a "resolved" record with nothing disbursed. The
// EndBlocker TIMEOUT runs in a cache context too: a failure commits
// nothing (escrow kept), is queued with backoff and settles on a later
// retry once the cause is gone.
func TestSettlementFailureHandling(t *testing.T) {
	e := newDispute(t, "s4", 0)
	e.verify(e.hash)
	e.challenge(e.f.ctx)
	_, err := e.ms.EscalateChallenge(e.f.ctx, &types.MsgEscalateChallenge{Creator: e.challenger, ContentId: e.contentID})
	require.NoError(t, err)

	e.f.bankKeeper.failModuleSends = true
	cc, _ := e.at(time.Hour).CacheContext() // a tx: discarded on error
	_, err = e.ms.ResolveEscalatedChallenge(cc, &types.MsgResolveEscalatedChallenge{
		Authority: e.f.authority, ContentId: e.contentID, Verdict: types.JuryVerdict_JURY_VERDICT_CHALLENGE_UPHELD,
	})
	require.ErrorIs(t, err, types.ErrSettlementFailed)
	has, _ := e.f.keeper.EscalatedChallenges.Has(e.f.ctx, e.contentID)
	require.True(t, has, "the escalation is still open for a retry")
	escrow := e.record().EscrowedChallengeFee
	require.True(t, escrow.IsPositive())

	// The EndBlocker TIMEOUT fails: nothing commits, the retry is queued.
	deadline := e.params.ChallengeJuryDeadline + time.Second
	ctx := e.endBlock(deadline)
	require.Equal(t, 1, countEvents(ctx, types.EventTypeSettlementFailure))
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED, e.status())
	has, _ = e.f.keeper.EscalatedChallenges.Has(e.f.ctx, e.contentID)
	require.True(t, has, "nothing committed")
	r := e.record()
	require.True(t, r.EscrowedChallengeFee.Equal(escrow), "escrow kept on the record")
	require.Equal(t, types.SettlementKind_SETTLEMENT_KIND_JURY_TIMEOUT, r.SettlementRetryKind)
	require.Equal(t, uint32(1), r.SettlementAttempts)
	require.Equal(t, ctx.BlockTime().Unix()+keeper.SettlementRetryBaseSeconds, r.SettlementRetryAt)
	queued, _ := e.f.keeper.SettlementRetryQueue.Has(e.f.ctx, collections.Join(r.SettlementRetryAt, e.contentID))
	require.True(t, queued)
	deadlineEntries := 0
	require.NoError(t, e.f.keeper.EscalatedChallengeDeadline.Walk(e.f.ctx, nil, func(collections.Pair[int64, uint64]) (bool, error) {
		deadlineEntries++
		return false, nil
	}))
	require.Zero(t, deadlineEntries, "the retry queue owns the TIMEOUT; no per-block re-run")

	// Not due yet: nothing happens.
	ctx = e.endBlock(deadline + time.Minute)
	require.Zero(t, countEvents(ctx, types.EventTypeSettlementFailure))
	require.Equal(t, uint32(1), e.record().SettlementAttempts)

	// The cause is gone: the due retry settles it.
	e.f.bankKeeper.failModuleSends = false
	e.endBlock(deadline + time.Duration(keeper.SettlementRetryBaseSeconds)*time.Second)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, e.status())
	has, _ = e.f.keeper.EscalatedChallenges.Has(e.f.ctx, e.contentID)
	require.False(t, has)
	r = e.record()
	require.True(t, r.EscrowedChallengeFee.IsZero())
	require.Zero(t, r.SettlementAttempts)
	require.Zero(t, r.SettlementRetryAt)
	require.Equal(t, types.SettlementKind_SETTLEMENT_KIND_UNSPECIFIED, r.SettlementRetryKind)
}

// FED-29: a settlement that keeps failing is retried a bounded number of
// times with backoff, then leaves the queue with its escrow still on the
// record; the committee can still settle the escalation.
func TestSettlementRetryExhaustsAttempts(t *testing.T) {
	e := newDispute(t, "s5", 0)
	e.verify(e.hash)
	e.challenge(e.f.ctx)
	_, err := e.ms.EscalateChallenge(e.f.ctx, &types.MsgEscalateChallenge{Creator: e.challenger, ContentId: e.contentID})
	require.NoError(t, err)
	escrow := e.record().EscrowedChallengeFee
	e.f.bankKeeper.failModuleSends = true

	at := e.params.ChallengeJuryDeadline + time.Second
	var abandoned int
	var prevGap int64
	for i := 0; i < keeper.MaxSettlementAttempts; i++ {
		ctx := e.endBlock(at)
		abandoned += countEvents(ctx, types.EventTypeSettlementAbandoned)
		r := e.record()
		require.Equal(t, uint32(i+1), r.SettlementAttempts)
		if r.SettlementRetryAt == 0 {
			break
		}
		gap := r.SettlementRetryAt - ctx.BlockTime().Unix()
		require.Greater(t, gap, prevGap, "backoff grows")
		prevGap = gap
		at = time.Duration(r.SettlementRetryAt-e.base.Unix()) * time.Second
	}
	r := e.record()
	require.Equal(t, uint32(keeper.MaxSettlementAttempts), r.SettlementAttempts)
	require.Zero(t, r.SettlementRetryAt, "out of the retry queue")
	require.Equal(t, 1, abandoned)
	require.True(t, r.EscrowedChallengeFee.Equal(escrow), "the escrow stays accounted for")
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED, e.status())
	queued := 0
	require.NoError(t, e.f.keeper.SettlementRetryQueue.Walk(e.f.ctx, nil, func(collections.Pair[int64, uint64]) (bool, error) {
		queued++
		return false, nil
	}))
	require.Zero(t, queued)

	// Later blocks leave it alone.
	ctx := e.endBlock(at + 30*24*time.Hour)
	require.Zero(t, countEvents(ctx, types.EventTypeSettlementFailure))

	// The committee settles it once sends work again.
	e.f.bankKeeper.failModuleSends = false
	_, err = e.ms.ResolveEscalatedChallenge(e.at(at+31*24*time.Hour), &types.MsgResolveEscalatedChallenge{
		Authority: e.f.authority, ContentId: e.contentID, Verdict: types.JuryVerdict_JURY_VERDICT_CHALLENGE_TIMEOUT,
	})
	require.NoError(t, err)
	r = e.record()
	require.True(t, r.EscrowedChallengeFee.IsZero())
	require.Zero(t, r.SettlementAttempts)
}

// FED-29: the no-quorum expiry (Phase 7) is retried the same way.
func TestNoQuorumSettlementIsRetried(t *testing.T) {
	e := newDispute(t, "s6", 0)
	e.verify(e.hash)
	e.challenge(e.f.ctx)
	escrow := e.record().EscrowedChallengeFee
	require.True(t, escrow.IsPositive())

	e.f.bankKeeper.failModuleSends = true
	at := e.params.ArbiterResolutionWindow + time.Second
	ctx := e.endBlock(at)
	require.Equal(t, 1, countEvents(ctx, types.EventTypeSettlementFailure))
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED, e.status())
	r := e.record()
	require.True(t, r.EscrowedChallengeFee.Equal(escrow))
	require.False(t, r.CommitmentReleased, "the commitment release rolled back with the rest")
	require.Equal(t, types.SettlementKind_SETTLEMENT_KIND_NO_QUORUM, r.SettlementRetryKind)

	e.f.bankKeeper.failModuleSends = false
	e.endBlock(at + time.Duration(keeper.SettlementRetryBaseSeconds)*time.Second)
	require.Equal(t, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, e.status())
	r = e.record()
	require.True(t, r.EscrowedChallengeFee.IsZero())
	require.True(t, r.CommitmentReleased)
	require.Zero(t, r.SettlementRetryAt)
}

// FED-29: the retry queue is rebuilt from the records at InitGenesis.
func TestSettlementRetryQueueGenesis(t *testing.T) {
	e := newDispute(t, "s7", 0)
	e.verify(e.hash)
	e.challenge(e.f.ctx)
	e.f.bankKeeper.failModuleSends = true
	e.endBlock(e.params.ArbiterResolutionWindow + time.Second)
	r := e.record()
	require.NotZero(t, r.SettlementRetryAt)

	gs, err := e.f.keeper.ExportGenesis(e.f.ctx)
	require.NoError(t, err)
	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *gs))
	has, err := g.keeper.SettlementRetryQueue.Has(g.ctx, collections.Join(r.SettlementRetryAt, e.contentID))
	require.NoError(t, err)
	require.True(t, has)
}

// FED-31 and FED-32: a reputation ack is dropped once the policy stops
// accepting attestations, and a stored one keeps the peer's per-tag scores,
// validated and re-capped on this side.
func TestReputationAckPolicyAndTags(t *testing.T) {
	f := initFixture(t)
	ibcPeer(t, f, "phoenix-1", "channel-7")
	user := testAddr(t, f, "rep-requester")
	require.NoError(t, f.keeper.IdentityLinks.Set(f.ctx, collections.Join(user, "phoenix-1"), types.IdentityLink{
		LocalAddress: user, PeerId: "phoenix-1", RemoteIdentity: "phoenix1me", Status: types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED,
	}))
	q := &types.ReputationQueryPacket{Requester: user, QueriedAddress: "phoenix1me"}
	tags := []types.TagReputation{
		{Tag: "go", Score: math.NewInt(5)},
		{Tag: "Bad Tag", Score: math.NewInt(9)},
		{Tag: "go", Score: math.NewInt(99)},
		{Tag: "art", Score: math.NewInt(-1)},
	}
	for i := 0; i < 40; i++ {
		tags = append(tags, types.TagReputation{Tag: fmt.Sprintf("t%02d", i), Score: math.NewInt(1)})
	}
	ack, _ := (&types.ReputationResponseData{Address: "phoenix1me", TrustLevel: 3, IsActive: true, Reputations: tags}).Marshal()

	policy, err := f.keeper.PeerPolicies.Get(f.ctx, "phoenix-1")
	require.NoError(t, err)
	policy.AcceptReputationAttestations = false
	require.NoError(t, f.keeper.PeerPolicies.Set(f.ctx, "phoenix-1", policy))
	require.NoError(t, f.keeper.OnAckReputationQuery(f.ctx, "channel-7", q, ack))
	has, _ := f.keeper.RepAttestations.Has(f.ctx, collections.Join(user, "phoenix-1"))
	require.False(t, has, "kill switch applies to acks in flight")
	require.True(t, hasEvent(f.sdkCtx(), types.EventTypeAckDropped, nil))

	policy.AcceptReputationAttestations = true
	require.NoError(t, f.keeper.PeerPolicies.Set(f.ctx, "phoenix-1", policy))
	require.NoError(t, f.keeper.OnAckReputationQuery(f.ctx, "channel-7", q, ack))
	att, err := f.keeper.RepAttestations.Get(f.ctx, collections.Join(user, "phoenix-1"))
	require.NoError(t, err)
	require.Len(t, att.RemoteReputations, types.MaxReputationResponseTags)
	require.Equal(t, "go", att.RemoteReputations[0].Tag, "sorted; the malformed and negative entries are gone")
	require.Equal(t, math.NewInt(5), att.RemoteReputations[0].Score, "first entry for a tag wins")
	for i := 1; i < len(att.RemoteReputations); i++ {
		require.Less(t, att.RemoteReputations[i-1].Tag, att.RemoteReputations[i].Tag)
	}
	has, _ = f.keeper.RepAttestationsByPeer.Has(f.ctx, collections.Join("phoenix-1", user))
	require.True(t, has, "the peer index follows the attestation")
}

// FED-29: the arbiter auto-verdict on DISPUTED content. A quorum on the
// operator's hash vindicates the operator (VERIFIED, no report); a quorum on
// a hash neither side submitted means both were wrong (REJECTED, operator
// counted, verifier slashed).
func TestDisputedAutoVerdictContentOutcome(t *testing.T) {
	for _, tc := range []struct {
		name     string
		quorum   func(e *disputeEnv) []byte
		status   types.FederatedContentStatus
		rejected uint64
	}{
		{"quorum on the operator's hash", func(e *disputeEnv) []byte { return e.hash }, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, 0},
		{"quorum on a third hash", func(e *disputeEnv) []byte { h := sha256.Sum256([]byte("third")); return h[:] }, types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_REJECTED, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newDispute(t, "s5", 3)
			e.verify(e.wrong) // DISPUTED
			before := e.verifierBond()
			for _, a := range e.arbs {
				require.NoError(t, e.vote(e.f.ctx, a, tc.quorum(e)))
			}
			require.Equal(t, types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_VERIFIER_WRONG, e.record().PendingVerifierVerdict)
			e.endBlock(e.params.ArbiterEscalationWindow + time.Second)
			require.Equal(t, tc.status, e.status())
			require.NotEqual(t, before, e.verifierBond(), "the verifier was wrong either way")
			b, err := e.f.keeper.BridgeBindings.Get(e.f.ctx, collections.Join(e.operator, e.peerID))
			require.NoError(t, err)
			require.Equal(t, tc.rejected, b.ContentRejected)
		})
	}
}
