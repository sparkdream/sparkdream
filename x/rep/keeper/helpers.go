package keeper

import (
	"bytes"
	"context"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// Helper functions for working with proto pointer types

// DerefInt safely dereferences a *math.Int, returning zero if nil
func DerefInt(i *math.Int) math.Int {
	if i == nil {
		return math.ZeroInt()
	}
	return *i
}

// DerefDec safely dereferences a *math.LegacyDec, returning zero if nil
func DerefDec(d *math.LegacyDec) math.LegacyDec {
	if d == nil {
		return math.LegacyZeroDec()
	}
	return *d
}

// PtrInt returns a pointer to the given math.Int
func PtrInt(i math.Int) *math.Int {
	return &i
}

// PtrDec returns a pointer to the given math.LegacyDec
func PtrDec(d math.LegacyDec) *math.LegacyDec {
	return &d
}

// ZeroInt returns a zero math.Int (convenience helper)
func ZeroInt() math.Int {
	return math.ZeroInt()
}

// IsAffiliated checks if an address is affiliated with an initiative.
// This is a basic check without project creator lookup. Prefer IsAffiliatedWithProject
// when a keeper is available for full affiliation checking.
//
// The author of the initiative counts as affiliated. Under a permissionless
// project anyone may create an initiative, so the author is frequently not the
// project creator — an affiliation test that looks only at assignee, apprentice
// and project creator therefore treats the person who wrote the initiative as a
// disinterested outsider.
func IsAffiliated(initiative types.Initiative, addr string) bool {
	if addr == "" {
		return false
	}
	if initiative.Assignee == addr {
		return true
	}
	if initiative.Apprentice == addr {
		return true
	}
	if initiative.Creator == addr {
		return true
	}
	return false
}

// IsSelfAssigned reports whether the assignee is judging their own commission:
// they either authored the initiative or created the project it sits under.
//
// This is the trigger for all three self-assignment safeguards — the DREAM bond
// (AssignInitiativeToMember), the raised external-conviction floor
// (CanCompleteInitiative) and the extended challenge window
// (TransitionToChallengePeriod). Each of those used to test
// `assignee == project.Creator` alone, which the permissionless path walks
// straight past: CreateInitiative does not require the author to own the
// project, and AssignInitiative authorises any self-assignment, so creating an
// initiative under somebody else's project and taking it yourself cleared all
// three at once.
//
// An empty assignee (an unassigned initiative) is never self-assigned.
func IsSelfAssigned(initiative types.Initiative, project types.Project, assignee string) bool {
	if assignee == "" {
		return false
	}
	return assignee == initiative.Creator || assignee == project.Creator
}

// InitiativeAffiliates returns every address with an insider stake in an
// initiative's outcome: the assignee, the apprentice, the author, and the
// creator of the parent project. Empty entries are omitted so a record that
// predates a field cannot match an empty address.
func InitiativeAffiliates(initiative types.Initiative, project types.Project) []string {
	addrs := make([]string, 0, 4)
	for _, a := range []string{initiative.Assignee, initiative.Apprentice, initiative.Creator, project.Creator} {
		if a != "" {
			addrs = append(addrs, a)
		}
	}
	return addrs
}

// IsAffiliatedWithProject checks if an address is affiliated with an initiative,
// including checking whether the address is the project creator.
func (k Keeper) IsAffiliatedWithProject(ctx context.Context, initiative types.Initiative, addr string) bool {
	if IsAffiliated(initiative, addr) {
		return true
	}
	// Check project creator
	project, err := k.GetProject(ctx, initiative.ProjectId)
	if err == nil && project.Creator == addr {
		return true
	}
	return false
}

// IsGovAuthority checks if the given address is the governance authority.
func (k Keeper) IsGovAuthority(addr string) bool {
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false
	}
	return bytes.Equal(k.GetAuthority(), addrBytes)
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

// IsOperationsCommittee checks if an address is a member of the Operations committee
// Returns false if commonsKeeper is not available (optional dependency).
func (k Keeper) IsOperationsCommittee(ctx context.Context, address sdk.AccAddress) bool {
	if k.commonsKeeper == nil {
		return false // Fallback when x/commons not wired
	}

	// Check Technical Council -> Operations Committee
	isMember, err := k.commonsKeeper.IsCommitteeMember(ctx, address, "technical", "operations")
	if err == nil && isMember {
		return true
	}

	// Fallback: Check Commons Council -> Operations Committee
	isMember, err = k.commonsKeeper.IsCommitteeMember(ctx, address, "commons", "operations")
	if err == nil && isMember {
		return true
	}

	return false
}

// IsGroupAccount returns true when addr is a known group-policy address.
// Falls back to true when commonsKeeper is not wired so unit tests with a nil
// commons keeper can exercise tag-budget logic without stubbing groups.
func (k Keeper) IsGroupAccount(ctx context.Context, addr string) bool {
	if k.commonsKeeper == nil {
		return true
	}
	return k.commonsKeeper.IsGroupPolicyAddress(ctx, addr)
}

// IsGroupMember returns true when addr is a member of the group identified by
// policyAddr. Falls back to true when commonsKeeper is not wired (see
// IsGroupAccount).
func (k Keeper) IsGroupMember(ctx context.Context, policyAddr, addr string) bool {
	if k.commonsKeeper == nil {
		return true
	}
	isMember, err := k.commonsKeeper.IsGroupPolicyMember(ctx, policyAddr, addr)
	if err != nil {
		return false
	}
	return isMember
}

// IsHRCommittee checks if an address is a member of the HR committee
// Returns false if commonsKeeper is not available (optional dependency).
func (k Keeper) IsHRCommittee(ctx context.Context, address sdk.AccAddress) bool {
	if k.commonsKeeper == nil {
		return false // Fallback when x/commons not wired
	}

	// Check Commons Council -> HR Committee
	isMember, err := k.commonsKeeper.IsCommitteeMember(ctx, address, "commons", "hr")
	if err == nil && isMember {
		return true
	}
	return false
}

// GetReputationForTags calculates the average reputation for a member across specific tags.
// If no tags are provided or the member has no reputation in those tags, returns zero.
// This is used for tag-weighted conviction calculation where only relevant skills matter.
func (k Keeper) GetReputationForTags(ctx context.Context, memberAddr sdk.AccAddress, tags []string) (math.LegacyDec, error) {
	if len(tags) == 0 {
		return math.LegacyZeroDec(), nil
	}

	// GetMember applies both reputation decay and DREAM decay lazily
	member, err := k.GetMember(ctx, memberAddr)
	if err != nil {
		return math.LegacyZeroDec(), err
	}

	totalRep := math.LegacyZeroDec()
	matchedTags := 0

	for _, tag := range tags {
		if repStr, exists := member.ReputationScores[tag]; exists {
			rep, err := math.LegacyNewDecFromStr(repStr)
			if err != nil {
				continue // Skip malformed reputation values
			}
			totalRep = totalRep.Add(rep)
			matchedTags++
		}
	}

	if matchedTags == 0 {
		return math.LegacyZeroDec(), nil
	}

	// Return average reputation across matched tags
	return totalRep.QuoInt64(int64(matchedTags)), nil
}

// IncrementMemberCompletedInterims increments the cached completed interims count for a member.
// This is called when an interim is completed to maintain the O(1) lookup for trust level calculations.
func (k Keeper) IncrementMemberCompletedInterims(ctx context.Context, memberAddr sdk.AccAddress) error {
	member, err := k.Member.Get(ctx, memberAddr.String())
	if err != nil {
		return err
	}

	member.CompletedInterimsCount++

	return k.Member.Set(ctx, memberAddr.String(), member)
}

// IncrementMemberCompletedInitiatives increments the cached completed initiatives count for a member.
// This is called when an initiative is completed to maintain the O(1) lookup.
func (k Keeper) IncrementMemberCompletedInitiatives(ctx context.Context, memberAddr sdk.AccAddress) error {
	member, err := k.Member.Get(ctx, memberAddr.String())
	if err != nil {
		return err
	}

	member.CompletedInitiativesCount++

	return k.Member.Set(ctx, memberAddr.String(), member)
}
