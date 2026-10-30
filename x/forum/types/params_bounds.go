package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// Compiled hard bounds on the operational params the Commons Operations
// Committee may set through MsgUpdateOperationalParams. They keep a committee
// decision from pricing the forum out of reach, disabling appeals (a cooldown
// longer than the appeal window), turning anti-spam windows off or minting
// outsized rewards. Governance (MsgUpdateParams) and genesis are not bound by
// them. The lower clock bounds apply on network builds only
// (opsClockFloorsEnabled, build-tagged), because e2e genesis pins second-long
// windows. Changing a bound needs a chain upgrade.
const (
	opsMaxFeeAmount          = int64(100_000_000) // 100 SPARK, any single fee / tax / deposit
	opsMaxCostPerByte        = int64(10_000)      // per byte, bond-denom base units
	opsMinEphemeralTTL       = int64(3600)        // 1 hour
	opsMaxEphemeralTTL       = int64(30 * 86400)  // 30 days
	opsMinArchiveThreshold   = int64(7 * 86400)   // 7 days
	opsMaxUnarchiveCooldown  = int64(30 * 86400)  // 30 days
	opsMaxArchiveCooldown    = int64(180 * 86400) // 180 days
	opsMaxAppealCooldown     = int64(7 * 86400)   // 7 days (appeal windows are 14 days)
	opsMaxEditWindow         = int64(30 * 86400)  // 30 days
	opsMaxRenewalPeriod      = int64(90 * 86400)  // 90 days
	opsMaxUnhideWindow       = int64(7 * 86400)   // 7 days
	opsMinRoleCooldown       = int64(86400)       // 1 day (unbond / demotion)
	opsMinConvictionLock     = int64(86400)       // 1 day
	opsMaxConvictionLock     = int64(90 * 86400)  // 90 days
	opsMinProposalTimeout    = int64(3600)        // 1 hour
	opsMaxProposalTimeout    = int64(30 * 86400)  // 30 days
	opsMaxCancellationFeePct = uint64(50)
	opsMaxPromotionsPerBlock = uint32(500)
)

var (
	opsMinSentinelBond      = math.NewInt(100_000_000)     // 100 DREAM
	opsMaxSentinelBond      = math.NewInt(100_000_000_000) // 100,000 DREAM
	opsMinSentinelSlash     = math.NewInt(1_000_000)       // 1 DREAM
	opsMaxCurationReward    = math.NewInt(50_000_000)      // 50 DREAM
	opsMaxStreamRate        = math.LegacyOneDec()          // rep per DREAM per day
	opsMaxForumRepPerEpoch  = math.LegacyNewDec(100)
	opsMaxAuthorRepSlash    = math.LegacyNewDec(100)
	opsMaxConvictionMinimum = math.NewInt(10_000_000_000) // 10,000 DREAM
)

// ValidateCommitteeBounds checks the operational params against the compiled
// committee bounds. Called only on the MsgUpdateOperationalParams path.
func (p ForumOperationalParams) ValidateCommitteeBounds() error {
	for name, v := range map[string]math.Int{
		"spam_tax_amount":          p.SpamTaxAmount,
		"reaction_spam_tax_amount": p.ReactionSpamTaxAmount,
		"flag_spam_tax_amount":     p.FlagSpamTaxAmount,
		"downvote_deposit_amount":  p.DownvoteDepositAmount,
		"edit_fee_amount":          p.EditFeeAmount,
	} {
		if !v.IsNil() && v.GT(math.NewInt(opsMaxFeeAmount)) {
			return fmt.Errorf("%s must be <= %d: %s", name, opsMaxFeeAmount, v)
		}
	}
	if !p.CostPerByteAmount.IsNil() && p.CostPerByteAmount.GT(math.NewInt(opsMaxCostPerByte)) {
		return fmt.Errorf("cost_per_byte_amount must be <= %d: %s", opsMaxCostPerByte, p.CostPerByteAmount)
	}
	if err := boundInt64("ephemeral_ttl", p.EphemeralTtl, clockFloor(opsMinEphemeralTTL), opsMaxEphemeralTTL); err != nil {
		return err
	}
	if p.ArchiveThreshold < clockFloor(opsMinArchiveThreshold) {
		return fmt.Errorf("archive_threshold must be >= %d: %d", opsMinArchiveThreshold, p.ArchiveThreshold)
	}
	if err := boundInt64("unarchive_cooldown", p.UnarchiveCooldown, 0, opsMaxUnarchiveCooldown); err != nil {
		return err
	}
	if err := boundInt64("archive_cooldown", p.ArchiveCooldown, 0, opsMaxArchiveCooldown); err != nil {
		return err
	}
	for name, v := range map[string]int64{
		"hide_appeal_cooldown": p.HideAppealCooldown,
		"lock_appeal_cooldown": p.LockAppealCooldown,
		"move_appeal_cooldown": p.MoveAppealCooldown,
	} {
		if err := boundInt64(name, v, 0, opsMaxAppealCooldown); err != nil {
			return err
		}
	}
	if err := boundInt64("edit_max_window", p.EditMaxWindow, 0, opsMaxEditWindow); err != nil {
		return err
	}
	if err := boundInt64("edit_grace_period", p.EditGracePeriod, 0, p.EditMaxWindow); err != nil {
		return err
	}
	if err := boundInt64("conviction_renewal_period", p.ConvictionRenewalPeriod, 0, opsMaxRenewalPeriod); err != nil {
		return err
	}
	if err := boundInt64("sentinel_unhide_window", p.SentinelUnhideWindow, 0, opsMaxUnhideWindow); err != nil {
		return err
	}
	if p.SentinelUnbondCooldown < clockFloor(opsMinRoleCooldown) {
		return fmt.Errorf("sentinel_unbond_cooldown must be >= %d: %d", opsMinRoleCooldown, p.SentinelUnbondCooldown)
	}
	if p.SentinelDemotionCooldown < clockFloor(opsMinRoleCooldown) {
		return fmt.Errorf("sentinel_demotion_cooldown must be >= %d: %d", opsMinRoleCooldown, p.SentinelDemotionCooldown)
	}
	if err := boundInt64("post_conviction_lock_seconds", p.PostConvictionLockSeconds, clockFloor(opsMinConvictionLock), opsMaxConvictionLock); err != nil {
		return err
	}
	if err := boundInt64("accept_proposal_timeout", p.AcceptProposalTimeout, clockFloor(opsMinProposalTimeout), opsMaxProposalTimeout); err != nil {
		return err
	}
	if p.BountyCancellationFeePercent > opsMaxCancellationFeePct {
		return fmt.Errorf("bounty_cancellation_fee_percent must be <= %d: %d", opsMaxCancellationFeePct, p.BountyCancellationFeePercent)
	}
	if p.MaxPromotionsPerBlock > opsMaxPromotionsPerBlock {
		return fmt.Errorf("max_promotions_per_block must be <= %d: %d", opsMaxPromotionsPerBlock, p.MaxPromotionsPerBlock)
	}
	if bond, ok := math.NewIntFromString(p.MinSentinelBond); ok && (bond.LT(opsMinSentinelBond) || bond.GT(opsMaxSentinelBond)) {
		return fmt.Errorf("min_sentinel_bond must be within [%s, %s]: %s", opsMinSentinelBond, opsMaxSentinelBond, bond)
	}
	if slash, ok := math.NewIntFromString(p.SentinelSlashAmount); ok && slash.LT(opsMinSentinelSlash) {
		return fmt.Errorf("sentinel_slash_amount must be >= %s: %s", opsMinSentinelSlash, slash)
	}
	if !p.CurationDreamReward.IsNil() && p.CurationDreamReward.GT(opsMaxCurationReward) {
		return fmt.Errorf("curation_dream_reward must be <= %s: %s", opsMaxCurationReward, p.CurationDreamReward)
	}
	if !p.MinPostConvictionStake.IsNil() && p.MinPostConvictionStake.GT(opsMaxConvictionMinimum) {
		return fmt.Errorf("min_post_conviction_stake must be <= %s: %s", opsMaxConvictionMinimum, p.MinPostConvictionStake)
	}
	if !p.PostConvictionStreamRatePerBlock.IsNil() && p.PostConvictionStreamRatePerBlock.GT(opsMaxStreamRate) {
		return fmt.Errorf("post_conviction_stream_rate_per_block must be <= %s: %s", opsMaxStreamRate, p.PostConvictionStreamRatePerBlock)
	}
	if !p.MaxForumRepPerTagPerEpoch.IsNil() && p.MaxForumRepPerTagPerEpoch.GT(opsMaxForumRepPerEpoch) {
		return fmt.Errorf("max_forum_rep_per_tag_per_epoch must be <= %s: %s", opsMaxForumRepPerEpoch, p.MaxForumRepPerTagPerEpoch)
	}
	if !p.AuthorRepSlash.IsNil() && p.AuthorRepSlash.GT(opsMaxAuthorRepSlash) {
		return fmt.Errorf("author_rep_slash must be <= %s: %s", opsMaxAuthorRepSlash, p.AuthorRepSlash)
	}
	return nil
}

func boundInt64(name string, v, lo, hi int64) error {
	if v < lo || v > hi {
		return fmt.Errorf("%s must be within [%d, %d]: %d", name, lo, hi, v)
	}
	return nil
}

// clockFloor returns a lower clock bound, or 0 when floors are disabled.
func clockFloor(v int64) int64 {
	if !opsClockFloorsEnabled {
		return 0
	}
	return v
}
