package keeper_test

import (
	"testing"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"
)

func TestCycleDetectionLogic(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	// SCENARIO: Create a ring: A -> B -> C -> A

	// 1. Setup Addresses
	addrA := sdk.AccAddress([]byte("policy_A____________")).String()
	addrB := sdk.AccAddress([]byte("policy_B____________")).String()
	addrC := sdk.AccAddress([]byte("policy_C____________")).String()

	// 2. Manually Stitch the Graph in State (Bypassing MsgServer)
	// Group A (Parent is B)
	require.NoError(t, k.Groups.Set(ctx, "GroupA", types.Group{PolicyAddress: addrA, ParentPolicyAddress: addrB}))
	require.NoError(t, k.PolicyToName.Set(ctx, addrA, "GroupA"))

	// Group B (Parent is C)
	require.NoError(t, k.Groups.Set(ctx, "GroupB", types.Group{PolicyAddress: addrB, ParentPolicyAddress: addrC}))
	require.NoError(t, k.PolicyToName.Set(ctx, addrB, "GroupB"))

	// Group C (Parent is Gov for now - Open Chain)
	govAddr := k.GetModuleAddressByName(govtypes.ModuleName).String()
	require.NoError(t, k.Groups.Set(ctx, "GroupC", types.Group{PolicyAddress: addrC, ParentPolicyAddress: govAddr}))
	require.NoError(t, k.PolicyToName.Set(ctx, addrC, "GroupC"))

	// 3. Test 1: No Cycle (Linear)
	// Check if A is in ancestry of C? (C -> Gov). No.
	hasCycle, err := k.DetectCycle(ctx, addrA, addrC)
	require.NoError(t, err)
	require.False(t, hasCycle, "A -> C should be valid (linear)")

	// 4. Test 2: Close the Loop (Cycle)
	// Update C to have Parent = A.
	// Now: C -> A -> B -> C ...
	require.NoError(t, k.Groups.Set(ctx, "GroupC", types.Group{PolicyAddress: addrC, ParentPolicyAddress: addrA}))

	// Trigger Check: Is C in ancestry of B?
	// Ancestry of B: B -> C -> A -> B ...
	// Since 'child' is C, and we ask if C is in B's ancestry, it should be YES.
	hasCycle, err = k.DetectCycle(ctx, addrC, addrB)
	require.NoError(t, err)
	require.True(t, hasCycle, "Cycle C->A->B->C should be detected")

	// 5. Test 3: Self Loop
	hasCycle, err = k.DetectCycle(ctx, addrA, addrA)
	require.NoError(t, err)
	require.True(t, hasCycle, "Self-loop should be detected")
}

// --- Member Management Tests ---

func TestAddMember_And_GetMember(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	council := "TestCouncil"
	addr := sdk.AccAddress([]byte("member_add_test_____")).String()

	member := types.Member{Address: addr, Weight: "5", Metadata: "test member"}
	require.NoError(t, k.AddMember(ctx, council, member))

	got, err := k.GetMember(ctx, council, addr)
	require.NoError(t, err)
	require.Equal(t, addr, got.Address)
	require.Equal(t, "5", got.Weight)
	require.Equal(t, "test member", got.Metadata)
}

func TestHasMember(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	council := "HasMemberCouncil"
	addr := sdk.AccAddress([]byte("member_has_test_____")).String()

	has, err := k.HasMember(ctx, council, addr)
	require.NoError(t, err)
	require.False(t, has)

	require.NoError(t, k.AddMember(ctx, council, types.Member{Address: addr, Weight: "1"}))

	has, err = k.HasMember(ctx, council, addr)
	require.NoError(t, err)
	require.True(t, has)
}

func TestRemoveMember(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	council := "RemoveCouncil"
	addr := sdk.AccAddress([]byte("member_rm_test______")).String()

	require.NoError(t, k.AddMember(ctx, council, types.Member{Address: addr, Weight: "1"}))

	has, _ := k.HasMember(ctx, council, addr)
	require.True(t, has)

	require.NoError(t, k.RemoveMember(ctx, council, addr))

	has, _ = k.HasMember(ctx, council, addr)
	require.False(t, has)
}

func TestGetCouncilMembers(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	council := "ListCouncil"
	addr1 := sdk.AccAddress([]byte("member1_list________")).String()
	addr2 := sdk.AccAddress([]byte("member2_list________")).String()

	require.NoError(t, k.AddMember(ctx, council, types.Member{Address: addr1, Weight: "1"}))
	require.NoError(t, k.AddMember(ctx, council, types.Member{Address: addr2, Weight: "2"}))

	members, err := k.GetCouncilMembers(ctx, council)
	require.NoError(t, err)
	require.Len(t, members, 2)
}

func TestGetCouncilMembers_Empty(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	members, err := k.GetCouncilMembers(ctx, "ghost_council")
	require.NoError(t, err)
	require.Empty(t, members)
}

func TestCountCouncilMembers(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	council := "CountCouncil"

	count, err := k.CountCouncilMembers(ctx, council)
	require.NoError(t, err)
	require.Equal(t, uint64(0), count)

	require.NoError(t, k.AddMember(ctx, council, types.Member{Address: "a1", Weight: "1"}))
	require.NoError(t, k.AddMember(ctx, council, types.Member{Address: "a2", Weight: "1"}))
	require.NoError(t, k.AddMember(ctx, council, types.Member{Address: "a3", Weight: "1"}))

	count, err = k.CountCouncilMembers(ctx, council)
	require.NoError(t, err)
	require.Equal(t, uint64(3), count)
}

func TestClearCouncilMembers(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	council := "ClearCouncil"
	require.NoError(t, k.AddMember(ctx, council, types.Member{Address: "a1", Weight: "1"}))
	require.NoError(t, k.AddMember(ctx, council, types.Member{Address: "a2", Weight: "1"}))

	count, _ := k.CountCouncilMembers(ctx, council)
	require.Equal(t, uint64(2), count)

	require.NoError(t, k.ClearCouncilMembers(ctx, council))

	count, _ = k.CountCouncilMembers(ctx, council)
	require.Equal(t, uint64(0), count)
}

func TestMemberIsolationBetweenCouncils(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	addr := sdk.AccAddress([]byte("shared_member_______")).String()

	require.NoError(t, k.AddMember(ctx, "CouncilX", types.Member{Address: addr, Weight: "1"}))
	require.NoError(t, k.AddMember(ctx, "CouncilY", types.Member{Address: addr, Weight: "5"}))

	// Same address, different weights per council
	memX, err := k.GetMember(ctx, "CouncilX", addr)
	require.NoError(t, err)
	require.Equal(t, "1", memX.Weight)

	memY, err := k.GetMember(ctx, "CouncilY", addr)
	require.NoError(t, err)
	require.Equal(t, "5", memY.Weight)

	// Removing from X doesn't affect Y
	require.NoError(t, k.RemoveMember(ctx, "CouncilX", addr))
	has, _ := k.HasMember(ctx, "CouncilX", addr)
	require.False(t, has)
	has, _ = k.HasMember(ctx, "CouncilY", addr)
	require.True(t, has)
}

// --- IsCouncilAuthorized Tests ---

func TestIsCouncilAuthorized_GovAuthority(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	// The authority in setupCommonsKeeper is sdk.AccAddress([]byte("authority"))
	authorityAddr := k.GetAuthorityString()
	require.True(t, k.IsCouncilAuthorized(ctx, authorityAddr, "any", "any"))
}

func TestIsCouncilAuthorized_PolicyAddress(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	policyAddr := sdk.AccAddress([]byte("council_policy______")).String()
	require.NoError(t, k.Groups.Set(ctx, "Commons Council", types.Group{PolicyAddress: policyAddr}))

	require.True(t, k.IsCouncilAuthorized(ctx, policyAddr, "commons", "any"))
}

func TestIsCouncilAuthorized_CommitteeMember(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	member := sdk.AccAddress([]byte("ops_committee_mbr___"))
	require.NoError(t, k.AddMember(ctx, "Commons Operations Committee", types.Member{
		Address: member.String(), Weight: "1",
	}))

	require.True(t, k.IsCouncilAuthorized(ctx, member.String(), "commons", "operations"))
}

func TestIsCouncilAuthorized_Unauthorized(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	random := sdk.AccAddress([]byte("random_addr_________")).String()
	require.False(t, k.IsCouncilAuthorized(ctx, random, "commons", "operations"))
}

// --- IsCommitteeMember Tests ---

func TestIsCommitteeMember_TechnicalOps(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	member := sdk.AccAddress([]byte("tech_ops_member_____"))
	require.NoError(t, k.AddMember(ctx, "Technical Operations Committee", types.Member{
		Address: member.String(), Weight: "1",
	}))

	isMember, err := k.IsCommitteeMember(ctx, member, "Technical Council", "operations")
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestIsCommitteeMember_UnknownCommittee(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	member := sdk.AccAddress([]byte("unknown_committee___"))
	_, err := k.IsCommitteeMember(ctx, member, "Technical Council", "nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown committee")
}

// --- Group/Policy Helper Tests ---

func TestGetGroup_SetGroup(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	group := types.Group{
		Index: "TestGroup", PolicyAddress: "policy1", FundingWeight: 100,
	}
	require.NoError(t, k.SetGroup(ctx, "TestGroup", group))

	got, err := k.GetGroup(ctx, "TestGroup")
	require.NoError(t, err)
	require.Equal(t, "policy1", got.PolicyAddress)
	require.Equal(t, uint64(100), got.FundingWeight)
}

func TestGetGroup_NotFound(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	_, err := k.GetGroup(ctx, "nonexistent")
	require.Error(t, err)
}

func TestGetPolicyPermissions_SetPolicyPermissions(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	perms := types.PolicyPermissions{
		PolicyAddress:   "policy_perms_test",
		AllowedMessages: []string{"/msg.A", "/msg.B"},
	}
	require.NoError(t, k.SetPolicyPermissions(ctx, "policy_perms_test", perms))

	got, err := k.GetPolicyPermissions(ctx, "policy_perms_test")
	require.NoError(t, err)
	require.Equal(t, 2, len(got.AllowedMessages))
}

// --- Policy Version Tests ---

func TestGetPolicyVersion_Default(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	v, err := k.GetPolicyVersion(ctx, "nonexistent_policy")
	require.NoError(t, err)
	require.Equal(t, uint64(0), v)
}

func TestBumpPolicyVersion(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	policyAddr := "bump_test_policy"

	v, err := k.BumpPolicyVersion(ctx, policyAddr)
	require.NoError(t, err)
	require.Equal(t, uint64(1), v)

	v, err = k.BumpPolicyVersion(ctx, policyAddr)
	require.NoError(t, err)
	require.Equal(t, uint64(2), v)

	got, err := k.GetPolicyVersion(ctx, policyAddr)
	require.NoError(t, err)
	require.Equal(t, uint64(2), got)
}

// --- IsGroupPolicyMember Tests ---

func TestIsGroupPolicyMember(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	policyAddr := "policy_gpm_test"
	memberAddr := sdk.AccAddress([]byte("gpm_member__________")).String()
	councilName := "GPMCouncil"

	require.NoError(t, k.PolicyToName.Set(ctx, policyAddr, councilName))
	require.NoError(t, k.AddMember(ctx, councilName, types.Member{Address: memberAddr, Weight: "1"}))

	isMember, err := k.IsGroupPolicyMember(ctx, policyAddr, memberAddr)
	require.NoError(t, err)
	require.True(t, isMember)

	// Non-member
	isMember, err = k.IsGroupPolicyMember(ctx, policyAddr, "other_address")
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestIsGroupPolicyMember_UnknownPolicy(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	isMember, err := k.IsGroupPolicyMember(ctx, "unknown_policy", "some_addr")
	require.NoError(t, err)
	require.False(t, isMember)
}

// --- IsGroupPolicyAddress Tests ---

func TestIsGroupPolicyAddress(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	require.NoError(t, k.PolicyToName.Set(ctx, "valid_policy", "SomeCouncil"))

	require.True(t, k.IsGroupPolicyAddress(ctx, "valid_policy"))
	require.False(t, k.IsGroupPolicyAddress(ctx, "invalid_policy"))
}

// --- IsSiblingPolicy Tests ---

func TestIsSiblingPolicy_SameCouncil(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	require.NoError(t, k.PolicyToName.Set(ctx, "policyA", "SharedCouncil"))
	require.NoError(t, k.PolicyToName.Set(ctx, "policyB", "SharedCouncil"))

	require.True(t, k.IsSiblingPolicy(ctx, "policyA", "policyB"))
}

func TestIsSiblingPolicy_VetoPolicy(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	require.NoError(t, k.PolicyToName.Set(ctx, "mainPolicy", "SibCouncil"))
	require.NoError(t, k.PolicyToName.Set(ctx, "vetoPolicy", "SibCouncilVeto"))
	require.NoError(t, k.Groups.Set(ctx, "SibCouncil", types.Group{PolicyAddress: "mainPolicy"}))
	require.NoError(t, k.VetoPolicies.Set(ctx, "SibCouncil", "vetoPolicy"))

	require.True(t, k.IsSiblingPolicy(ctx, "vetoPolicy", "mainPolicy"))
}

func TestIsSiblingPolicy_DifferentCouncils(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	require.NoError(t, k.PolicyToName.Set(ctx, "policyX", "CouncilX"))
	require.NoError(t, k.PolicyToName.Set(ctx, "policyY", "CouncilY"))
	require.NoError(t, k.Groups.Set(ctx, "CouncilX", types.Group{PolicyAddress: "policyX"}))
	require.NoError(t, k.Groups.Set(ctx, "CouncilY", types.Group{PolicyAddress: "policyY"}))

	require.False(t, k.IsSiblingPolicy(ctx, "policyX", "policyY"))
}

// --- normalizeCouncilName Tests (via IsCouncilAuthorized) ---

func TestNormalizeCouncilName_Shortcuts(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	// Set up councils with their full names
	policyAddrTech := sdk.AccAddress([]byte("policy_tech_________")).String()
	policyAddrEco := sdk.AccAddress([]byte("policy_eco__________")).String()
	policyAddrComm := sdk.AccAddress([]byte("policy_comm_________")).String()

	require.NoError(t, k.Groups.Set(ctx, "Technical Council", types.Group{PolicyAddress: policyAddrTech}))
	require.NoError(t, k.Groups.Set(ctx, "Ecosystem Council", types.Group{PolicyAddress: policyAddrEco}))
	require.NoError(t, k.Groups.Set(ctx, "Commons Council", types.Group{PolicyAddress: policyAddrComm}))

	// Short names should resolve to policy addresses
	require.True(t, k.IsCouncilAuthorized(ctx, policyAddrTech, "technical", ""))
	require.True(t, k.IsCouncilAuthorized(ctx, policyAddrEco, "ecosystem", ""))
	require.True(t, k.IsCouncilAuthorized(ctx, policyAddrComm, "commons", ""))
}

// --- DeriveCouncilAddress Tests ---

func TestDeriveCouncilAddress_Deterministic(t *testing.T) {
	addr1 := keeper.DeriveCouncilAddress(1, "standard")
	addr2 := keeper.DeriveCouncilAddress(1, "standard")
	require.Equal(t, addr1, addr2)

	addr3 := keeper.DeriveCouncilAddress(2, "standard")
	require.NotEqual(t, addr1, addr3)

	addr4 := keeper.DeriveCouncilAddress(1, "veto")
	require.NotEqual(t, addr1, addr4)
}
