package keeper

import (
	"context"
	"errors"
	"fmt"

	"sparkdream/x/name/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/group"
)

// --- Params Helper ---

func (k Keeper) GetParams(ctx context.Context) types.Params {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return types.DefaultParams()
	}
	return params
}

// --- Dispute Helpers ---

func (k Keeper) SetDispute(ctx context.Context, dispute types.Dispute) error {
	return k.Disputes.Set(ctx, dispute.Name, dispute)
}

func (k Keeper) GetDispute(ctx context.Context, name string) (types.Dispute, bool) {
	dispute, err := k.Disputes.Get(ctx, name)
	if err != nil {
		return types.Dispute{}, false
	}
	return dispute, true
}

func (k Keeper) RemoveDispute(ctx context.Context, name string) error {
	return k.Disputes.Remove(ctx, name)
}

// --- Name Helpers ---

func (k Keeper) GetNameOwner(ctx context.Context, name string) (sdk.AccAddress, bool) {
	record, err := k.Names.Get(ctx, name)
	if err != nil {
		return nil, false
	}
	addr, err := sdk.AccAddressFromBech32(record.Owner)
	if err != nil {
		return nil, false
	}
	return addr, true
}

func (k Keeper) GetName(ctx context.Context, name string) (types.NameRecord, bool) {
	record, err := k.Names.Get(ctx, name)
	if err != nil {
		return types.NameRecord{}, false
	}
	return record, true
}

func (k Keeper) SetName(ctx context.Context, record types.NameRecord) error {
	return k.Names.Set(ctx, record.Name, record)
}

func (k Keeper) RemoveNameFromOwner(ctx context.Context, owner sdk.AccAddress, name string) error {
	// Remove from Secondary Index
	return k.OwnerNames.Remove(ctx, collections.Join(owner.String(), name))
}

func (k Keeper) AddNameToOwner(ctx context.Context, owner sdk.AccAddress, name string) error {
	// 1. Add to Secondary Index (Efficient Lookup)
	if err := k.OwnerNames.Set(ctx, collections.Join(owner.String(), name)); err != nil {
		return err
	}

	// 2. Ensure OwnerInfo exists for metadata (LastActiveTime, PrimaryName)
	_, err := k.Owners.Get(ctx, owner.String())
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		// Initialize if not found
		info := types.OwnerInfo{Address: owner.String()}
		return k.Owners.Set(ctx, owner.String(), info)
	}
	return nil
}

// --- Helper Implementations ---

// GetOwnedNamesCount counts how many names an address owns using the secondary index.
// Optimization: Uses O(M) prefix iteration where M is user's name count (max ~5).
func (k Keeper) GetOwnedNamesCount(ctx context.Context, owner sdk.AccAddress) (uint64, error) {
	var count uint64

	// Create a range that matches all pairs starting with owner address
	rng := collections.NewPrefixedPairRange[string, string](owner.String())

	// Walk only the relevant keys
	err := k.OwnerNames.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
		count++
		return false, nil
	})

	return count, err
}

// SetPrimaryName sets the primary name in the OwnerInfo struct.
func (k Keeper) SetPrimaryName(ctx context.Context, owner sdk.AccAddress, name string) error {
	info, err := k.Owners.Get(ctx, owner.String())
	if err != nil {
		info = types.OwnerInfo{Address: owner.String()}
	}
	info.PrimaryName = name
	return k.Owners.Set(ctx, owner.String(), info)
}

// GetLastActiveTime retrieves the last active timestamp for an address.
func (k Keeper) GetLastActiveTime(ctx context.Context, owner sdk.AccAddress) int64 {
	info, err := k.Owners.Get(ctx, owner.String())
	if err != nil {
		return 0
	}
	return info.LastActiveTime
}

// --- Council Helper ---

// GetCouncilAddress retrieves the "standard" Policy Address of the Commons Council.
func (k Keeper) GetCouncilAddress(ctx context.Context, groupID uint64) (sdk.AccAddress, error) {
	req := &group.QueryGroupPoliciesByGroupRequest{
		GroupId: groupID,
	}

	res, err := k.groupKeeper.GroupPoliciesByGroup(ctx, req)
	if err != nil {
		return nil, err
	}

	if len(res.GroupPolicies) == 0 {
		return nil, fmt.Errorf("no policies found for council group %d", groupID)
	}

	// Search for the "standard" policy
	for _, policy := range res.GroupPolicies {
		if policy.Metadata == "standard" {
			return sdk.AccAddressFromBech32(policy.Address)
		}
	}

	return nil, fmt.Errorf("council policy 'standard' not found for group %d", groupID)
}
