package keeper

import (
	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
)

// resolveModerationAuthority decides whether a sentinel/council moderation
// action (hide, lock, move) runs as the council (gov) path. It centralizes the
// disambiguation shared by every moderation message so an account that is BOTH
// a bonded sentinel AND a committee member never silently upgrades to the
// cheaper, less-accountable council path. See
// docs/x-forum-spec.md (Shared ModerationAuthority).
//
//   - sentinelEligible reports whether the caller satisfies the action's own
//     sentinel requirements (bonded NORMAL/RECOVERY plus, e.g., the lock
//     bond/tier or the move reserved-tag check).
//   - sentinelErr is the specific reason the sentinel path is unavailable
//     (ErrNotSentinel / ErrSentinelDemoted / ErrInsufficientLockBond / ...).
//     It is surfaced for an explicit SENTINEL request and for the AUTO case
//     where no path is available — never a generic "unauthorized".
//   - isCouncil reports Commons Operations Committee / governance authorization.
//
// On error the bool return is meaningless; callers must check err first.
func resolveModerationAuthority(
	authority types.ModerationAuthority,
	sentinelEligible bool,
	sentinelErr error,
	isCouncil bool,
) (isGovAuthority bool, err error) {
	switch authority {
	case types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL:
		// Force sentinel; no silent fallback to council.
		if !sentinelEligible {
			return false, sentinelErr
		}
		return false, nil
	case types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL:
		// Force council; the deliberate "act as committee" choice, allowed even
		// for an eligible sentinel.
		if !isCouncil {
			return true, errorsmod.Wrap(types.ErrNotAuthorized, "not authorized as council")
		}
		return true, nil
	case types.ModerationAuthority_MODERATION_AUTHORITY_AUTO:
		// Prefer the accountable sentinel path; fall through to council only
		// when the account is not eligible for the sentinel path.
		switch {
		case sentinelEligible:
			return false, nil
		case isCouncil:
			return true, nil
		default:
			// Neither path available: surface the specific sentinel reason.
			return false, sentinelErr
		}
	default:
		return false, errorsmod.Wrapf(types.ErrInvalidModerationAuthority, "unknown authority %d", authority)
	}
}
