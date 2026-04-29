package keeper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// GetAuthority returns the module's authority.
func (k Keeper) GetAuthority() []byte {
	return k.authority
}

func (k Keeper) GetAuthorityString() string {
	addr, _ := k.addressCodec.BytesToString(k.authority)
	return addr
}

// normalizeCouncilName converts short council names to their full form.
func normalizeCouncilName(council string) string {
	switch strings.ToLower(council) {
	case "technical":
		return "Technical Council"
	case "ecosystem":
		return "Ecosystem Council"
	case "commons":
		return "Commons Council"
	default:
		return council
	}
}

// resolveCommitteeName returns the full committee group name for a council/committee pair.
func resolveCommitteeName(council string, committee string) string {
	normalizedCouncil := normalizeCouncilName(council)
	switch normalizedCouncil {
	case "Technical Council":
		switch committee {
		case "operations":
			return "Technical Operations Committee"
		case "governance", "hr":
			return "Technical Governance Committee"
		}
	case "Ecosystem Council":
		switch committee {
		case "operations":
			return "Ecosystem Operations Committee"
		case "governance", "hr":
			return "Ecosystem Governance Committee"
		}
	case "Commons Council":
		switch committee {
		case "operations":
			return "Commons Operations Committee"
		case "governance", "hr":
			return "Commons Governance Committee"
		}
	}
	return ""
}

// IsCouncilAuthorized checks if the given address is authorized to act on behalf
// of a council/committee.
func (k Keeper) IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool {
	// 1. Check governance authority
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false
	}
	if bytes.Equal(k.authority, addrBytes) {
		return true
	}

	// 2. Check council policy address
	councilGroup, err := k.Groups.Get(ctx, normalizeCouncilName(council))
	if err == nil && councilGroup.PolicyAddress == addr {
		return true
	}

	// 3. Check committee policy address
	if committee != "" {
		committeeName := resolveCommitteeName(council, committee)
		if committeeName != "" {
			committeeGroup, err := k.Groups.Get(ctx, committeeName)
			if err == nil && committeeGroup.PolicyAddress == addr {
				return true
			}
		}
	}

	// 4. Check committee membership
	isMember, err := k.IsCommitteeMember(ctx, sdk.AccAddress(addrBytes), council, committee)
	if err == nil && isMember {
		return true
	}

	return false
}

// IsCouncilPolicyOrGov returns true only if addr is the gov authority or the
// council's policy address. Unlike IsCouncilAuthorized, individual committee
// membership does NOT satisfy this check — it is for handlers that require an
// actual council vote (executed via MsgExecuteProposal whose signer is the
// policy address) or governance.
func (k Keeper) IsCouncilPolicyOrGov(ctx context.Context, addr string, council string) bool {
	// gov authority
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false
	}
	if bytes.Equal(k.authority, addrBytes) {
		return true
	}

	// council policy address (the address that signs executed council proposals)
	councilGroup, err := k.Groups.Get(ctx, normalizeCouncilName(council))
	if err != nil {
		return false
	}
	return councilGroup.PolicyAddress == addr
}

// GetModuleAddress returns the address of the x/commons module account
func (k Keeper) GetModuleAddress() sdk.AccAddress {
	return k.authKeeper.GetModuleAddress(types.ModuleName)
}

// GetModuleAddressByName returns the address of a specific module by name
func (k Keeper) GetModuleAddressByName(name string) sdk.AccAddress {
	return k.authKeeper.GetModuleAddress(name)
}

// --- Extended Group Helpers ---

func (k Keeper) GetGroup(ctx context.Context, name string) (types.Group, error) {
	return k.Groups.Get(ctx, name)
}

func (k Keeper) SetGroup(ctx context.Context, name string, group types.Group) error {
	return k.Groups.Set(ctx, name, group)
}

// --- Policy Permissions Helpers ---

func (k Keeper) GetPolicyPermissions(ctx context.Context, policyAddress string) (types.PolicyPermissions, error) {
	return k.PolicyPermissions.Get(ctx, policyAddress)
}

func (k Keeper) SetPolicyPermissions(ctx context.Context, policyAddress string, perms types.PolicyPermissions) error {
	return k.PolicyPermissions.Set(ctx, policyAddress, perms)
}

// DetectCycle checks if a parent-child relationship would form a loop.
func (k Keeper) DetectCycle(ctx sdk.Context, childPolicy string, parentPolicy string) (bool, error) {
	if childPolicy == parentPolicy {
		return true, nil
	}

	cursor := parentPolicy
	govAddr := k.authKeeper.GetModuleAddress(types.GovModuleName).String()

	for i := 0; i < 1000; i++ {
		if cursor == govAddr || cursor == "" {
			return false, nil
		}
		if cursor == childPolicy {
			return true, nil
		}

		groupName, err := k.PolicyToName.Get(ctx, cursor)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return false, nil
			}
			return false, err
		}

		group, err := k.Groups.Get(ctx, groupName)
		if err != nil {
			return false, err
		}

		cursor = group.ParentPolicyAddress
	}

	return true, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ancestry depth limit exceeded")
}

// --- Member Management Helpers (replacing x/group) ---

// AddMember adds a member to a council in the native Members collection.
func (k Keeper) AddMember(ctx context.Context, councilName string, member types.Member) error {
	return k.Members.Set(ctx, collections.Join(councilName, member.Address), member)
}

// RemoveMember removes a member from a council.
func (k Keeper) RemoveMember(ctx context.Context, councilName string, address string) error {
	return k.Members.Remove(ctx, collections.Join(councilName, address))
}

// HasMember checks if an address is a member of a council.
func (k Keeper) HasMember(ctx context.Context, councilName string, address string) (bool, error) {
	return k.Members.Has(ctx, collections.Join(councilName, address))
}

// GetMember returns a member of a council.
func (k Keeper) GetMember(ctx context.Context, councilName string, address string) (types.Member, error) {
	return k.Members.Get(ctx, collections.Join(councilName, address))
}

// GetCouncilMembers returns all members of a council.
func (k Keeper) GetCouncilMembers(ctx context.Context, councilName string) ([]types.Member, error) {
	var members []types.Member
	rng := collections.NewPrefixedPairRange[string, string](councilName)
	err := k.Members.Walk(ctx, rng, func(key collections.Pair[string, string], member types.Member) (bool, error) {
		members = append(members, member)
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return members, nil
}

// CountCouncilMembers returns the number of members in a council.
func (k Keeper) CountCouncilMembers(ctx context.Context, councilName string) (uint64, error) {
	var count uint64
	rng := collections.NewPrefixedPairRange[string, string](councilName)
	err := k.Members.Walk(ctx, rng, func(_ collections.Pair[string, string], _ types.Member) (bool, error) {
		count++
		return false, nil
	})
	return count, err
}

// ClearCouncilMembers removes all members from a council.
func (k Keeper) ClearCouncilMembers(ctx context.Context, councilName string) error {
	rng := collections.NewPrefixedPairRange[string, string](councilName)
	return k.Members.Walk(ctx, rng, func(key collections.Pair[string, string], _ types.Member) (bool, error) {
		if err := k.Members.Remove(ctx, key); err != nil {
			return true, err
		}
		return false, nil
	})
}

// --- Committee Helpers ---

// IsCommitteeMember checks if an address is a member of a specific committee in a council.
// Now uses native Members collection instead of x/group.
func (k Keeper) IsCommitteeMember(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error) {
	groupName := ""
	normalizedCouncil := normalizeCouncilName(council)

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
		groupName = fmt.Sprintf("%s_%s", council, committee)
	}

	// Check membership via native Members collection — O(1) lookup
	has, err := k.Members.Has(ctx, collections.Join(groupName, address.String()))
	if err != nil {
		return false, err
	}
	return has, nil
}

// GetCommitteeGroupInfo returns the group info for a committee
func (k Keeper) GetCommitteeGroupInfo(ctx context.Context, council string, committee string) (interface{}, error) {
	groupName := ""

	switch normalizeCouncilName(council) {
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
		groupName = fmt.Sprintf("%s_%s", council, committee)
	}

	group, err := k.Groups.Get(ctx, groupName)
	if err != nil {
		return nil, err
	}

	return group, nil
}

// --- Group Policy Helpers (for cross-module integration) ---

// IsGroupPolicyMember checks if a member address is part of the group associated with
// a given policy address. Now uses native Members collection.
func (k Keeper) IsGroupPolicyMember(ctx context.Context, policyAddr string, memberAddr string) (bool, error) {
	groupName, err := k.PolicyToName.Get(ctx, policyAddr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return false, nil
		}
		return false, err
	}

	has, err := k.Members.Has(ctx, collections.Join(groupName, memberAddr))
	if err != nil {
		return false, nil
	}
	return has, nil
}

// IsGroupPolicyAddress checks if a given address is a valid group policy address.
func (k Keeper) IsGroupPolicyAddress(ctx context.Context, addr string) bool {
	_, err := k.PolicyToName.Get(ctx, addr)
	return err == nil
}

// IsSiblingPolicy checks if two policy addresses belong to the same council (are siblings).
// This replaces the x/group GroupPolicyInfo check for shared GroupId.
func (k Keeper) IsSiblingPolicy(ctx context.Context, policyA string, policyB string) bool {
	nameA, errA := k.PolicyToName.Get(ctx, policyA)
	nameB, errB := k.PolicyToName.Get(ctx, policyB)
	if errA != nil || errB != nil {
		return false
	}
	// Two policies are siblings if they map to the same council name
	// OR if one is the veto policy of the other's council
	if nameA == nameB {
		return true
	}

	// Check if policyA is the veto policy of nameB's council or vice versa
	groupB, err := k.Groups.Get(ctx, nameB)
	if err == nil {
		vetoAddr, err := k.VetoPolicies.Get(ctx, nameB)
		if err == nil && vetoAddr == policyA {
			return true
		}
		_ = groupB
	}

	groupA, err := k.Groups.Get(ctx, nameA)
	if err == nil {
		vetoAddr, err := k.VetoPolicies.Get(ctx, nameA)
		if err == nil && vetoAddr == policyB {
			return true
		}
		_ = groupA
	}

	return false
}

// GetPolicyVersion returns the current policy version for a policy address.
func (k Keeper) GetPolicyVersion(ctx context.Context, policyAddr string) (uint64, error) {
	version, err := k.PolicyVersion.Get(ctx, policyAddr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return version, nil
}

// BumpPolicyVersion increments the policy version, invalidating all pending proposals.
func (k Keeper) BumpPolicyVersion(ctx context.Context, policyAddr string) (uint64, error) {
	current, err := k.GetPolicyVersion(ctx, policyAddr)
	if err != nil {
		return 0, err
	}
	newVersion := current + 1
	return newVersion, k.PolicyVersion.Set(ctx, policyAddr, newVersion)
}

// parseMembers converts parallel string arrays into Member structs.
func (k Keeper) parseMembers(addresses []string, weights []string) ([]types.Member, error) {
	if len(addresses) != len(weights) {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "member count (%d) does not match weight count (%d)", len(addresses), len(weights))
	}

	var members []types.Member
	for i, addr := range addresses {
		if _, err := sdk.AccAddressFromBech32(addr); err != nil {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid member address %s: %s", addr, err)
		}
		members = append(members, types.Member{
			Address:  addr,
			Weight:   weights[i],
			Metadata: "",
		})
	}
	return members, nil
}
