package keeper

import (
	"context"
	"errors"
	"slices"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"

	"sparkdream/x/commons/types"
)

// vetoGroupProposalsTypeURL is the only message a veto seat may propose or
// vote on.
const vetoGroupProposalsTypeURL = "/sparkdream.commons.v1.MsgVetoGroupProposals"

// BoardSeatMsgs are the messages a council needs to act as a member of
// another body (the Technical and Ecosystem Councils on the Commons
// Supervisory Board): a member that is a policy address has no key, so it
// proposes and votes by executing these from a proposal of its own.
var BoardSeatMsgs = []string{
	"/sparkdream.commons.v1.MsgSubmitProposal",
	"/sparkdream.commons.v1.MsgVoteProposal",
}

// vetoSeat returns the seat a council's veto policy may act for in body, or
// "" when actor is not such a policy or the proposal is not a pure veto.
//
// A body whose members are councils (the Commons Supervisory Board) decides
// through its members' own proposals. A council's standard policy is slow by
// design (a voting window plus a min execution period of days), which is
// fine for the board's lasting powers (replacing or deleting a committee)
// but too slow for its brake: the committee it oversees executes a passed
// proposal after its own min execution period, and a veto that lands later
// vetoes nothing. So for a proposal that carries only MsgVetoGroupProposals,
// the council's veto policy (the same minority, no execution wait, that may
// already veto the council's own children) may propose and cast the vote of
// the council's seat. Everything else the body decides still needs the
// council's standard policy.
//
// The rule applies only in a body with no veto policy of its own: a council
// has one, and its members vote there directly.
func (k Keeper) vetoSeat(ctx context.Context, body, actor string, msgs []*codectypes.Any) (string, error) {
	if len(msgs) == 0 {
		return "", nil
	}
	for _, anyMsg := range msgs {
		var sdkMsg sdk.Msg
		if err := k.cdc.UnpackAny(anyMsg, &sdkMsg); err != nil {
			return "", nil // refused later, with the ordinary error
		}
		if sdk.MsgTypeURL(sdkMsg) != vetoGroupProposalsTypeURL {
			return "", nil
		}
	}
	notFound := func(err error) (string, error) {
		if errors.Is(err, collections.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	bodyGroup, err := k.Groups.Get(ctx, body)
	if err != nil {
		return notFound(err)
	}
	if bodyGroup.VetoPolicyAddress != "" {
		return "", nil
	}
	council, err := k.PolicyToName.Get(ctx, actor)
	if err != nil {
		return notFound(err)
	}
	vetoAddr, err := k.VetoPolicies.Get(ctx, council)
	if err != nil {
		return notFound(err)
	}
	if vetoAddr != actor {
		return "", nil
	}
	councilGroup, err := k.Groups.Get(ctx, council)
	if err != nil {
		return notFound(err)
	}
	seat := councilGroup.PolicyAddress
	if seat == "" {
		return "", nil
	}
	isMember, err := k.HasMember(ctx, body, seat)
	if err != nil {
		return "", err
	}
	if !isMember {
		return "", nil
	}
	return seat, nil
}

// boardSeatRecusal returns the member whose vote does not count on proposal,
// or "" when every member votes.
//
// The Commons Council holds a golden-share seat, half of the votes, on the
// Technical and Ecosystem Councils. Those two councils are the Commons
// Supervisory Board, which oversees the Commons pillar's governance
// committee: the body that elects the Commons Council. If the golden seat
// counted when a council decides how to use its board seat, the Commons
// Council could block every board action against its own committee through
// the standard policies (nothing reaches 66% without its half), and cast
// both councils' veto seats by itself through the veto policies (its half
// alone clears 49%), which would hand it a veto over the committee that can
// replace it. So on a proposal that carries only board-seat messages
// (BoardSeatMsgs) the golden seat is recused: its vote is not counted and
// its weight is left out of the body the threshold is measured against.
func (k Keeper) boardSeatRecusal(ctx context.Context, proposal types.Proposal) (string, error) {
	if len(proposal.Messages) == 0 {
		return "", nil
	}
	for _, anyMsg := range proposal.Messages {
		var sdkMsg sdk.Msg
		if err := k.cdc.UnpackAny(anyMsg, &sdkMsg); err != nil {
			return "", nil
		}
		if !slices.Contains(BoardSeatMsgs, sdk.MsgTypeURL(sdkMsg)) {
			return "", nil
		}
	}
	return k.goldenShareSeat(ctx, proposal.CouncilName)
}
