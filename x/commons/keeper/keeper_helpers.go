package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/group"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"
)

// GetAuthority returns the module's authority.
func (k Keeper) GetAuthority() []byte {
	return k.authority
}

func (k Keeper) GetAuthorityString() string {
	addr, _ := k.addressCodec.BytesToString(k.authority)
	return addr
}

// GetGroupKeeper exposes the internal groupKeeper for simulation/testing purposes
func (k Keeper) GetGroupKeeper() groupkeeper.Keeper {
	return k.groupKeeper
}

// GetModuleAddress returns the address of the x/commons module account
func (k Keeper) GetModuleAddress() sdk.AccAddress {
	return k.authKeeper.GetModuleAddress(types.ModuleName)
}

// GetModuleAddressByName returns the address of a specific module by name (e.g. "gov", "distribution")
func (k Keeper) GetModuleAddressByName(name string) sdk.AccAddress {
	return k.authKeeper.GetModuleAddress(name)
}

// --- Extended Group Helpers ---

func (k Keeper) GetExtendedGroup(ctx context.Context, name string) (types.ExtendedGroup, error) {
	return k.ExtendedGroup.Get(ctx, name)
}

func (k Keeper) SetExtendedGroup(ctx context.Context, name string, group types.ExtendedGroup) error {
	return k.ExtendedGroup.Set(ctx, name, group)
}

// --- Policy Permissions Helpers ---

func (k Keeper) GetPolicyPermissions(ctx context.Context, policyAddress string) (types.PolicyPermissions, error) {
	return k.PolicyPermissions.Get(ctx, policyAddress)
}

func (k Keeper) SetPolicyPermissions(ctx context.Context, policyAddress string, perms types.PolicyPermissions) error {
	return k.PolicyPermissions.Set(ctx, policyAddress, perms)
}

// detectCycle checks if a parent-child relationship would form a loop.
// Optimized to use the O(1) PolicyToName index.
func (k Keeper) DetectCycle(ctx sdk.Context, childPolicy string, parentPolicy string) (bool, error) {
	// 1. Immediate Self Check
	if childPolicy == parentPolicy {
		return true, nil
	}

	// 2. Ancestry Walk
	cursor := parentPolicy
	govAddr := k.authKeeper.GetModuleAddress(govtypes.ModuleName).String()

	// Safety limit
	for i := 0; i < 1000; i++ {
		// A. Check for Termination (Success)
		if cursor == govAddr || cursor == "" {
			return false, nil
		}

		// B. Check for Cycle (Fail)
		if cursor == childPolicy {
			return true, nil
		}

		// C. Move Up (Optimized Lookup)
		// 1. Get Group Name from Policy Address
		groupName, err := k.PolicyToName.Get(ctx, cursor)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				// Parent exists but is not an Extended Group (maybe a raw address or deleted).
				// Valid termination.
				return false, nil
			}
			return false, err
		}

		// 2. Get Group Object
		group, err := k.ExtendedGroup.Get(ctx, groupName)
		if err != nil {
			// This indicates database corruption (index exists but group doesn't),
			// but for logic flow it means we can't find the next parent.
			return false, err
		}

		cursor = group.ParentPolicyAddress
	}

	return true, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ancestry depth limit exceeded")
}

// --- Committee Helpers ---

// IsCommitteeMember checks if an address is a member of a specific committee in a council
func (k Keeper) IsCommitteeMember(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error) {
	// Map council and committee to actual group names used in genesis
	// Normalize council name (handle both "technical" and "Technical Council" formats)
	groupName := ""
	normalizedCouncil := council

	// Normalize lowercase council names
	switch strings.ToLower(council) {
	case "technical":
		normalizedCouncil = "Technical Council"
	case "ecosystem":
		normalizedCouncil = "Ecosystem Council"
	case "commons":
		normalizedCouncil = "Commons Council"
	}

	switch normalizedCouncil {
	case "Technical Council":
		switch committee {
		case "operations":
			groupName = "Technical Operations Committee"
		case "governance", "hr":
			groupName = "Technical Governance Committee"
		default:
			return false, fmt.Errorf("unknown committee '%s' for council '%s'", committee, council)
		}
	case "Ecosystem Council":
		switch committee {
		case "operations":
			groupName = "Ecosystem Operations Committee"
		case "governance", "hr":
			groupName = "Ecosystem Governance Committee"
		default:
			return false, fmt.Errorf("unknown committee '%s' for council '%s'", committee, council)
		}
	case "Commons Council":
		switch committee {
		case "operations":
			groupName = "Commons Operations Committee"
		case "governance", "hr":
			groupName = "Commons Governance Committee"
		default:
			return false, fmt.Errorf("unknown committee '%s' for council '%s'", committee, council)
		}
	default:
		// Fallback to old convention for backwards compatibility or unknown councils
		groupName = fmt.Sprintf("%s_%s", council, committee)
	}

	// Get ExtendedGroup to find GroupID
	extendedGroup, err := k.ExtendedGroup.Get(ctx, groupName)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return false, nil // Group doesn't exist
		}
		return false, err
	}

	// Check membership in x/group
	// We iterate through members since committees are small (< 20 members)
	iterator, err := k.groupKeeper.GroupMembers(ctx, &group.QueryGroupMembersRequest{
		GroupId: extendedGroup.GroupId,
	})
	if err != nil {
		return false, nil
	}

	for _, member := range iterator.Members {
		if member.Member.Address == address.String() {
			return true, nil
		}
	}

	return false, nil
}

// GetCommitteeGroupInfo returns the group info for a committee
func (k Keeper) GetCommitteeGroupInfo(ctx context.Context, council string, committee string) (interface{}, error) {
	// Use the same mapping logic as IsCommitteeMember
	groupName := ""

	switch council {
	case "Technical Council":
		switch committee {
		case "operations":
			groupName = "Technical Operations Committee"
		case "governance", "hr":
			groupName = "Technical Governance Committee"
		default:
			return nil, fmt.Errorf("unknown committee '%s' for council '%s'", committee, council)
		}
	case "Ecosystem Council":
		switch committee {
		case "operations":
			groupName = "Ecosystem Operations Committee"
		case "governance", "hr":
			groupName = "Ecosystem Governance Committee"
		default:
			return nil, fmt.Errorf("unknown committee '%s' for council '%s'", committee, council)
		}
	case "Commons Council":
		switch committee {
		case "operations":
			groupName = "Commons Operations Committee"
		case "governance", "hr":
			groupName = "Commons Governance Committee"
		default:
			return nil, fmt.Errorf("unknown committee '%s' for council '%s'", committee, council)
		}
	default:
		// Fallback to old convention for backwards compatibility
		groupName = fmt.Sprintf("%s_%s", council, committee)
	}

	extendedGroup, err := k.ExtendedGroup.Get(ctx, groupName)
	if err != nil {
		return nil, err
	}

	return extendedGroup, nil
}
