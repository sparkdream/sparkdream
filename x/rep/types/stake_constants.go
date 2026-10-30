package types

// Staking constants that shape reward accounting and bound per-block work.
// These are deliberately compile-time constants rather than governance params:
// they are structural limits whose safe range depends on keeper implementation
// details (per-block iteration cost, MasterChef debt semantics), so changing
// them belongs to a chain upgrade alongside the code that reads them.
const (
	// MaxStakeTranchesPerTarget caps how many separate stake records one member
	// may hold on a single target.
	//
	// Stakes are deliberately never merged: each carries its own created_at
	// maturity clock and its own reward_debt baseline, and averaging two joins
	// made at different times or at different accumulator values would break
	// both. But nothing about that design needs an *unbounded* record count,
	// and two costs scale with it — the EndBlocker conviction sweep touches
	// every stake of every active initiative on every block, and CreateStake's
	// per-member cap check is O(existing stakes on the target), making n
	// tranches cost O(n^2) to accumulate. Since the staked DREAM is fully
	// refundable via Unstake, an uncapped count lets a member impose permanent
	// per-block work on every validator for close to free.
	MaxStakeTranchesPerTarget = 10
)

// Per-member structural bounds (REP-E-10, REP-E-16). Compile-time for the same
// reason as MaxStakeTranchesPerTarget: they bound per-handler and per-epoch
// work, so changing them belongs to a chain upgrade.
const (
	// MaxStakeRecordsPerMember caps how many stake records one member holds
	// across every target. Every reward-bearing record is decayed and
	// rewritten each epoch, so at the min_stake_amount floor an uncapped
	// count let one member buy tens of thousands of per-epoch writes.
	MaxStakeRecordsPerMember = 500

	// MaxVouchedTags caps the tags an invitation vouches for; each becomes a
	// reputation key on the new member.
	MaxVouchedTags = 10

	// MaxChallengeEvidence and MaxChallengeEvidenceLength bound a challenge's
	// evidence list, which is stored on the record for good.
	MaxChallengeEvidence       = 20
	MaxChallengeEvidenceLength = 1024

	// MaxTagCreationsPerEpoch caps the tags one member creates per epoch, so
	// the registry cap cannot be filled by one account.
	MaxTagCreationsPerEpoch = 5

	// CouncilTagHeadroomBps is the share of DefaultMaxTotalTags (basis
	// points) only council or committee policy addresses may fill: members
	// stop at the rest, so a full registry still leaves room for governance
	// to add a tag.
	CouncilTagHeadroomBps = 1000
)

// NominalBlockSeconds converts epochs to an approximate duration for display
// fields only (Invitation.accountability_end / referral_end). No consensus
// check depends on it: windows are counted in epochs (block height).
const NominalBlockSeconds = int64(6)
