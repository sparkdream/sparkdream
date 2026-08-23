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

	// DefaultRoleOverturnCooldown is the duration (seconds) a role holder is
	// locked out of new moderation actions (on every surface) after a lost
	// appeal. Moved here from forum's DefaultSentinelOverturnCooldown when
	// the shared accountability record moved to x/rep.
	DefaultRoleOverturnCooldown = int64(86400) // 24 hours

	// MaxAcceptanceCriteria bounds the definition-of-done an initiative may
	// declare. Criteria are stored on the initiative and read on every
	// challenge citation and juror vote, so the list is state a creator
	// controls and has to stay small.
	MaxAcceptanceCriteria = 20

	// MaxCriteriaIDLength and MaxCriteriaQuestionLength bound the strings
	// inside each criterion for the same reason.
	MaxCriteriaIDLength       = 64
	MaxCriteriaQuestionLength = 512

	// MaxVerifierCount bounds how many reviewer approvals a project may demand
	// before an initiative can complete. A ceiling matters because the reviewer
	// gate is a liveness risk as much as a quality one: a project asking for
	// more approvals than the roster can supply stalls every initiative under it
	// until the committee escalation path fires.
	MaxVerifierCount = uint32(10)

	// MaxDeliverableURILength bounds the pointer to the submitted work. Like
	// the criteria strings above, this is state the submitter controls and
	// every reviewer reads, so it needs a ceiling.
	MaxDeliverableURILength = 512

	// MaxJuryRedraws bounds replacement rounds per review. A pool that will not
	// answer after this many attempts is a quorum problem no amount of
	// redrawing fixes, and the review falls through to its deadline tally.
	//
	// One round, not two: the acceptance window is now a quarter of the review
	// period rather than a fixed ~2 hours, so a second round would consume half
	// the time the replacement jurors need to read the work. Declines also
	// refill immediately now, so the sweep is the fallback for silence rather
	// than the only way a seat ever moves.
	DefaultMaxJuryRedraws = uint32(1)

	// DefaultMinJurorReward floors the per-juror payment so that a jury on a
	// small dispute is still paid something for reading the work, and so content
	// challenges and moderation appeals — which have no initiative budget to
	// scale against — have a defined rate. Seeds the min_juror_reward param;
	// also the fallback when a chain's stored params predate that field.
	DefaultMinJurorReward = int64(5_000_000) // 5 DREAM

	// MinSeatedJurors is the floor below which the redraw sweep will not shrink
	// a jury.
	//
	// Quorum is len(jurors)/2+1, so an unguarded jury that vacated down to one
	// seat would give that juror a quorum of one — enough to uphold a challenge
	// and burn the assignee's bond alone. Seats stop being vacated at this
	// floor; the remaining silent jurors simply hold quorum where it is, and the
	// review falls through to its deadline tally (and from there to the
	// adjudication terminal path) rather than being decided by a rump.
	MinSeatedJurors = 3

	// MinJurorSelectionWeight is the floor on the responsiveness multiplier
	// applied to a juror's selection weight.
	//
	// A juror who never answers is drawn less often, never *not at all*: broad
	// sortition is what makes juries hard to capture, and an address that drops
	// to zero weight is excluded in all but name — with no way to earn its way
	// back, since it would never be drawn again to demonstrate otherwise.
	DefaultMinJurorSelectionWeight = 0.1

	// MinJurySeatingsForWeighting is the number of seatings a juror must have
	// on record before responsiveness affects their selection weight. Below it
	// they are drawn at full weight: one missed summons is not evidence of
	// anything, and a new member has no record at all.
	DefaultMinJurySeatingsForWeighting = uint64(3)

	// DefaultSentinelAccuracyWindowEpochs is the rolling window (in reward
	// epochs) over which sentinel reward accuracy is measured.
	DefaultSentinelAccuracyWindowEpochs = uint64(6)

	// MaxSentinelAccuracyWindowEpochs caps SentinelAccuracyWindowEpochs. It MUST
	// equal the forum SentinelAccuracyRingSize (the ring cannot resolve a window
	// longer than it has slots). Kept here (not imported from x/forum) to avoid
	// a rep -> forum import; the two are asserted equal by a forum-side test.
	MaxSentinelAccuracyWindowEpochs = uint64(24)
)
