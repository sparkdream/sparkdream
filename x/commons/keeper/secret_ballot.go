package keeper

import (
	"context"
	"sort"
	commontypes "sparkdream/x/common/types"

	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	zkcrypto "sparkdream/tools/crypto"
)

// Secret ballots (spec §11). A secret-ballot proposal or poll snapshots the
// group's members when it opens: a Merkle tree of MiMC(zk_public_key, 0) for
// every member with a registered ZK key, built with the x/shield circuit's
// depth and hash. A vote is then MsgAnonymousVoteProposal or
// MsgAnonymousVotePoll inside x/shield's MsgShieldedExec, proven against that
// snapshot's root (PROOF_DOMAIN_GROUP_TREE, see ShieldMerkleRoot) with a
// nullifier scoped to the proposal or poll, so each member votes once and
// only members vote. Open votes are refused on a secret ballot, which closes
// the old hole where an anonymous vote could be cast on top of an open one.

// ballotMode reads a group's ballot mode, UNSPECIFIED as OPEN.
func ballotMode(g types.Group) types.BallotMode {
	if g.BallotMode == types.BallotMode_BALLOT_MODE_UNSPECIFIED {
		return types.BallotMode_BALLOT_MODE_OPEN
	}
	return g.BallotMode
}

// resolveSecretBallot decides whether a new proposal to g is a secret
// ballot: always in a SECRET group, at the proposer's choice in a
// PROPOSER_CHOICE group, never in an OPEN group (asking for one is refused).
func resolveSecretBallot(g types.Group, requested bool) (bool, error) {
	switch ballotMode(g) {
	case types.BallotMode_BALLOT_MODE_SECRET:
		return true, nil
	case types.BallotMode_BALLOT_MODE_PROPOSER_CHOICE:
		return requested, nil
	default:
		if requested {
			return false, errorsmod.Wrapf(types.ErrBallotMode, "group %s votes openly", g.Index)
		}
		return false, nil
	}
}

// VoterTreeGasPerLeaf is the gas charged per voter-tree leaf for the MiMC
// hashing a snapshot does outside the store (the leaf hash and its share of
// the sparse Merkle tree), which the gas meter does not see on its own
// (CMN-107). Store reads are metered as usual.
const VoterTreeGasPerLeaf uint64 = 2_000

// buildVoterTree snapshots councilName's members for a secret ballot. It
// refuses a group whose members do not all share one weight (an anonymous
// vote carries no weight) and a snapshot smaller than MinSecretBallotVoters
// (the tally would all but name each vote).
func (k Keeper) buildVoterTree(ctx context.Context, councilName string) (types.VoterTree, error) {
	if k.late.repKeeper == nil {
		return types.VoterTree{}, errorsmod.Wrap(types.ErrSecretBallotUnavailable, "x/rep is not wired")
	}
	members, err := k.GetCouncilMembers(ctx, councilName)
	if err != nil {
		return types.VoterTree{}, err
	}
	if len(members) == 0 {
		return types.VoterTree{}, errorsmod.Wrapf(types.ErrSecretBallotUnavailable, "group %s has no members", councilName)
	}
	weight, err := math.LegacyNewDecFromStr(members[0].Weight)
	if err != nil {
		return types.VoterTree{}, errorsmod.Wrapf(types.ErrInvalidMemberWeight, "%s: %s", members[0].Address, members[0].Weight)
	}
	for _, m := range members[1:] {
		w, err := math.LegacyNewDecFromStr(m.Weight)
		if err != nil || !w.Equal(weight) {
			return types.VoterTree{}, errorsmod.Wrapf(types.ErrSecretBallotUnavailable, "group %s members have unequal weights", councilName)
		}
	}

	// Member-address order, so every node builds the same tree.
	sort.Slice(members, func(i, j int) bool { return members[i].Address < members[j].Address })

	gasMeter := sdk.UnwrapSDKContext(ctx).GasMeter()
	leaves := make(map[uint64][]byte, len(members))
	seen := make(map[string]bool, len(members))
	var list []types.VoterTreeLeaf
	for _, m := range members {
		pk := k.late.repKeeper.ActiveZkPublicKey(ctx, m.Address)
		if len(pk) == 0 {
			continue
		}
		gasMeter.ConsumeGas(VoterTreeGasPerLeaf, "secret ballot voter tree")
		leaf := zkcrypto.ComputeLeaf(pk, 0)
		// Two members cannot both prove with one key; count it once.
		if seen[string(leaf)] {
			continue
		}
		seen[string(leaf)] = true
		idx := uint64(len(list))
		leaves[idx] = leaf
		list = append(list, types.VoterTreeLeaf{Index: idx, Hash: leaf})
	}
	if uint64(len(list)) < MinSecretBallotVoters {
		return types.VoterTree{}, errorsmod.Wrapf(types.ErrSecretBallotUnavailable,
			"group %s has %d members with a registered ZK key; a secret ballot needs %d", councilName, len(list), MinSecretBallotVoters)
	}
	proof, err := zkcrypto.SparseMerkleProof(leaves, zkcrypto.TreeDepth, 0)
	if err != nil {
		return types.VoterTree{}, err
	}
	return types.VoterTree{
		Root:         proof.Root,
		Depth:        uint32(zkcrypto.TreeDepth),
		Leaves:       list,
		MemberCount:  uint64(len(members)),
		MemberWeight: members[0].Weight,
	}, nil
}

// startSecretBallot snapshots the voters of a new secret-ballot proposal
// and records the snapshot on it. The caller stores the proposal; the tree
// is stored under its ID by storeProposalVoterTree.
func (k Keeper) startSecretBallot(ctx context.Context, proposal *types.Proposal) (types.VoterTree, error) {
	tree, err := k.buildVoterTree(ctx, proposal.CouncilName)
	if err != nil {
		return types.VoterTree{}, err
	}
	proposal.SecretBallot = true
	proposal.VoterRoot = tree.Root
	proposal.BallotMemberCount = tree.MemberCount
	proposal.BallotMemberWeight = tree.MemberWeight
	return tree, nil
}

// dropProposalVoterTree deletes a proposal's voter snapshot once voting on
// it is over (the root stays on the proposal).
func (k Keeper) dropProposalVoterTree(ctx context.Context, proposalID uint64) {
	if err := k.ProposalVoterTrees.Remove(ctx, proposalID); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("dropProposalVoterTree: ProposalVoterTrees.Remove failed", "err", err)
	}
}

// secretBallotWeights returns a secret ballot's (yes, cast, whole-body)
// weights: each anonymous vote counts the snapshot's equal member weight,
// and the body is the snapshot's member count, members without a ZK key
// included (they cannot vote, like an absent member).
func (k Keeper) secretBallotWeights(ctx context.Context, proposal types.Proposal) (yes, cast, body math.LegacyDec, err error) {
	w, err := math.LegacyNewDecFromStr(proposal.BallotMemberWeight)
	if err != nil {
		return math.LegacyDec{}, math.LegacyDec{}, math.LegacyDec{}, err
	}
	tally, err := k.AnonVoteTallies.Get(ctx, proposal.Id)
	if err != nil {
		tally = types.AnonVoteTally{}
	}
	n := tally.YesCount + tally.NoCount + tally.AbstainCount + tally.NoWithVetoCount
	return w.MulInt64(commontypes.Saturate[int64](tally.YesCount)), w.MulInt64(commontypes.Saturate[int64](n)), w.MulInt64(commontypes.Saturate[int64](proposal.BallotMemberCount)), nil
}

// ShieldMerkleRoot implements x/shield's ShieldRootResolver: the root a
// PROOF_DOMAIN_GROUP_TREE proof for msg must be against. Only a secret
// ballot still open for voting has one.
func (k Keeper) ShieldMerkleRoot(ctx context.Context, msg sdk.Msg) ([]byte, error) {
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	switch m := msg.(type) {
	case *types.MsgAnonymousVoteProposal:
		p, err := k.Proposals.Get(ctx, m.ProposalId)
		if err != nil {
			return nil, errorsmod.Wrapf(types.ErrBallotMode, "proposal %d not found", m.ProposalId)
		}
		if !p.SecretBallot || p.Status != types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED || now > p.VotingDeadline {
			return nil, errorsmod.Wrapf(types.ErrBallotMode, "proposal %d is not an open secret ballot", m.ProposalId)
		}
		return p.VoterRoot, nil
	case *types.MsgAnonymousVotePoll:
		p, err := k.Polls.Get(ctx, m.PollId)
		if err != nil {
			return nil, errorsmod.Wrapf(types.ErrPollNotFound, "poll %d", m.PollId)
		}
		if !p.SecretBallot || p.Status != types.PollStatus_POLL_STATUS_OPEN || now > p.Deadline {
			return nil, errorsmod.Wrapf(types.ErrBallotMode, "poll %d is not an open secret ballot", m.PollId)
		}
		return p.VoterRoot, nil
	default:
		return nil, errorsmod.Wrapf(types.ErrBallotMode, "%s has no voter snapshot", sdk.MsgTypeURL(msg))
	}
}
