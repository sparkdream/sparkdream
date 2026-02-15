package keeper

import (
	"bytes"
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

// --- Authority Helpers ---

// IsGovAuthority checks if the given address is the governance authority.
func (k Keeper) IsGovAuthority(addr string) bool {
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false
	}
	return bytes.Equal(k.authority, addrBytes)
}

// IsCouncilAuthorized checks if the address is authorized via governance authority,
// council policy address, or committee membership.
// Delegates to x/commons IsCouncilAuthorized when available.
// Falls back to IsGovAuthority when x/commons is not wired.
func (k Keeper) isCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool {
	if k.commonsKeeper == nil {
		return k.IsGovAuthority(addr)
	}
	return k.commonsKeeper.IsCouncilAuthorized(ctx, addr, council, committee)
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

// IsCommonsCouncilMember checks if the provided address is a member of the "Commons Council" group.
func (k Keeper) IsCommonsCouncilMember(ctx context.Context, memberAddr string) (bool, error) {
	// 1. Get the actual Group ID of the Commons Council from the x/commons module.
	// NOTE: We assume k.commonsKeeper is wired to the x/commons keeper.
	councilGroup, err := k.commonsKeeper.GetExtendedGroup(ctx, "Commons Council")
	if err != nil {
		// If the group isn't found, it's a critical setup error.
		return false, errors.New("critical: failed to find Commons Council group")
	}

	// 2. Use the GroupKeeper to check if the creator is a member of that specific Group ID.
	groupReq := &group.QueryGroupsByMemberRequest{
		Address: memberAddr,
	}
	// We use the gRPC query method exposed by the GroupKeeper interface
	groupRes, err := k.groupKeeper.GroupsByMember(ctx, groupReq)
	if err != nil {
		return false, err
	}

	for _, g := range groupRes.Groups {
		// Compare the group ID of the member's groups with the Commons Council Group ID
		if g.Id == councilGroup.GroupId {
			return true, nil
		}
	}

	return false, nil
}
