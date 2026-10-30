package simulation

import (
	"fmt"

	"cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	commonstypes "sparkdream/x/commons/types"
	"sparkdream/x/reveal/types"
)

// Approve, Reject and ResolveDispute are signed by the Commons Council
// policy address (or x/gov). The spec routes them as Commons Council
// proposals: an Operations Committee member submits the message as an
// x/commons proposal, the council votes, and the accepted proposal is
// executed, which runs the reveal handler as the policy. The simulation
// drives exactly that, one step per call, with real x/commons transactions:
//
//  1. execute an accepted reveal proposal of the kind whose min execution
//     period has passed (anyone may execute);
//  2. otherwise vote an open one through (council members vote YES until
//     the threshold accepts it early);
//  3. otherwise submit a new one for a target no other reveal proposal
//     covers, and vote it through.
//
// Each step is dry-run first; a proposal whose message the reveal handler
// would now refuse (the contribution moved on) is left to expire.

// commonsCouncilName is the council whose policy address IsCouncilPolicyOrGov
// accepts for "commons".
const commonsCouncilName = "Commons Council"

// opsCommitteeName is the committee whose members initiate reveal council
// proposals in the spec.
const opsCommitteeName = "Commons Operations Committee"

// councilKind is the reveal message a council proposal carries.
type councilKind int

const (
	kindApprove councilKind = iota
	kindReject
	kindResolve
)

// msgOf returns an empty message of the kind, for NoOp labels.
func (k councilKind) msgOf() sdk.Msg {
	switch k {
	case kindReject:
		return &types.MsgReject{}
	case kindResolve:
		return &types.MsgResolveDispute{}
	default:
		return &types.MsgApprove{}
	}
}

// councilProp is a Commons Council proposal carrying one reveal message.
type councilProp struct {
	p         commonstypes.Proposal
	kind      councilKind
	contribID uint64
	inner     sdk.Msg
}

// decodeReveal decodes a proposal message that is a reveal council message.
func decodeReveal(a *codectypes.Any) (councilKind, sdk.Msg, uint64, bool) {
	if a == nil {
		return 0, nil, 0, false
	}
	switch a.TypeUrl {
	case sdk.MsgTypeURL(&types.MsgApprove{}):
		m := &types.MsgApprove{}
		if m.Unmarshal(a.Value) != nil {
			return 0, nil, 0, false
		}
		return kindApprove, m, m.ContributionId, true
	case sdk.MsgTypeURL(&types.MsgReject{}):
		m := &types.MsgReject{}
		if m.Unmarshal(a.Value) != nil {
			return 0, nil, 0, false
		}
		return kindReject, m, m.ContributionId, true
	case sdk.MsgTypeURL(&types.MsgResolveDispute{}):
		m := &types.MsgResolveDispute{}
		if m.Unmarshal(a.Value) != nil {
			return 0, nil, 0, false
		}
		return kindResolve, m, m.ContributionId, true
	}
	return 0, nil, 0, false
}

// councilPolicy returns the Commons Council policy address.
func (e *simEnv) councilPolicy() (string, bool) {
	res, err := e.commonsQuery.GetGroup(e.ctx, &commonstypes.QueryGetGroupRequest{Index: commonsCouncilName})
	if err != nil || res.Group.PolicyAddress == "" {
		return "", false
	}
	// The policy must be a real council policy (it has a decision policy)
	// and never a sim account: x/name's simulation can overwrite the
	// council group with a sim account as its "policy", and signing as it
	// would bypass the council vote this path exists to exercise.
	if _, isAcc := getAccount(e.accs, res.Group.PolicyAddress); isAcc {
		return "", false
	}
	if _, err := e.commonsQuery.GetDecisionPolicy(e.ctx, &commonstypes.QueryGetDecisionPolicyRequest{PolicyAddress: res.Group.PolicyAddress}); err != nil {
		return "", false
	}
	return res.Group.PolicyAddress, true
}

// seated returns the live sim accounts seated on a commons group.
func (e *simEnv) seated(group string) []simtypes.Account {
	res, err := e.commonsQuery.GetCouncilMembers(e.ctx, &commonstypes.QueryGetCouncilMembersRequest{CouncilName: group})
	if err != nil {
		return nil
	}
	var out []simtypes.Account
	for _, m := range res.Members {
		if acc, ok := e.account(m.Address); ok {
			out = append(out, acc)
		}
	}
	return out
}

// proposalFee is the tx fee x/commons' ante handler requires of a
// MsgSubmitProposal carrying executable messages.
func (e *simEnv) proposalFee() (sdk.Coins, bool) {
	res, err := e.commonsQuery.Params(e.ctx, &commonstypes.QueryParamsRequest{})
	if err != nil {
		return nil, false
	}
	fee, err := sdk.ParseCoinsNormalized(res.Params.ProposalFee)
	return fee, err == nil
}

// revealProposals returns the open (SUBMITTED or ACCEPTED) Commons Council
// proposals that carry exactly one reveal message.
func (e *simEnv) revealProposals() ([]councilProp, error) {
	var out []councilProp
	var next []byte
	for {
		res, err := e.commonsQuery.ListProposals(e.ctx, &commonstypes.QueryListProposalsRequest{
			CouncilName: commonsCouncilName,
			Pagination:  &query.PageRequest{Key: next, Limit: 200},
		})
		if err != nil {
			return out, nil // no council (or no proposals): nothing open
		}
		for _, p := range res.Proposals {
			if p.Status != commonstypes.ProposalStatus_PROPOSAL_STATUS_SUBMITTED &&
				p.Status != commonstypes.ProposalStatus_PROPOSAL_STATUS_ACCEPTED {
				continue
			}
			if len(p.Messages) != 1 {
				continue
			}
			kind, inner, contribID, ok := decodeReveal(p.Messages[0])
			if !ok {
				continue
			}
			out = append(out, councilProp{p: p, kind: kind, contribID: contribID, inner: inner})
		}
		if res.Pagination == nil || len(res.Pagination.NextKey) == 0 {
			return out, nil
		}
		next = res.Pagination.NextKey
	}
}

// covered returns the contributions an open reveal proposal already targets.
func covered(props []councilProp) map[uint64]bool {
	out := map[uint64]bool{}
	for _, cp := range props {
		out[cp.contribID] = true
	}
	return out
}

// advanceCouncil moves a reveal council proposal of kind one step (see the
// file comment). delivered is false when nothing could be done; opMsg then
// carries the reason.
func (e *simEnv) advanceCouncil(kind councilKind) (simtypes.OperationMsg, bool, error) {
	label := kind.msgOf()
	policy, ok := e.councilPolicy()
	if !ok {
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(label), "no usable Commons Council policy (missing, or overwritten with a sim account)"), false, nil
	}
	props, err := e.revealProposals()
	if err != nil {
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(label), err.Error()), false, err
	}
	now := e.ctx.BlockTime().Unix()

	// 1. Execute an accepted proposal.
	for _, i := range e.r.Perm(len(props)) {
		cp := props[i]
		if cp.kind != kind || cp.p.Status != commonstypes.ProposalStatus_PROPOSAL_STATUS_ACCEPTED || now < cp.p.ExecutionTime {
			continue
		}
		executors := accountsWhere(e.accs, func(a simtypes.Account) bool { return !e.tombstoned(a.Address.String()) })
		acc, ok := pick(e.r, executors)
		if !ok {
			break
		}
		msg := &commonstypes.MsgExecuteProposal{Executor: acc.Address.String(), ProposalId: cp.p.Id}
		if e.dryRun(msg) != nil {
			continue // the reveal message no longer applies; let it expire
		}
		opMsg, _, err := e.deliverSmallFee(acc, msg)
		opMsg.Comment = "executes " + sdk.MsgTypeURL(cp.inner)
		return opMsg, err == nil && opMsg.OK, err
	}

	// 2. Vote an open proposal through.
	for _, i := range e.r.Perm(len(props)) {
		cp := props[i]
		if cp.kind != kind || cp.p.Status != commonstypes.ProposalStatus_PROPOSAL_STATUS_SUBMITTED || now > cp.p.VotingDeadline {
			continue
		}
		if e.dryRun(cp.inner) != nil {
			continue
		}
		opMsg, voted, err := e.voteThrough(cp.p.Id)
		opMsg.Comment = "votes for " + sdk.MsgTypeURL(cp.inner)
		if err != nil || voted {
			return opMsg, voted, err
		}
	}

	// 3. Submit a new proposal and vote it through.
	proposer, ok := e.councilProposer()
	if !ok {
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(label), "no council member can pay the proposal fee"), false, nil
	}
	inner, reason, err := e.newCouncilMsg(kind, policy, proposer.Address.String(), covered(props))
	if err != nil || reason != "" {
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(label), reason), false, err
	}
	if err := e.dryRun(inner); err != nil {
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(label), "handler would reject: "+err.Error()), false, nil
	}
	anyMsg, err := codectypes.NewAnyWithValue(inner)
	if err != nil {
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(label), err.Error()), false, err
	}
	fee, ok := e.proposalFee()
	if !ok {
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(label), "no x/commons proposal fee"), false, nil
	}
	submit := &commonstypes.MsgSubmitProposal{
		Proposer:      proposer.Address.String(),
		PolicyAddress: policy,
		Messages:      []*codectypes.Any{anyMsg},
		Metadata:      fmt.Sprintf("reveal: %s", sdk.MsgTypeURL(inner)),
	}
	if err := e.dryRun(submit); err != nil {
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(submit), "handler would reject: "+err.Error()), false, nil
	}
	// The proposal fee is enforced on the tx fee by the x/commons ante
	// handler, so this tx pays exactly the fee instead of a random one.
	opMsg, _, err := simulation.GenAndDeliverTx(e.input(proposer, submit, fee), fee)
	if err != nil || !opMsg.OK {
		return opMsg, false, err
	}
	if id, found := e.latestProposalBy(proposer.Address.String()); found {
		if voteMsg, voted, err := e.voteThrough(id); err != nil || voted {
			voteMsg.Comment = "submits and votes for " + sdk.MsgTypeURL(inner)
			return voteMsg, voted, err
		}
	}
	opMsg.Comment = "submits " + sdk.MsgTypeURL(inner)
	return opMsg, true, nil
}

// deliverSmallFee delivers a council vote or execution with a small random
// fee (at most 0.01 SPARK). The same few council seats sign every vote, and
// the random fee GenAndDeliverTxWithRandFees draws (any share of the
// balance) would drain them within a few proposals, leaving no seat able to
// pay the next proposal fee.
func (e *simEnv) deliverSmallFee(acc simtypes.Account, msg sdk.Msg) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	denom := sdk.DefaultBondDenom
	spendable := e.bk.SpendableCoins(e.ctx, acc.Address).AmountOf(denom)
	capAmt := math.MinInt(spendable, math.NewInt(10_000))
	fee := sdk.NewCoins()
	if capAmt.IsPositive() {
		amt, err := simtypes.RandPositiveInt(e.r, capAmt)
		if err == nil {
			fee = sdk.NewCoins(sdk.NewCoin(denom, amt))
		}
	}
	return simulation.GenAndDeliverTx(e.input(acc, msg, sdk.NewCoins()), fee)
}

// councilProposer returns a live Commons Council member who can pay the
// proposal fee and sits on the Operations Committee: the reveal msgs record
// the initiator, which must be an Operations Committee member.
func (e *simEnv) councilProposer() (simtypes.Account, bool) {
	fee, ok := e.proposalFee()
	if !ok {
		return simtypes.Account{}, false
	}
	canPay := func(a simtypes.Account) bool {
		return e.bk.SpendableCoins(e.ctx, a.Address).IsAllGTE(fee)
	}
	council := accountsWhere(e.seated(commonsCouncilName), canPay)
	ops := map[string]bool{}
	for _, a := range e.seated(opsCommitteeName) {
		ops[a.Address.String()] = true
	}
	return pick(e.r, accountsWhere(council, func(a simtypes.Account) bool { return ops[a.Address.String()] }))
}

// latestProposalBy returns the id of proposer's newest SUBMITTED Commons
// Council proposal (the one just submitted).
func (e *simEnv) latestProposalBy(proposer string) (uint64, bool) {
	var id uint64
	found := false
	var next []byte
	for {
		res, err := e.commonsQuery.ListProposals(e.ctx, &commonstypes.QueryListProposalsRequest{
			CouncilName: commonsCouncilName,
			Pagination:  &query.PageRequest{Key: next, Limit: 200},
		})
		if err != nil {
			return id, found
		}
		for _, p := range res.Proposals {
			if p.Proposer == proposer && p.Status == commonstypes.ProposalStatus_PROPOSAL_STATUS_SUBMITTED && (!found || p.Id > id) {
				id, found = p.Id, true
			}
		}
		if res.Pagination == nil || len(res.Pagination.NextKey) == 0 {
			return id, found
		}
		next = res.Pagination.NextKey
	}
}

// voteThrough has council members who have not voted yet vote YES until the
// proposal leaves SUBMITTED (the threshold accepts it early) or no member
// is left. It returns the last vote delivered.
func (e *simEnv) voteThrough(id uint64) (simtypes.OperationMsg, bool, error) {
	label := &commonstypes.MsgVoteProposal{}
	last := simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(label), "no council member left to vote")
	delivered := false
	members := e.seated(commonsCouncilName)
	for _, i := range e.r.Perm(len(members)) {
		res, err := e.commonsQuery.GetProposal(e.ctx, &commonstypes.QueryGetProposalRequest{ProposalId: id})
		if err != nil || res.Proposal.Status != commonstypes.ProposalStatus_PROPOSAL_STATUS_SUBMITTED {
			break
		}
		voter := members[i]
		if e.votedOn(id, voter.Address.String()) {
			continue
		}
		msg := &commonstypes.MsgVoteProposal{
			Voter:      voter.Address.String(),
			ProposalId: id,
			Option:     commonstypes.VoteOption_VOTE_OPTION_YES,
		}
		if e.dryRun(msg) != nil {
			continue
		}
		opMsg, _, err := e.deliverSmallFee(voter, msg)
		if err != nil {
			return opMsg, false, err
		}
		last, delivered = opMsg, delivered || opMsg.OK
	}
	return last, delivered, nil
}

// votedOn reports whether voter already voted on a commons proposal.
func (e *simEnv) votedOn(id uint64, voter string) bool {
	res, err := e.commonsQuery.GetProposalVotes(e.ctx, &commonstypes.QueryGetProposalVotesRequest{ProposalId: id})
	if err != nil {
		return true
	}
	for _, v := range res.Votes {
		if v.Voter == voter {
			return true
		}
	}
	return false
}

// newCouncilMsg builds the reveal message for a new council proposal of
// kind, for a target no open reveal proposal covers. A non-empty reason
// means there is none.
func (e *simEnv) newCouncilMsg(kind councilKind, policy, proposer string, taken map[uint64]bool) (sdk.Msg, string, error) {
	switch kind {
	case kindApprove, kindReject:
		proposed, err := e.contributionsWhere(func(c types.Contribution) bool {
			return c.Status == types.ContributionStatus_CONTRIBUTION_STATUS_PROPOSED && !taken[c.Id]
		})
		if err != nil {
			return nil, "", err
		}
		c, ok := pick(e.r, proposed)
		if !ok {
			if kind == kindReject {
				return nil, "no proposed contribution to reject", nil
			}
			// Approval needs something to approve: propose it first.
			if c, ok, err = e.ensureProposed(); err != nil || !ok {
				return nil, "no proposed contribution and none could be proposed", err
			}
		}
		if kind == kindReject {
			return &types.MsgReject{Authority: policy, Proposer: proposer, ContributionId: c.Id, Reason: randomReason(e.r)}, "", nil
		}
		return &types.MsgApprove{Authority: policy, Proposer: proposer, ContributionId: c.Id}, "", nil
	default:
		disputed, err := e.tranchesIn(types.TrancheStatus_TRANCHE_STATUS_DISPUTED, func(ref trancheRef) bool {
			return !taken[ref.c.Id] && e.height() < ref.t.VerificationDeadline+e.params.DisputeResolutionEpochs
		})
		if err != nil {
			return nil, "", err
		}
		ref, ok := pick(e.r, disputed)
		if !ok {
			return nil, "no disputed tranche (disputes only arise when a verification deadline tallies too few YES votes)", nil
		}
		return &types.MsgResolveDispute{
			Authority:      policy,
			Proposer:       proposer,
			ContributionId: ref.c.Id,
			TrancheId:      ref.t.Id,
			Verdict:        randomDisputeVerdict(e.r),
			Reason:         randomReason(e.r),
		}, "", nil
	}
}
