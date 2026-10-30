package simulation

import (
	"fmt"

	"cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/blog/types"
	commonstypes "sparkdream/x/commons/types"
)

// MsgUpdateOperationalParams is accepted from x/gov or the policy address
// of the Commons Council or its Operations Committee (an executed proposal,
// never a member's own signature). It reaches the handler the way the spec
// routes it: a seat on a body whose allowlist carries the message submits an
// x/commons proposal, the seats vote it through, and anyone executes it once
// accepted and its min execution period has passed. advance drives that one
// step per call, with real x/commons transactions:
//
//  1. execute an accepted proposal carrying the op's message type;
//  2. otherwise vote an open one through (seats vote YES until the decision
//     policy accepts it early);
//  3. otherwise build a new message, submit it to a body whose live sim
//     seats can pass it, vote it through, and execute it at once when the
//     body has no min execution period left.
//
// Every step is dry-run first; a proposal whose message the handler would
// now refuse is left to expire. This follows x/name/simulation/council.go,
// read through the x/commons queries instead of its keeper.

// resolverBodies are the x/commons groups whose policy
// MsgUpdateOperationalParams accepts (keeper isCouncilOrCommitteePolicy "commons"/"operations").
var resolverBodies = []string{"Commons Council", "Commons Operations Committee"}

// smallFeeCap bounds the fee of a vote or execution (0.01 SPARK): the same
// few seats sign every vote, and GenAndDeliverTxWithRandFees' fee (any
// share of the balance) would drain them within a few proposals.
const smallFeeCap = 10_000

// deliverSmallFee delivers msg from acc with a small random fee.
func (e *simEnv) deliverSmallFee(acc simtypes.Account, msg sdk.Msg) (simtypes.OperationMsg, error) {
	spendable := e.spendable(acc)
	capAmt := math.MinInt(spendable, math.NewInt(smallFeeCap))
	fee := sdk.NewCoins()
	if capAmt.IsPositive() {
		if amt, err := simtypes.RandPositiveInt(e.r, capAmt); err == nil {
			fee = sdk.NewCoins(sdk.NewCoin(e.denom, amt))
		}
	}
	opMsg, _, err := simulation.GenAndDeliverTx(e.input(acc, msg, math.ZeroInt()), fee)
	e.invalidate()
	return opMsg, err
}

// proposalFee is the tx fee the x/commons ante decorator requires of a
// proposal carrying executable messages.
func (e *simEnv) proposalFee() (sdk.Coins, error) {
	res, err := e.commonsQuery.Params(e.ctx, &commonstypes.QueryParamsRequest{})
	if err != nil {
		return nil, err
	}
	return sdk.ParseCoinsNormalized(res.Params.ProposalFee)
}

// body is a commons group with its policy and decision policy.
type body struct {
	name   string
	policy string
	g      commonstypes.Group
	dp     commonstypes.DecisionPolicy
}

// seat is a body member that is a live sim account.
type seat struct {
	acc    simtypes.Account
	weight math.LegacyDec
}

// resolveBody returns the named body when it is a usable group: it exists,
// its policy has a decision policy and is never a sim account (signing as a
// sim account "policy" would bypass the vote this path exists for).
func (e *simEnv) resolveBody(name string) (body, bool) {
	res, err := e.commonsQuery.GetGroup(e.ctx, &commonstypes.QueryGetGroupRequest{Index: name})
	if err != nil || res.Group.PolicyAddress == "" {
		return body{}, false
	}
	if _, isAcc := getAccountForAddress(res.Group.PolicyAddress, e.accs); isAcc {
		return body{}, false
	}
	dp, err := e.commonsQuery.GetDecisionPolicy(e.ctx, &commonstypes.QueryGetDecisionPolicyRequest{PolicyAddress: res.Group.PolicyAddress})
	if err != nil {
		return body{}, false
	}
	return body{name: name, policy: res.Group.PolicyAddress, g: res.Group, dp: dp.DecisionPolicy}, true
}

// seats returns a body's live sim seats and its total weight.
func (e *simEnv) seats(name string) ([]seat, math.LegacyDec) {
	total := math.LegacyZeroDec()
	res, err := e.commonsQuery.GetCouncilMembers(e.ctx, &commonstypes.QueryGetCouncilMembersRequest{CouncilName: name})
	if err != nil {
		return nil, total
	}
	var live []seat
	for _, m := range res.Members {
		w, err := math.LegacyNewDecFromStr(m.Weight)
		if err != nil {
			continue // the tally skips unparseable weights too
		}
		total = total.Add(w)
		if acc, ok := e.account(m.Address); ok {
			live = append(live, seat{acc: acc, weight: w})
		}
	}
	return live, total
}

// carrier checks that the body can carry a proposal with a message of
// typeURL (on its allowlist, a live term, open ballots, live sim seats that
// can pass it) and returns a seat who can pay the proposal fee. A non-empty
// reason means it cannot.
func (e *simEnv) carrier(b body, typeURL string, fee sdk.Coins) (simtypes.Account, string) {
	perms, err := e.commonsQuery.GetPolicyPermissions(e.ctx, &commonstypes.QueryGetPolicyPermissionsRequest{PolicyAddress: b.policy})
	if err != nil {
		return simtypes.Account{}, "policy has no allowlist"
	}
	allowed := false
	for _, m := range perms.PolicyPermissions.AllowedMessages {
		allowed = allowed || m == typeURL
	}
	if !allowed {
		return simtypes.Account{}, typeURL + " is not on " + b.name + "'s allowlist"
	}
	if b.g.CurrentTermExpiration > 0 && e.ctx.BlockTime().Unix() > b.g.CurrentTermExpiration {
		return simtypes.Account{}, b.name + "'s term has expired"
	}
	if b.g.BallotMode == commonstypes.BallotMode_BALLOT_MODE_SECRET {
		return simtypes.Account{}, b.name + " votes by secret ballot (x/shield only)"
	}
	live, total := e.seats(b.name)
	if !total.IsPositive() {
		return simtypes.Account{}, b.name + " has no weighted members"
	}
	yes := math.LegacyZeroDec()
	for _, s := range live {
		yes = yes.Add(s.weight)
	}
	threshold, err := math.LegacyNewDecFromStr(b.dp.Threshold)
	if err != nil {
		return simtypes.Account{}, b.name + " has an unparseable threshold"
	}
	passes := yes.GTE(threshold)
	if b.dp.PolicyType == "percentage" {
		passes = yes.Quo(total).GTE(threshold)
	}
	if !passes {
		return simtypes.Account{}, b.name + "'s live sim seats cannot pass it"
	}
	var payers []simtypes.Account
	for _, s := range live {
		if e.bk.SpendableCoins(e.ctx, s.acc.Address).IsAllGTE(fee) {
			payers = append(payers, s.acc)
		}
	}
	proposer, ok := pick(e.r, payers)
	if !ok {
		return simtypes.Account{}, "no seat of " + b.name + " can pay the proposal fee"
	}
	return proposer, ""
}

// openProp is an open (SUBMITTED or ACCEPTED) proposal carrying exactly one
// message of the op's type, decoded.
type openProp struct {
	p     commonstypes.Proposal
	inner sdk.Msg
}

// openProposals returns the open proposals of the resolver bodies that
// carry one message of typeURL, decoded by decode.
func (e *simEnv) openProposals(typeURL string, decode func(*codectypes.Any) (sdk.Msg, bool)) []openProp {
	var out []openProp
	for _, name := range resolverBodies {
		var next []byte
		for {
			res, err := e.commonsQuery.ListProposals(e.ctx, &commonstypes.QueryListProposalsRequest{
				CouncilName: name,
				Pagination:  &query.PageRequest{Key: next, Limit: 200},
			})
			if err != nil {
				break
			}
			for _, p := range res.Proposals {
				if p.Status != commonstypes.ProposalStatus_PROPOSAL_STATUS_SUBMITTED &&
					p.Status != commonstypes.ProposalStatus_PROPOSAL_STATUS_ACCEPTED {
					continue
				}
				if len(p.Messages) != 1 || p.Messages[0] == nil || p.Messages[0].TypeUrl != typeURL {
					continue
				}
				inner, ok := decode(p.Messages[0])
				if !ok {
					continue // an undecodable proposal is left to expire
				}
				out = append(out, openProp{p: p, inner: inner})
			}
			if res.Pagination == nil || len(res.Pagination.NextKey) == 0 {
				break
			}
			next = res.Pagination.NextKey
		}
	}
	return out
}

// latestProposalBy returns the id of proposer's newest SUBMITTED proposal
// on a body (the one just submitted).
func (e *simEnv) latestProposalBy(bodyName, proposer string) (uint64, bool) {
	var id uint64
	found := false
	var next []byte
	for {
		res, err := e.commonsQuery.ListProposals(e.ctx, &commonstypes.QueryListProposalsRequest{
			CouncilName: bodyName,
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

// proposal fetches a proposal.
func (e *simEnv) proposal(id uint64) (commonstypes.Proposal, bool) {
	res, err := e.commonsQuery.GetProposal(e.ctx, &commonstypes.QueryGetProposalRequest{ProposalId: id})
	if err != nil {
		return commonstypes.Proposal{}, false
	}
	return res.Proposal, true
}

// votedOn reports whether voter already voted on a proposal.
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

// voteThrough has the live sim seats of the proposal's body that have not
// voted yet vote YES until the proposal leaves SUBMITTED or no seat is left.
// It returns the last vote delivered.
func (e *simEnv) voteThrough(id uint64) (simtypes.OperationMsg, bool, error) {
	label := &commonstypes.MsgVoteProposal{}
	last := simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(label), "no seat left to vote")
	delivered := false
	p, ok := e.proposal(id)
	if !ok {
		return last, false, nil
	}
	live, _ := e.seats(p.CouncilName)
	for _, i := range e.r.Perm(len(live)) {
		p, ok := e.proposal(id)
		if !ok || p.Status != commonstypes.ProposalStatus_PROPOSAL_STATUS_SUBMITTED {
			break
		}
		voter := live[i].acc
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
		opMsg, err := e.deliverSmallFee(voter, msg)
		if err != nil {
			return opMsg, false, err
		}
		last, delivered = opMsg, delivered || opMsg.OK
	}
	return last, delivered, nil
}

// executable reports whether an accepted proposal may be executed now (the
// dry-run checks the execution window).
func (e *simEnv) executable(p commonstypes.Proposal) bool {
	return p.Status == commonstypes.ProposalStatus_PROPOSAL_STATUS_ACCEPTED && e.ctx.BlockTime().Unix() >= p.ExecutionTime
}

// execute has a random live sim account execute an accepted proposal when
// its message and the execution dry-run. done is false when it was not
// attempted.
func (e *simEnv) execute(id uint64, inner sdk.Msg) (simtypes.OperationMsg, bool, error) {
	if e.dryRun(inner) != nil {
		return simtypes.OperationMsg{}, false, nil // let it expire
	}
	acc, ok := pick(e.r, e.live(func(a simtypes.Account) bool { return e.spendable(a).IsPositive() }))
	if !ok {
		return simtypes.OperationMsg{}, false, nil
	}
	msg := &commonstypes.MsgExecuteProposal{Executor: acc.Address.String(), ProposalId: id}
	if e.dryRun(msg) != nil {
		return simtypes.OperationMsg{}, false, nil
	}
	opMsg, err := e.deliverSmallFee(acc, msg)
	return opMsg, err == nil && opMsg.OK, err
}

// advance moves a resolver-body proposal carrying a message of label's
// type one step (see the file comment). build returns the message for a new
// proposal signed by policy, or a reason why there is none; taken lists the
// open proposals' messages so build can avoid duplicating one.
func (e *simEnv) advance(
	label sdk.Msg,
	decode func(*codectypes.Any) (sdk.Msg, bool),
	build func(policy string, taken []sdk.Msg) (sdk.Msg, string, error),
) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	typeURL := sdk.MsgTypeURL(label)
	props := e.openProposals(typeURL, decode)

	// 1. Execute an accepted proposal.
	for _, i := range e.r.Perm(len(props)) {
		if !e.executable(props[i].p) {
			continue
		}
		opMsg, done, err := e.execute(props[i].p.Id, props[i].inner)
		if err != nil || done {
			return relabel(opMsg, typeURL, "executes"), nil, err
		}
	}

	// 2. Vote an open proposal through.
	now := e.ctx.BlockTime().Unix()
	for _, i := range e.r.Perm(len(props)) {
		p := props[i].p
		if p.Status != commonstypes.ProposalStatus_PROPOSAL_STATUS_SUBMITTED || p.SecretBallot || now > p.VotingDeadline {
			continue
		}
		if e.dryRun(props[i].inner) != nil {
			continue // the message no longer applies; let it expire
		}
		opMsg, voted, err := e.voteThrough(p.Id)
		if err != nil || voted {
			return relabel(opMsg, typeURL, "votes for"), nil, err
		}
	}

	// 3. Submit a new proposal, vote it through, execute when possible.
	fee, err := e.proposalFee()
	if err != nil {
		return noOpErr(label, err)
	}
	reason := "no resolver body"
	var b body
	var proposer simtypes.Account
	for _, i := range e.r.Perm(len(resolverBodies)) {
		cand, ok := e.resolveBody(resolverBodies[i])
		if !ok {
			reason = resolverBodies[i] + " is not a usable group policy"
			continue
		}
		acc, why := e.carrier(cand, typeURL, fee)
		if why != "" {
			reason = why
			continue
		}
		b, proposer, reason = cand, acc, ""
		break
	}
	if reason != "" {
		return noOp(label, reason)
	}
	taken := make([]sdk.Msg, 0, len(props))
	for _, op := range props {
		taken = append(taken, op.inner)
	}
	inner, reason, err := build(b.policy, taken)
	if err != nil {
		return noOpErr(label, err)
	}
	if reason != "" {
		return noOp(label, reason)
	}
	if err := e.dryRun(inner); err != nil {
		return noOp(label, "handler would reject: "+err.Error())
	}
	anyMsg, err := codectypes.NewAnyWithValue(inner)
	if err != nil {
		return noOpErr(label, err)
	}
	submit := &commonstypes.MsgSubmitProposal{
		Proposer:      proposer.Address.String(),
		PolicyAddress: b.policy,
		Messages:      []*codectypes.Any{anyMsg},
		Metadata:      fmt.Sprintf("simulation: %s", typeURL),
	}
	if e.tombstoned(submit.Proposer) {
		return noOp(label, "proposer is a tombstoned address")
	}
	if err := e.dryRun(submit); err != nil {
		return noOp(label, "proposal would be rejected: "+err.Error())
	}
	// The proposal fee is enforced on the tx fee by the x/commons ante
	// decorator, so this tx pays exactly the fee instead of a random one.
	opMsg, _, err := simulation.GenAndDeliverTx(e.input(proposer, submit, math.ZeroInt()), fee)
	e.invalidate()
	if err != nil || !opMsg.OK {
		return opMsg, nil, err
	}
	step := "submits"
	id, found := e.latestProposalBy(b.name, proposer.Address.String())
	if !found {
		return relabel(opMsg, typeURL, step), nil, nil
	}
	if _, voted, err := e.voteThrough(id); err != nil {
		return opMsg, nil, err
	} else if voted {
		step = "submits and votes for"
	}
	if p, ok := e.proposal(id); ok && e.executable(p) {
		_, done, err := e.execute(id, inner)
		if err != nil {
			return opMsg, nil, err
		}
		if done {
			step = "submits, votes and executes"
		}
	}
	return relabel(opMsg, typeURL, step), nil, nil
}

// relabel reports a delivered proposal step under the op's own message type
// (the per-op simulation statistics), naming the step in the comment.
func relabel(opMsg simtypes.OperationMsg, typeURL, step string) simtypes.OperationMsg {
	if !opMsg.OK {
		return opMsg
	}
	opMsg.Comment = step + " " + typeURL + " (delivered " + opMsg.Name + ")"
	opMsg.Name = typeURL
	return opMsg
}
