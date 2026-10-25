package keeper

import (
	"context"
	"fmt"
	"math"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/season/types"
)

// ---------------------------------------------------------------------------
// XP engine (x-season-spec "XP Sources" / "GrantXP")
//
// Every XP source goes through GrantXP, which enforces the global per-epoch
// cap and keeps the per-source EpochXpTracker buckets. Profiles are created
// on first touch so a member admitted before the hook existed, or seeded by
// genesis without a profile, still earns XP.
//
// Gas: these paths run inside other modules' transactions (a council vote,
// a forum reply), where the user pays for every store read. The season
// Params record is a few KB, so it is loaded once per hook into an xpEnv
// and passed down; nothing below re-reads it or recomputes the epoch.
// ---------------------------------------------------------------------------

// xpEnv is the per-hook snapshot of params and epoch every XP helper reads.
type xpEnv struct {
	params types.Params
	epoch  int64
}

// loadXPEnv reads params once and derives the current epoch from them.
func (k Keeper) loadXPEnv(ctx context.Context) (xpEnv, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return xpEnv{}, err
	}
	return xpEnv{params: params, epoch: epochAt(ctx, params)}, nil
}

// epochAt is GetCurrentEpoch without the params read.
func epochAt(ctx context.Context, params types.Params) int64 {
	if params.EpochBlocks <= 0 {
		return 0
	}
	return sdk.UnwrapSDKContext(ctx).BlockHeight() / params.EpochBlocks
}

// levelForXP is CalculateLevel without the params read.
func levelForXP(params types.Params, xp uint64) uint64 {
	level := uint64(1)
	for i, threshold := range params.LevelThresholds {
		if xp >= threshold {
			level = uint64(i + 1)
		} else {
			break
		}
	}
	return level
}

// GrantXP grants `amount` XP to `member` from `source`, capped at what remains
// of the member's per-epoch allowance. Returns ErrXPCapReached when the
// allowance is already spent; callers that treat XP as best-effort should
// log and continue.
func (k Keeper) GrantXP(ctx context.Context, member sdk.AccAddress, amount uint64, source types.XPSource, reference string) error {
	env, err := k.loadXPEnv(ctx)
	if err != nil {
		return err
	}
	return k.grantXP(ctx, env, member, amount, source, reference)
}

// grantXP is GrantXP with the params/epoch already loaded: it loads the
// profile, applies the grant and writes the profile once.
func (k Keeper) grantXP(ctx context.Context, env xpEnv, member sdk.AccAddress, amount uint64, source types.XPSource, reference string) error {
	if amount == 0 {
		return nil
	}
	memberStr, err := k.addressCodec.BytesToString(member)
	if err != nil {
		return err
	}
	profile := k.getOrCreateProfile(ctx, memberStr, env.epoch)
	if err := k.grantXPToProfile(ctx, env, &profile, amount, source, reference); err != nil {
		return err
	}
	return k.MemberProfile.Set(ctx, memberStr, profile)
}

// grantXPToProfile applies a grant to an in-memory profile: it enforces the
// epoch cap, updates the EpochXpTracker (small record), bumps XP and level
// on `profile` and emits the events. The caller persists the profile, so a
// hook that also bumps a counter pays for one profile write, not three
// (a MemberProfile with titles and achievements is several hundred bytes
// and store writes are the dominant gas in these hooks).
func (k Keeper) grantXPToProfile(ctx context.Context, env xpEnv, profile *types.MemberProfile, amount uint64, source types.XPSource, reference string) error {
	if amount == 0 {
		return nil
	}
	memberStr := profile.Address
	tracker := k.getEpochXPTracker(ctx, memberStr, env.epoch)
	total := tracker.VoteXpEarned + tracker.ForumXpEarned + tracker.QuestXpEarned + tracker.OtherXpEarned
	if env.params.MaxXpPerEpoch > 0 {
		if total >= env.params.MaxXpPerEpoch {
			return types.ErrXPCapReached
		}
		if remaining := env.params.MaxXpPerEpoch - total; amount > remaining {
			amount = remaining
		}
	}

	switch source {
	case types.XPSourceVoteCast, types.XPSourceProposalCreated:
		tracker.VoteXpEarned += amount
	case types.XPSourceForumReply, types.XPSourceForumHelpful:
		tracker.ForumXpEarned += amount
	case types.XPSourceQuest:
		tracker.QuestXpEarned += amount
	default:
		tracker.OtherXpEarned += amount
	}
	if err := k.EpochXpTracker.Set(ctx, tracker.MemberEpoch, tracker); err != nil {
		return err
	}
	if profile.SeasonXp > math.MaxUint64-amount || profile.LifetimeXp > math.MaxUint64-amount {
		return errorsmod.Wrap(types.ErrInvalidXPSource, "xp overflow")
	}
	profile.SeasonXp += amount
	profile.LifetimeXp += amount
	profile.LastActiveEpoch = env.epoch

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	oldLevel := profile.SeasonLevel
	if newLevel := levelForXP(env.params, profile.SeasonXp); newLevel > oldLevel {
		profile.SeasonLevel = newLevel
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("level_up",
			sdk.NewAttribute("member", memberStr),
			sdk.NewAttribute("old_level", fmt.Sprintf("%d", oldLevel)),
			sdk.NewAttribute("new_level", fmt.Sprintf("%d", newLevel)),
		))
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("xp_granted",
		sdk.NewAttribute("member", memberStr),
		sdk.NewAttribute("amount", fmt.Sprintf("%d", amount)),
		sdk.NewAttribute("source", source.String()),
		sdk.NewAttribute("reference", reference),
		sdk.NewAttribute("guild_id", fmt.Sprintf("%d", profile.GuildId)),
	))
	return nil
}

// getOrCreateProfile returns the member's profile, creating a level-1 one
// when none exists yet.
func (k Keeper) getOrCreateProfile(ctx context.Context, memberStr string, epoch int64) types.MemberProfile {
	profile, err := k.MemberProfile.Get(ctx, memberStr)
	if err != nil {
		profile = types.MemberProfile{Address: memberStr, SeasonLevel: 1, LastActiveEpoch: epoch}
	}
	if profile.SeasonLevel == 0 {
		profile.SeasonLevel = 1
	}
	return profile
}

// --- EpochXpTracker --------------------------------------------------------

func epochXPTrackerKey(memberStr string, epoch int64) string {
	return fmt.Sprintf("%s:%d", memberStr, epoch)
}

func (k Keeper) getEpochXPTracker(ctx context.Context, memberStr string, epoch int64) types.EpochXpTracker {
	key := epochXPTrackerKey(memberStr, epoch)
	tracker, err := k.EpochXpTracker.Get(ctx, key)
	if err != nil {
		return types.EpochXpTracker{MemberEpoch: key}
	}
	return tracker
}

// --- VoteXpRecord ----------------------------------------------------------

func voteXPRecordKey(season uint64, memberStr string, proposalID uint64) string {
	return fmt.Sprintf("%d:%s:%d", season, memberStr, proposalID)
}

func (k Keeper) hasVoteXPRecord(ctx context.Context, season uint64, memberStr string, proposalID uint64) bool {
	has, err := k.VoteXpRecord.Has(ctx, voteXPRecordKey(season, memberStr, proposalID))
	return err == nil && has
}

// voteXPCountThisEpoch counts proposals the member already earned vote XP
// for in `epoch`, derived from the tracker bucket so no extra state is kept.
func (k Keeper) voteXPCountThisEpoch(ctx context.Context, memberStr string, epoch int64, xpPerVote uint64) uint64 {
	if xpPerVote == 0 {
		return 0
	}
	return k.getEpochXPTracker(ctx, memberStr, epoch).VoteXpEarned / xpPerVote
}

// --- MemberRegistration / forum cooldowns ------------------------------------

func (k Keeper) isAccountOldEnough(ctx context.Context, memberStr string, epoch int64, minEpochs uint64) bool {
	reg, err := k.MemberRegistration.Get(ctx, memberStr)
	if err != nil {
		return false
	}
	age := epoch - reg.RegisteredEpoch
	return age >= 0 && uint64(age) >= minEpochs
}

func forumXPCooldownKey(beneficiary, actor string) string {
	return beneficiary + ":" + actor
}

// forumXPCooldownPassed reports whether `actor` may grant forum XP to
// `beneficiary` again (the last grant is older than the cooldown).
func (k Keeper) forumXPCooldownPassed(ctx context.Context, beneficiary, actor string, epoch int64, cooldownEpochs uint64) bool {
	// Forum hooks run inside forum txs, which have gas slack: check every
	// (beneficiary alias, actor alias) pair inside the dedup horizon.
	for _, b := range k.recentAddressesOf(ctx, beneficiary) {
		for _, a := range k.recentAddressesOf(ctx, actor) {
			cd, err := k.ForumXpCooldown.Get(ctx, forumXPCooldownKey(b, a))
			if err != nil {
				continue
			}
			if epoch-cd.LastGrantedEpoch < int64(cooldownEpochs) {
				return false
			}
		}
	}
	return true
}

func (k Keeper) setForumXPCooldown(ctx context.Context, beneficiary, actor string, epoch int64) error {
	key := forumXPCooldownKey(beneficiary, actor)
	return k.ForumXpCooldown.Set(ctx, key, types.ForumXpCooldown{
		BeneficiaryActor: key,
		LastGrantedEpoch: epoch,
	})
}

// grantForumXP applies the shared forum anti-gaming rules (self-grant,
// actor account age, reciprocal cooldown, per-epoch forum cap) and grants
// `amount` to the author. Returns false when nothing was granted.
func (k Keeper) grantForumXP(ctx context.Context, env xpEnv, author, actor string, amount uint64, source types.XPSource, reference string) (bool, error) {
	if amount == 0 || k.sameMember(ctx, author, actor) {
		return false, nil
	}
	if !k.isAccountOldEnough(ctx, actor, env.epoch, env.params.ForumXpMinAccountAgeEpochs) {
		return false, nil
	}
	if !k.forumXPCooldownPassed(ctx, author, actor, env.epoch, env.params.ForumXpReciprocalCooldownEpochs) {
		return false, nil
	}
	tracker := k.getEpochXPTracker(ctx, author, env.epoch)
	if env.params.MaxForumXpPerEpoch > 0 {
		if tracker.ForumXpEarned >= env.params.MaxForumXpPerEpoch {
			return false, nil
		}
		if remaining := env.params.MaxForumXpPerEpoch - tracker.ForumXpEarned; amount > remaining {
			amount = remaining
		}
	}
	authorAddr, err := k.addressCodec.StringToBytes(author)
	if err != nil {
		return false, nil
	}
	if err := k.grantXP(ctx, env, authorAddr, amount, source, reference); err != nil {
		if errorsmod.IsOf(err, types.ErrXPCapReached) {
			return false, nil
		}
		return false, err
	}
	return true, k.setForumXPCooldown(ctx, author, actor, env.epoch)
}
