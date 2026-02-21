package keeper

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/vote/types"
)

// NullifierKey returns the composite key for a vote by proposal and nullifier hex.
func NullifierKey(proposalID uint64, nullifier []byte) string {
	return fmt.Sprintf("%d/%s", proposalID, hex.EncodeToString(nullifier))
}

// NullifierHex returns the hex string of a nullifier.
func NullifierHex(nullifier []byte) string {
	return hex.EncodeToString(nullifier)
}

// nullifierKey returns a composite key for the UsedNullifier store.
func nullifierKey(proposalID uint64, nullifier []byte) string {
	return NullifierKey(proposalID, nullifier)
}

// proposalNullifierKey returns a composite key for the UsedProposalNullifier store.
func proposalNullifierKey(epoch uint64, nullifier []byte) string {
	return fmt.Sprintf("%d/%s", epoch, hex.EncodeToString(nullifier))
}

// tleShareKey returns a composite key for TleDecryptionShare.
func tleShareKey(validator string, epoch uint64) string {
	return fmt.Sprintf("%s/%d", validator, epoch)
}

// isNullifierUsed checks whether a nullifier has been used for a given proposal.
func (k Keeper) isNullifierUsed(ctx context.Context, proposalID uint64, nullifier []byte) bool {
	key := nullifierKey(proposalID, nullifier)
	has, err := k.UsedNullifier.Has(ctx, key)
	if err != nil {
		return false
	}
	return has
}

// isProposalNullifierUsed checks whether a proposal-creation nullifier has been used for a given epoch.
func (k Keeper) isProposalNullifierUsed(ctx context.Context, epoch uint64, nullifier []byte) bool {
	key := proposalNullifierKey(epoch, nullifier)
	has, err := k.UsedProposalNullifier.Has(ctx, key)
	if err != nil {
		return false
	}
	return has
}

// recordNullifier stores a used nullifier for a proposal.
func (k Keeper) recordNullifier(ctx context.Context, proposalID uint64, nullifier []byte) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	key := nullifierKey(proposalID, nullifier)
	return k.UsedNullifier.Set(ctx, key, types.UsedNullifier{
		Index:      key,
		ProposalId: proposalID,
		Nullifier:  nullifier,
		UsedAt:     sdkCtx.BlockHeight(),
	})
}

// recordProposalNullifier stores a used proposal-creation nullifier for an epoch.
func (k Keeper) recordProposalNullifier(ctx context.Context, epoch uint64, nullifier []byte) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	key := proposalNullifierKey(epoch, nullifier)
	return k.UsedProposalNullifier.Set(ctx, key, types.UsedProposalNullifier{
		Index:     key,
		Epoch:     epoch,
		Nullifier: nullifier,
		UsedAt:    sdkCtx.BlockHeight(),
	})
}

// updateTally increments the tally count for a given vote option on a proposal.
func (k Keeper) updateTally(ctx context.Context, proposal *types.VotingProposal, optionIndex uint32) error {
	for i, t := range proposal.Tally {
		if t.OptionId == optionIndex {
			proposal.Tally[i].VoteCount++
			return k.VotingProposal.Set(ctx, proposal.Id, *proposal)
		}
	}
	return fmt.Errorf("option index %d not found in tally", optionIndex)
}

// getBlocksPerEpoch returns blocks per epoch from params.
func (k Keeper) getBlocksPerEpoch(ctx context.Context) int64 {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 17280 // default: ~1 day at 5s blocks
	}
	if params.BlocksPerEpoch > 0 {
		return int64(params.BlocksPerEpoch)
	}
	return 17280
}

// isModuleAccount checks if an address is a module account via the auth keeper.
func (k Keeper) isModuleAccount(ctx context.Context, addr sdk.AccAddress) bool {
	acc := k.authKeeper.GetAccount(ctx, addr)
	if acc == nil {
		return false
	}
	_, ok := acc.(sdk.ModuleAccountI)
	return ok
}

// validateProposalOptions validates vote options for a proposal creation message.
func (k Keeper) validateProposalOptions(params types.Params, options []*types.VoteOption) error {
	optCount := uint32(len(options))
	if optCount < params.MinVoteOptions || optCount > params.MaxVoteOptions {
		return types.ErrVoteOptionsOutOfRange
	}

	hasStandard := false
	abstainCount := 0
	vetoCount := 0
	for i, opt := range options {
		if opt.Id != uint32(i) {
			return types.ErrInvalidVoteOptions
		}
		switch opt.Role {
		case types.OptionRole_OPTION_ROLE_STANDARD:
			hasStandard = true
		case types.OptionRole_OPTION_ROLE_ABSTAIN:
			abstainCount++
		case types.OptionRole_OPTION_ROLE_VETO:
			vetoCount++
		}
	}

	if !hasStandard {
		return types.ErrNoStandardOption
	}
	if abstainCount > 1 {
		return types.ErrDuplicateAbstainRole
	}
	if vetoCount > 1 {
		return types.ErrDuplicateVetoRole
	}

	return nil
}

// buildTreeSnapshot builds a voter tree snapshot from active registrations.
func (k Keeper) buildTreeSnapshot(ctx context.Context) (root []byte, voterCount uint64, err error) {
	var zkPubKeys [][]byte
	err = k.VoterRegistration.Walk(ctx, nil, func(_ string, reg types.VoterRegistration) (bool, error) {
		if reg.Active {
			zkPubKeys = append(zkPubKeys, reg.ZkPublicKey)
		}
		return false, nil
	})
	if err != nil {
		return nil, 0, err
	}
	root, voterCount = buildMerkleTree(zkPubKeys)
	return root, voterCount, nil
}

// isZkPubKeyUnique checks if a ZK public key is already registered to another address.
func (k Keeper) isZkPubKeyUnique(ctx context.Context, zkPubKey []byte, excludeAddr string) (bool, error) {
	unique := true
	err := k.VoterRegistration.Walk(ctx, nil, func(addr string, reg types.VoterRegistration) (bool, error) {
		if addr != excludeAddr && reg.Active && bytes.Equal(reg.ZkPublicKey, zkPubKey) {
			unique = false
			return true, nil // stop iteration
		}
		return false, nil
	})
	return unique, err
}

// initTally creates an initial tally slice from vote options.
func initTally(options []*types.VoteOption) []*types.VoteTally {
	tally := make([]*types.VoteTally, len(options))
	for i, opt := range options {
		tally[i] = &types.VoteTally{
			OptionId:  opt.Id,
			VoteCount: 0,
		}
	}
	return tally
}
