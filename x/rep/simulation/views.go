package simulation

import (
	"sort"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// State views. Every read goes through the keeper's collections or getters;
// nothing here writes.

// memberAcc is a live sim account with its member record.
type memberAcc struct {
	acc simtypes.Account
	m   types.Member
}

// addr is the member's address.
func (ma memberAcc) addr() string { return ma.acc.Address.String() }

// member returns addr's member record.
func (e *simEnv) member(addr string) (types.Member, bool) {
	m, err := e.k.Member.Get(e.ctx, addr)
	return m, err == nil
}

// isMember reports whether addr holds any member record.
func (e *simEnv) isMember(addr string) bool {
	_, ok := e.member(addr)
	return ok
}

// unlocked is a member's DREAM not locked by stakes, bonds or escrow.
func unlocked(m types.Member) math.Int {
	return keeper.DerefInt(m.DreamBalance).Sub(keeper.DerefInt(m.StakedDream))
}

// members returns the live, ACTIVE members accepted by keep whose address is
// a sim account that can pay a fee.
func (e *simEnv) members(keep func(memberAcc) bool) ([]memberAcc, error) {
	var out []memberAcc
	err := e.k.Member.Walk(e.ctx, nil, func(addr string, m types.Member) (bool, error) {
		if m.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE {
			return false, nil
		}
		acc, ok := e.account(addr)
		if !ok || !e.spendable(acc).IsPositive() {
			return false, nil
		}
		ma := memberAcc{acc: acc, m: m}
		if keep == nil || keep(ma) {
			out = append(out, ma)
		}
		return false, nil
	})
	return out, err
}

// nonMembers returns the live sim accounts that hold no member record.
func (e *simEnv) nonMembers() []simtypes.Account {
	return e.live(func(a simtypes.Account) bool { return !e.isMember(a.Address.String()) })
}

// walkAll collects the values of a uint64-keyed map accepted by keep.
func walkAll[V any](e *simEnv, m collections.Map[uint64, V], keep func(V) bool) ([]V, error) {
	var out []V
	err := m.Walk(e.ctx, nil, func(_ uint64, v V) (bool, error) {
		if keep == nil || keep(v) {
			out = append(out, v)
		}
		return false, nil
	})
	return out, err
}

// projects returns the projects accepted by keep.
func (e *simEnv) projects(keep func(types.Project) bool) ([]types.Project, error) {
	return walkAll(e, e.k.Project, keep)
}

// initiatives returns the initiatives accepted by keep.
func (e *simEnv) initiatives(keep func(types.Initiative) bool) ([]types.Initiative, error) {
	return walkAll(e, e.k.Initiative, keep)
}

// interims returns the interims accepted by keep.
func (e *simEnv) interims(keep func(types.Interim) bool) ([]types.Interim, error) {
	return walkAll(e, e.k.Interim, keep)
}

// invitations returns the invitations accepted by keep.
func (e *simEnv) invitations(keep func(types.Invitation) bool) ([]types.Invitation, error) {
	return walkAll(e, e.k.Invitation, keep)
}

// stakes returns the stakes accepted by keep.
func (e *simEnv) stakes(keep func(types.Stake) bool) ([]types.Stake, error) {
	return walkAll(e, e.k.Stake, keep)
}

// challenges returns the challenges accepted by keep.
func (e *simEnv) challenges(keep func(types.Challenge) bool) ([]types.Challenge, error) {
	return walkAll(e, e.k.Challenge, keep)
}

// contentChallenges returns the content challenges accepted by keep.
func (e *simEnv) contentChallenges(keep func(types.ContentChallenge) bool) ([]types.ContentChallenge, error) {
	return walkAll(e, e.k.ContentChallenge, keep)
}

// juryReviews returns the jury reviews accepted by keep.
func (e *simEnv) juryReviews(keep func(types.JuryReview) bool) ([]types.JuryReview, error) {
	return walkAll(e, e.k.JuryReview, keep)
}

// tagBudgets returns the tag budgets accepted by keep.
func (e *simEnv) tagBudgets(keep func(types.TagBudget) bool) ([]types.TagBudget, error) {
	return walkAll(e, e.k.TagBudget, keep)
}

// migrations returns the member migrations accepted by keep.
func (e *simEnv) migrations(keep func(types.MemberMigration) bool) ([]types.MemberMigration, error) {
	return walkAll(e, e.k.MemberMigrations, keep)
}

// memberReports returns the member reports accepted by keep.
func (e *simEnv) memberReports(keep func(types.MemberReport) bool) ([]types.MemberReport, error) {
	var out []types.MemberReport
	err := e.k.MemberReport.Walk(e.ctx, nil, func(_ string, rep types.MemberReport) (bool, error) {
		if keep == nil || keep(rep) {
			out = append(out, rep)
		}
		return false, nil
	})
	return out, err
}

// tags returns the registered tag names.
func (e *simEnv) tags() ([]string, error) {
	var out []string
	err := e.k.Tag.Walk(e.ctx, nil, func(name string, _ types.Tag) (bool, error) {
		out = append(out, name)
		return false, nil
	})
	return out, err
}

// project returns a project.
func (e *simEnv) project(id uint64) (types.Project, bool) {
	p, err := e.k.Project.Get(e.ctx, id)
	return p, err == nil
}

// initiative returns an initiative.
func (e *simEnv) initiative(id uint64) (types.Initiative, bool) {
	in, err := e.k.Initiative.Get(e.ctx, id)
	return in, err == nil
}

// pendingInvitationTo reports whether addr has a PENDING invitation.
func (e *simEnv) pendingInvitationTo(addr string) (bool, error) {
	found := false
	err := e.k.Invitation.Walk(e.ctx, nil, func(_ uint64, inv types.Invitation) (bool, error) {
		if inv.InviteeAddress == addr && inv.Status == types.InvitationStatus_INVITATION_STATUS_PENDING {
			found = true
			return true, nil
		}
		return false, nil
	})
	return found, err
}

// tierConfig returns the configuration of an initiative tier.
func (e *simEnv) tierConfig(tier types.InitiativeTier) types.TierConfig {
	return keeper.TierConfigFor(e.params, tier)
}

// avgReputation is a member's average reputation over tags, the figure
// AssignInitiativeToMember checks against the tier minimum.
func avgReputation(m types.Member, tags []string) math.LegacyDec {
	sum := math.LegacyZeroDec()
	if len(tags) == 0 {
		if len(m.ReputationScores) == 0 {
			return sum
		}
		for _, v := range m.ReputationScores {
			if d, err := math.LegacyNewDecFromStr(v); err == nil {
				sum = sum.Add(d)
			}
		}
		return sum.QuoInt64(int64(len(m.ReputationScores)))
	}
	for _, t := range tags {
		if d, err := math.LegacyNewDecFromStr(m.ReputationScores[t]); err == nil {
			sum = sum.Add(d)
		}
	}
	return sum.QuoInt64(int64(len(tags)))
}

// repTags returns the tags a member holds reputation in.
func repTags(m types.Member) []string {
	var out []string
	for t, v := range m.ReputationScores {
		if d, err := math.LegacyNewDecFromStr(v); err == nil && d.IsPositive() {
			out = append(out, t)
		}
	}
	sort.Strings(out) // map order is random; the simulation must replay
	return out
}

// accAddr parses a bech32 address (sim addresses always parse).
func accAddr(addr string) sdk.AccAddress {
	a, err := sdk.AccAddressFromBech32(addr)
	if err != nil {
		return nil
	}
	return a
}
