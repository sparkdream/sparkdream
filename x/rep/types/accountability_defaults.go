package types

import "cosmossdk.io/math"

// Defaults for accountability flows (member reports, warnings, gov action
// appeals).
var (
	// DefaultMinSentinelBond is the minimum DREAM required to file or co-sign
	// a member report.
	DefaultMinSentinelBond = math.NewInt(500)
)

const (
	// DefaultMinRepTierSentinel is the minimum reputation tier required to act
	// as a sentinel (report members, appeal actions).
	DefaultMinRepTierSentinel = uint64(3)

	// DefaultMemberReportCosignThreshold is the number of cosigners required
	// to escalate a member report.
	DefaultMemberReportCosignThreshold = uint64(3)

	// DefaultMaxMemberReporters caps the number of cosigners on a report.
	DefaultMaxMemberReporters = uint64(20)

	// DefaultMinDefenseWait is the minimum wait in seconds between defense
	// submission and report resolution.
	DefaultMinDefenseWait = int64(86400) // 24 hours

	// DefaultAppealDeadline is the appeal window in seconds.
	DefaultAppealDeadline = int64(1209600) // 14 days

	// DefaultAppealBondAmount is the uspark (SPARK) bond charged to the
	// appellant when filing MsgAppealGovAction. Refund/burn rules depend on
	// verdict (see MsgResolveGovActionAppeal handler).
	DefaultAppealBondAmount = int64(10_000_000) // 10 SPARK in uspark

	// DefaultSentinelOverturnSlash is the DREAM amount slashed from the
	// sentinel whose gov action was overturned on appeal.
	DefaultSentinelOverturnSlash = int64(100_000_000) // 100 DREAM in microDREAM

	// DefaultMaxConsecutiveOverturnsBeforeDemotion is the streak of consecutive
	// overturned sentinel actions that triggers automatic demotion.
	DefaultMaxConsecutiveOverturnsBeforeDemotion = uint64(3)

	// DefaultSentinelDemotionCooldown is the duration (seconds) a demoted
	// sentinel must wait before regaining sentinel privileges.
	DefaultSentinelDemotionCooldown = int64(604800) // 7 days
)
