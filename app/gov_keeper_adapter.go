package app

import (
	"context"
	"time"

	"cosmossdk.io/collections"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

// GovKeeperAdapter wraps the concrete *govkeeper.Keeper to satisfy the
// commons types.GovKeeper interface, bridging method-name differences
// between the concrete keeper and the interface.
type GovKeeperAdapter struct {
	keeper *govkeeper.Keeper
}

func NewGovKeeperAdapter(k *govkeeper.Keeper) *GovKeeperAdapter {
	return &GovKeeperAdapter{keeper: k}
}

func (a *GovKeeperAdapter) GetProposal(ctx context.Context, proposalID uint64) (v1.Proposal, error) {
	return a.keeper.Proposals.Get(ctx, proposalID)
}

func (a *GovKeeperAdapter) SetProposal(ctx context.Context, proposal v1.Proposal) error {
	return a.keeper.SetProposal(ctx, proposal)
}

func (a *GovKeeperAdapter) Tally(ctx context.Context, proposal v1.Proposal) (bool, bool, v1.TallyResult, error) {
	return a.keeper.Tally(ctx, proposal)
}

func (a *GovKeeperAdapter) CancelProposal(ctx context.Context, proposalID uint64, proposer string) error {
	return a.keeper.CancelProposal(ctx, proposalID, proposer)
}

func (a *GovKeeperAdapter) ChargeDeposit(ctx context.Context, proposalID uint64, destAddress string, percent string) error {
	return a.keeper.ChargeDeposit(ctx, proposalID, destAddress, percent)
}

func (a *GovKeeperAdapter) ActiveProposalsQueueRemove(ctx context.Context, proposalID uint64, votingEndTime time.Time) error {
	return a.keeper.ActiveProposalsQueue.Remove(ctx, collections.Join(votingEndTime, proposalID))
}

func (a *GovKeeperAdapter) VotingPeriodProposalsRemove(ctx context.Context, proposalID uint64) error {
	return a.keeper.VotingPeriodProposals.Remove(ctx, proposalID)
}
