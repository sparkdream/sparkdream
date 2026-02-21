package simulation

import (
	"math/rand"

	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

// --- Finder functions ---

// findVoterRegistration returns a random voter registration matching the active flag.
func findVoterRegistration(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, active bool) (*types.VoterRegistration, error) {
	var regs []types.VoterRegistration
	err := k.VoterRegistration.Walk(ctx, nil, func(addr string, reg types.VoterRegistration) (bool, error) {
		if reg.Active == active {
			regs = append(regs, reg)
		}
		return false, nil
	})
	if err != nil || len(regs) == 0 {
		return nil, err
	}
	selected := regs[r.Intn(len(regs))]
	return &selected, nil
}

// findProposal returns a random proposal with the given status.
func findProposal(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, status types.ProposalStatus) (*types.VotingProposal, error) {
	var proposals []types.VotingProposal
	err := k.VotingProposal.Walk(ctx, nil, func(id uint64, p types.VotingProposal) (bool, error) {
		if p.Status == status {
			proposals = append(proposals, p)
		}
		return false, nil
	})
	if err != nil || len(proposals) == 0 {
		return nil, err
	}
	selected := proposals[r.Intn(len(proposals))]
	return &selected, nil
}

// findSealedVote returns a random sealed vote on the given proposal matching the revealed flag.
func findSealedVote(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, proposalID uint64, revealed bool) (*types.SealedVote, error) {
	var votes []types.SealedVote
	err := k.SealedVote.Walk(ctx, nil, func(key string, sv types.SealedVote) (bool, error) {
		if sv.ProposalId == proposalID && sv.Revealed == revealed {
			votes = append(votes, sv)
		}
		return false, nil
	})
	if err != nil || len(votes) == 0 {
		return nil, err
	}
	selected := votes[r.Intn(len(votes))]
	return &selected, nil
}

// --- Get-or-create functions ---

// getOrCreateVoterRegistration registers a voter with random ZK key if not already registered.
func getOrCreateVoterRegistration(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, address string) error {
	// Check if already registered
	has, err := k.VoterRegistration.Has(ctx, address)
	if err != nil {
		return err
	}
	if has {
		return nil
	}

	reg := types.VoterRegistration{
		Address:             address,
		ZkPublicKey:         randomZKPublicKey(r),
		EncryptionPublicKey: randomZKPublicKey(r),
		RegisteredAt:        ctx.BlockHeight(),
		Active:              true,
	}

	return k.VoterRegistration.Set(ctx, address, reg)
}

// getOrCreateActiveProposal creates or finds a PUBLIC proposal in ACTIVE status.
func getOrCreateActiveProposal(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, proposer string) (uint64, error) {
	// Try to find existing ACTIVE proposal
	p, err := findProposal(r, ctx, k, types.ProposalStatus_PROPOSAL_STATUS_ACTIVE)
	if err == nil && p != nil {
		return p.Id, nil
	}

	// Ensure proposer is registered
	if err := getOrCreateVoterRegistration(r, ctx, k, proposer); err != nil {
		return 0, err
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}

	proposalID, err := k.VotingProposalSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	options := randomVoteOptions()
	tally := make([]*types.VoteTally, len(options))
	for i, opt := range options {
		tally[i] = &types.VoteTally{OptionId: opt.Id, VoteCount: 0}
	}

	proposal := types.VotingProposal{
		Id:             proposalID,
		Title:          randomProposalTitle(r),
		Description:    randomProposalDescription(r),
		Proposer:       proposer,
		MerkleRoot:     randomNullifier(r),
		SnapshotBlock:  ctx.BlockHeight(),
		EligibleVoters: 100,
		Options:        options,
		VotingStart:    ctx.BlockHeight(),
		VotingEnd:      ctx.BlockHeight() + params.DefaultVotingPeriodEpochs,
		Quorum:         params.DefaultQuorum,
		Threshold:      params.DefaultThreshold,
		Tally:          tally,
		Status:         types.ProposalStatus_PROPOSAL_STATUS_ACTIVE,
		Outcome:        types.ProposalOutcome_PROPOSAL_OUTCOME_UNSPECIFIED,
		ProposalType:   types.ProposalType_PROPOSAL_TYPE_GENERAL,
		CreatedAt:      ctx.BlockHeight(),
		Visibility:     types.VisibilityLevel_VISIBILITY_PUBLIC,
	}

	if err := k.VotingProposal.Set(ctx, proposalID, proposal); err != nil {
		return 0, err
	}

	// Create a mock snapshot
	snapshot := types.VoterTreeSnapshot{
		ProposalId:    proposalID,
		MerkleRoot:    proposal.MerkleRoot,
		SnapshotBlock: ctx.BlockHeight(),
		VoterCount:    100,
	}
	if err := k.VoterTreeSnapshot.Set(ctx, proposalID, snapshot); err != nil {
		return 0, err
	}

	return proposalID, nil
}

// getOrCreateSealedProposal creates or finds a SEALED proposal in ACTIVE status.
func getOrCreateSealedProposal(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, proposer string) (uint64, error) {
	// Try to find existing ACTIVE SEALED proposal
	var found *types.VotingProposal
	_ = k.VotingProposal.Walk(ctx, nil, func(id uint64, p types.VotingProposal) (bool, error) {
		if p.Status == types.ProposalStatus_PROPOSAL_STATUS_ACTIVE &&
			p.Visibility == types.VisibilityLevel_VISIBILITY_SEALED {
			found = &p
			return true, nil // stop
		}
		return false, nil
	})
	if found != nil {
		return found.Id, nil
	}

	// Ensure proposer is registered
	if err := getOrCreateVoterRegistration(r, ctx, k, proposer); err != nil {
		return 0, err
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}

	proposalID, err := k.VotingProposalSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	options := randomVoteOptions()
	tally := make([]*types.VoteTally, len(options))
	for i, opt := range options {
		tally[i] = &types.VoteTally{OptionId: opt.Id, VoteCount: 0}
	}

	proposal := types.VotingProposal{
		Id:             proposalID,
		Title:          randomProposalTitle(r),
		Description:    randomProposalDescription(r),
		Proposer:       proposer,
		MerkleRoot:     randomNullifier(r),
		SnapshotBlock:  ctx.BlockHeight(),
		EligibleVoters: 100,
		Options:        options,
		VotingStart:    ctx.BlockHeight(),
		VotingEnd:      ctx.BlockHeight() + params.DefaultVotingPeriodEpochs,
		Quorum:         params.DefaultQuorum,
		Threshold:      params.DefaultThreshold,
		Tally:          tally,
		Status:         types.ProposalStatus_PROPOSAL_STATUS_ACTIVE,
		Outcome:        types.ProposalOutcome_PROPOSAL_OUTCOME_UNSPECIFIED,
		ProposalType:   types.ProposalType_PROPOSAL_TYPE_GENERAL,
		CreatedAt:      ctx.BlockHeight(),
		Visibility:     types.VisibilityLevel_VISIBILITY_SEALED,
	}

	if err := k.VotingProposal.Set(ctx, proposalID, proposal); err != nil {
		return 0, err
	}

	snapshot := types.VoterTreeSnapshot{
		ProposalId:    proposalID,
		MerkleRoot:    proposal.MerkleRoot,
		SnapshotBlock: ctx.BlockHeight(),
		VoterCount:    100,
	}
	if err := k.VoterTreeSnapshot.Set(ctx, proposalID, snapshot); err != nil {
		return 0, err
	}

	return proposalID, nil
}

// getOrCreateTallyingProposal creates or finds a SEALED proposal in TALLYING status (for reveal).
func getOrCreateTallyingProposal(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, proposer string) (uint64, error) {
	// Try to find existing TALLYING proposal
	p, err := findProposal(r, ctx, k, types.ProposalStatus_PROPOSAL_STATUS_TALLYING)
	if err == nil && p != nil {
		return p.Id, nil
	}

	// Create a sealed proposal and transition to TALLYING
	proposalID, err := getOrCreateSealedProposal(r, ctx, k, proposer)
	if err != nil {
		return 0, err
	}

	proposal, err := k.VotingProposal.Get(ctx, proposalID)
	if err != nil {
		return 0, err
	}

	if proposal.Status == types.ProposalStatus_PROPOSAL_STATUS_TALLYING {
		return proposalID, nil
	}

	proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_TALLYING

	if err := k.VotingProposal.Set(ctx, proposalID, proposal); err != nil {
		return 0, err
	}

	return proposalID, nil
}

// --- Data generators ---

// randomZKPublicKey generates a 32-byte random key.
func randomZKPublicKey(r *rand.Rand) []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(r.Intn(256))
	}
	return key
}

// randomNullifier generates a 32-byte random nullifier.
func randomNullifier(r *rand.Rand) []byte {
	return randomZKPublicKey(r)
}

// randomProof generates a 192-byte random proof.
func randomProof(r *rand.Rand) []byte {
	proof := make([]byte, 192)
	for i := range proof {
		proof[i] = byte(r.Intn(256))
	}
	return proof
}

// randomVoteCommitment generates a 32-byte random vote commitment.
func randomVoteCommitment(r *rand.Rand) []byte {
	return randomZKPublicKey(r)
}

// randomProposalTitle generates a random proposal title.
func randomProposalTitle(r *rand.Rand) string {
	titles := []string{
		"Parameter Update Proposal",
		"Budget Allocation Decision",
		"Council Election Vote",
		"Community Initiative Review",
		"Governance Policy Change",
		"Resource Distribution Plan",
		"Technical Upgrade Approval",
		"Membership Policy Update",
		"Treasury Spending Proposal",
		"Protocol Enhancement Review",
	}
	return titles[r.Intn(len(titles))]
}

// randomProposalDescription generates a random proposal description.
func randomProposalDescription(r *rand.Rand) string {
	descriptions := []string{
		"This proposal seeks to adjust system parameters for improved performance.",
		"A budget allocation request for the upcoming development cycle.",
		"Electing new council members for the governance structure.",
		"Review of community-driven initiative for approval.",
		"Proposed changes to governance policies and procedures.",
		"Plan for equitable distribution of community resources.",
	}
	return descriptions[r.Intn(len(descriptions))]
}

// randomVoteOptions returns a standard set of vote options (yes/no/abstain).
func randomVoteOptions() []*types.VoteOption {
	return []*types.VoteOption{
		{Id: 0, Label: "yes"},
		{Id: 1, Label: "no"},
		{Id: 2, Label: "abstain"},
	}
}

// --- Account helpers ---

// pickDifferentAccount returns a random sim account that is NOT the given address.
func pickDifferentAccount(r *rand.Rand, accs []simtypes.Account, exclude string) (simtypes.Account, bool) {
	filtered := make([]simtypes.Account, 0, len(accs))
	for _, acc := range accs {
		if acc.Address.String() != exclude {
			filtered = append(filtered, acc)
		}
	}
	if len(filtered) == 0 {
		return simtypes.Account{}, false
	}
	return filtered[r.Intn(len(filtered))], true
}
