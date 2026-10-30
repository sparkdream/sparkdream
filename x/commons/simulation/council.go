package simulation

import (
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// A policy-gated message reaches its handler the way the spec routes it: a
// member of the authorized body submits an x/commons proposal carrying the
// message, members vote it through, and anyone executes it once accepted
// and its min execution period has passed. advance drives that one step per
// call, with real x/commons transactions:
//
//  1. execute an accepted proposal carrying the op's message type (anyone
//     may execute);
//  2. otherwise vote an open one through (members vote YES until the
//     decision policy accepts it early);
//  3. otherwise build a new message, submit it to a body whose live sim
//     seats can pass it, vote it through, and execute it at once when its
//     body has no min execution period.
//
// Every step is dry-run first; a proposal whose message the handler would
// now refuse (its target moved on, the body was vetoed) is left to expire.

// membershipMsgs are the messages that need a strict majority of the body
// (keeper proposalChangesMembership).
var membershipMsgs = map[string]bool{
	sdk.MsgTypeURL(&types.MsgUpdateGroupMembers{}): true,
	sdk.MsgTypeURL(&types.MsgRenewGroup{}):         true,
}

// feeExemptMsgs are the emergency actions the proposal-fee ante decorator
// lets through without the fee.
var feeExemptMsgs = map[string]bool{
	sdk.MsgTypeURL(&types.MsgEmergencyCancelGovProposal{}): true,
	sdk.MsgTypeURL(&types.MsgVetoGroupProposals{}):         true,
}

// proposalFee is the tx fee the x/commons ante decorator requires of a
// proposal carrying a message of typeURL.
func (e *simEnv) proposalFee(typeURL string) (sdk.Coins, error) {
	if feeExemptMsgs[typeURL] {
		return sdk.NewCoins(), nil
	}
	params, err := e.k.Params.Get(e.ctx)
	if err != nil {
		return nil, err
	}
	return sdk.ParseCoinsNormalized(params.ProposalFee)
}

// body is a group policy with the body behind it.
type body struct {
	policy string
	name   string
	g      types.Group
	dp     types.DecisionPolicy
}

// policyBody resolves a policy address to its body. ok is false when it is
// not a usable group policy: unknown, without a decision policy, not the
// standard or veto policy of the group it names, or a sim account (x/name's
// simulation can overwrite a group with a sim account as its "policy";
// signing as it would bypass the vote this path exists to exercise).
func (e *simEnv) policyBody(policy string) (body, bool, error) {
	if policy == "" {
		return body{}, false, nil
	}
	if _, isAcc := getAccount(e.accs, policy); isAcc {
		return body{}, false, nil
	}
	name, err := e.k.PolicyToName.Get(e.ctx, policy)
	if errors.Is(err, collections.ErrNotFound) {
		return body{}, false, nil
	}
	if err != nil {
		return body{}, false, err
	}
	g, found, err := e.group(name)
	if err != nil || !found {
		return body{}, false, err
	}
	if g.PolicyAddress != policy {
		veto, err := e.k.VetoPolicies.Get(e.ctx, name)
		if errors.Is(err, collections.ErrNotFound) || (err == nil && veto != policy) {
			return body{}, false, nil
		}
		if err != nil {
			return body{}, false, err
		}
	}
	dp, err := e.k.DecisionPolicies.Get(e.ctx, policy)
	if errors.Is(err, collections.ErrNotFound) {
		return body{}, false, nil
	}
	if err != nil {
		return body{}, false, err
	}
	return body{policy: policy, name: name, g: g, dp: dp}, true, nil
}

// vetoPolicy returns the veto policy of the group name ("" when none).
func (e *simEnv) vetoPolicy(name string) (string, error) {
	veto, err := e.k.VetoPolicies.Get(e.ctx, name)
	if errors.Is(err, collections.ErrNotFound) {
		return "", nil
	}
	return veto, err
}

// passable reports whether the live sim seats of b, all voting YES, meet
// its decision policy (keeper thresholdMet), including the strict majority
// a membership change needs.
func (e *simEnv) passable(b body, changesMembership bool) (bool, error) {
	_, live, total, err := e.seats(b.name)
	if err != nil || !total.IsPositive() {
		return false, err
	}
	yes := math.LegacyZeroDec()
	for _, s := range live {
		yes = yes.Add(s.weight)
	}
	if changesMembership && !yes.MulInt64(2).GT(total) {
		return false, nil
	}
	threshold, err := math.LegacyNewDecFromStr(b.dp.Threshold)
	if err != nil {
		return false, nil // an unparseable threshold never passes in the keeper either
	}
	if b.dp.PolicyType == keeper.PolicyTypePercentage {
		return yes.Quo(total).GTE(threshold), nil
	}
	return yes.GTE(threshold), nil
}

// carrier checks that policy can carry a proposal with a message of typeURL
// through its body (the message on its allowlist, a live term unless it is
// a renewal, open ballots, sim seats that can pass it) and returns a live
// seat who can pay the proposal fee. A non-empty reason means it cannot.
func (e *simEnv) carrier(policy, typeURL string) (body, simtypes.Account, string, error) {
	b, ok, err := e.policyBody(policy)
	if err != nil || !ok {
		return b, simtypes.Account{}, "not a usable group policy", err
	}
	perms, err := e.k.PolicyPermissions.Get(e.ctx, policy)
	if errors.Is(err, collections.ErrNotFound) {
		return b, simtypes.Account{}, "policy has no allowlist", nil
	}
	if err != nil {
		return b, simtypes.Account{}, "", err
	}
	allowed := false
	for _, m := range perms.AllowedMessages {
		allowed = allowed || m == typeURL
	}
	if !allowed {
		return b, simtypes.Account{}, typeURL + " is not on the policy's allowlist", nil
	}
	if e.termExpired(b.g) && typeURL != sdk.MsgTypeURL(&types.MsgRenewGroup{}) {
		return b, simtypes.Account{}, "the body's term has expired", nil
	}
	if b.g.BallotMode == types.BallotMode_BALLOT_MODE_SECRET {
		return b, simtypes.Account{}, "the body votes by secret ballot (x/shield only)", nil
	}
	ok, err = e.passable(b, membershipMsgs[typeURL])
	if err != nil || !ok {
		return b, simtypes.Account{}, "the body's live sim seats cannot pass it", err
	}
	fee, err := e.proposalFee(typeURL)
	if err != nil {
		return b, simtypes.Account{}, "", err
	}
	seats, err := e.liveSeatAccounts(b.name)
	if err != nil {
		return b, simtypes.Account{}, "", err
	}
	proposer, ok := pick(e.r, accountsWhere(seats, func(a simtypes.Account) bool { return e.canPay(a, fee) }))
	if !ok {
		return b, simtypes.Account{}, "no seat can pay the proposal fee", nil
	}
	return b, proposer, "", nil
}

// pickCarrier returns a random policy among candidates that can carry a
// message of typeURL, with its proposer. reason holds the last refusal when
// none can.
func (e *simEnv) pickCarrier(candidates []string, typeURL string) (string, simtypes.Account, string, error) {
	reason := "no candidate body"
	for _, i := range e.r.Perm(len(candidates)) {
		_, proposer, why, err := e.carrier(candidates[i], typeURL)
		if err != nil {
			return "", simtypes.Account{}, "", err
		}
		if why == "" {
			return candidates[i], proposer, "", nil
		}
		reason = why
	}
	return "", simtypes.Account{}, reason, nil
}

// openProp is an open (SUBMITTED or ACCEPTED) proposal carrying exactly one
// message, unpacked.
type openProp struct {
	p     types.Proposal
	inner sdk.Msg
}

// openProposalIDs returns the ids of the open proposals (the proposal
// queue indexes exactly the SUBMITTED and ACCEPTED ones).
func (e *simEnv) openProposalIDs() ([]uint64, error) {
	var out []uint64
	seen := map[uint64]bool{}
	err := e.k.ProposalQueue.Walk(e.ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		if id := key.K2(); !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
		return false, nil
	})
	return out, err
}

// openProposals returns the open proposals carrying one message of typeURL.
func (e *simEnv) openProposals(typeURL string) ([]openProp, error) {
	ids, err := e.openProposalIDs()
	if err != nil {
		return nil, err
	}
	var out []openProp
	for _, id := range ids {
		p, err := e.k.Proposals.Get(e.ctx, id)
		if errors.Is(err, collections.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if len(p.Messages) != 1 || p.Messages[0] == nil || p.Messages[0].TypeUrl != typeURL {
			continue
		}
		var inner sdk.Msg
		if err := e.k.Codec().UnpackAny(p.Messages[0], &inner); err != nil {
			continue // an undecodable proposal is left to expire
		}
		out = append(out, openProp{p: p, inner: inner})
	}
	return out, nil
}

// executable reports whether an accepted proposal is inside its execution
// window now.
func (e *simEnv) executable(p types.Proposal) bool {
	now := e.ctx.BlockTime().Unix()
	return p.Status == types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED &&
		now >= p.ExecutionTime && now <= p.ExecutionTime+keeper.ProposalExecutionWindow
}

// execute has a random live sim account execute an accepted proposal when
// the dry-run accepts it. delivered is false when it was not attempted.
func (e *simEnv) execute(id uint64) (simtypes.OperationMsg, bool, error) {
	executors := accountsWhere(e.liveAccounts(), func(a simtypes.Account) bool {
		return e.bk.SpendableCoins(e.ctx, a.Address).AmountOf(sdk.DefaultBondDenom).IsPositive()
	})
	acc, ok := pick(e.r, executors)
	if !ok {
		return simtypes.OperationMsg{}, false, nil
	}
	msg := &types.MsgExecuteProposal{Executor: acc.Address.String(), ProposalId: id}
	if e.dryRun(msg) != nil {
		return simtypes.OperationMsg{}, false, nil
	}
	opMsg, _, err := e.deliver(acc, msg)
	return opMsg, err == nil && opMsg.OK, err
}

// voteThrough has the live sim seats of the proposal's body that have not
// voted yet vote YES until the proposal leaves SUBMITTED or no seat is left.
// It returns the last vote delivered.
func (e *simEnv) voteThrough(id uint64) (simtypes.OperationMsg, bool, error) {
	label := &types.MsgVoteProposal{}
	last := simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(label), "no seat left to vote")
	delivered := false
	p, err := e.k.Proposals.Get(e.ctx, id)
	if err != nil {
		return last, false, err
	}
	seats, err := e.liveSeatAccounts(p.CouncilName)
	if err != nil {
		return last, false, err
	}
	for _, i := range e.r.Perm(len(seats)) {
		p, err := e.k.Proposals.Get(e.ctx, id)
		if err != nil {
			return last, delivered, err
		}
		if p.Status != types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED {
			break
		}
		voter := seats[i]
		has, err := e.k.Votes.Has(e.ctx, collections.Join(id, voter.Address.String()))
		if err != nil {
			return last, delivered, err
		}
		if has {
			continue
		}
		msg := &types.MsgVoteProposal{
			Voter:      voter.Address.String(),
			ProposalId: id,
			Option:     types.VoteOption_VOTE_OPTION_YES,
		}
		if e.dryRun(msg) != nil {
			continue
		}
		opMsg, _, err := e.deliver(voter, msg)
		if err != nil {
			return opMsg, false, err
		}
		last, delivered = opMsg, delivered || opMsg.OK
	}
	return last, delivered, nil
}

// proposal is a new proposal an op wants submitted: inner signed by policy,
// submitted by proposer (a live seat of the policy's body).
type proposal struct {
	policy   string
	proposer simtypes.Account
	inner    sdk.Msg
}

// advance moves a proposal carrying a message of label's type one step (see
// the file comment). build returns the new proposal for step 3, or a reason
// why there is none.
func (e *simEnv) advance(label sdk.Msg, build func() (proposal, string, error)) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	typeURL := sdk.MsgTypeURL(label)
	props, err := e.openProposals(typeURL)
	if err != nil {
		return noOpErr(label, err)
	}

	// 1. Execute an accepted proposal.
	for _, i := range e.r.Perm(len(props)) {
		if !e.executable(props[i].p) {
			continue
		}
		opMsg, done, err := e.execute(props[i].p.Id)
		if err != nil || done {
			return relabel(opMsg, typeURL, "executes"), nil, err
		}
	}

	// 2. Vote an open proposal through.
	now := e.ctx.BlockTime().Unix()
	for _, i := range e.r.Perm(len(props)) {
		p := props[i].p
		if p.Status != types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED || p.SecretBallot || now > p.VotingDeadline {
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
	prop, reason, err := build()
	if err != nil || reason != "" {
		if err != nil {
			return noOpErr(label, err)
		}
		return noOp(label, reason)
	}
	if err := e.dryRun(prop.inner); err != nil {
		return noOp(label, "handler would reject: "+err.Error())
	}
	anyMsg, err := codectypes.NewAnyWithValue(prop.inner)
	if err != nil {
		return noOpErr(label, err)
	}
	fee, err := e.proposalFee(typeURL)
	if err != nil {
		return noOpErr(label, err)
	}
	submit := &types.MsgSubmitProposal{
		Proposer:      prop.proposer.Address.String(),
		PolicyAddress: prop.policy,
		Messages:      []*codectypes.Any{anyMsg},
		Metadata:      fmt.Sprintf("simulation: %s", typeURL),
	}
	if e.tombstoned(submit.Proposer) {
		return noOp(submit, "proposer is a tombstoned address")
	}
	if err := e.dryRun(submit); err != nil {
		return noOp(submit, "handler would reject: "+err.Error())
	}
	// The proposal fee is enforced on the tx fee by the x/commons ante
	// decorator, so this tx pays exactly the fee instead of a random one.
	opMsg, _, err := simulation.GenAndDeliverTx(e.input(prop.proposer, submit), fee)
	if err != nil || !opMsg.OK {
		return opMsg, nil, err
	}
	next, err := e.k.ProposalSeq.Peek(e.ctx)
	if err != nil || next == 0 {
		return opMsg, nil, err
	}
	id := next - 1
	step := "submits"
	if voteMsg, voted, err := e.voteThrough(id); err != nil {
		return voteMsg, nil, err
	} else if voted {
		step = "submits and votes for"
	}
	p, err := e.k.Proposals.Get(e.ctx, id)
	if err != nil {
		return opMsg, nil, err
	}
	if e.executable(p) {
		execMsg, done, err := e.execute(id)
		if err != nil {
			return execMsg, nil, err
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
