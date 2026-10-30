package simulation

import (
	"cosmossdk.io/math"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// Member reports. A reporter (or co-signer) needs reputation tier 3 (200
// total reputation) and at least DefaultMinSentinelBond of locked DREAM.

// totalReputation sums a member's reputation scores.
func totalReputation(m types.Member) math.LegacyDec {
	sum := math.LegacyZeroDec()
	for _, t := range repTags(m) {
		if d, err := math.LegacyNewDecFromStr(m.ReputationScores[t]); err == nil {
			sum = sum.Add(d)
		}
	}
	return sum
}

// reporterTier reports whether m clears the reporter reputation tier.
func reporterTier(m types.Member) bool {
	return totalReputation(m).GTE(math.LegacyNewDec(200))
}

// canReport reports whether m may file or co-sign a member report.
func canReport(m types.Member) bool {
	return reporterTier(m) && keeper.DerefInt(m.StakedDream).GTE(types.DefaultMinSentinelBond) &&
		unlocked(m).GTE(types.DefaultMaxMemberReportBond.MulRaw(2))
}

// reporters returns the live members outside exclude that may report,
// staking the missing sentinel bond for one high-reputation member first (a
// real MsgStake on a tag) when none can.
func (e *simEnv) reporters(exclude map[string]bool) ([]memberAcc, error) {
	eligible := func(ma memberAcc) bool { return !exclude[ma.addr()] && canReport(ma.m) }
	rs, err := e.members(eligible)
	if err != nil || len(rs) > 0 {
		return rs, err
	}
	tags, err := e.tags()
	if err != nil {
		return nil, err
	}
	tag, ok := pick(e.r, tags)
	if !ok {
		return nil, nil
	}
	cands, err := e.members(func(ma memberAcc) bool {
		need := types.DefaultMinSentinelBond.Sub(keeper.DerefInt(ma.m.StakedDream))
		return !exclude[ma.addr()] && reporterTier(ma.m) && need.IsPositive() &&
			unlocked(ma.m).GTE(need.Add(types.DefaultMaxMemberReportBond.MulRaw(4)))
	})
	if err != nil {
		return nil, err
	}
	for n, i := range e.r.Perm(len(cands)) {
		if n >= maxPrereqTries {
			break
		}
		ma := cands[i]
		need := types.DefaultMinSentinelBond.Sub(keeper.DerefInt(ma.m.StakedDream)).Add(dream(1))
		ok, err := e.ensure(ma.acc, &types.MsgStake{
			Staker: ma.addr(), TargetType: types.StakeTargetType_STAKE_TARGET_TAG, TargetIdentifier: tag, Amount: PtrInt(need),
		}, math.ZeroInt())
		if err != nil {
			return nil, err
		}
		if ok {
			return e.members(eligible)
		}
	}
	return nil, nil
}

// seated reports whether addr sits on any x/commons group. Seat holders are
// never reported: a demotion could cost a council its quorum mid-run.
func (e *simEnv) seated(addr string) bool {
	for _, g := range e.allGroups() {
		if e.sitsOn(g.Index, addr) {
			return true
		}
	}
	return false
}
