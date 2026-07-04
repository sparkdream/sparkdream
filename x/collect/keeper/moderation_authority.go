package keeper

import (
	errorsmod "cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"sparkdream/x/collect/types"
)

// resolveModerationAuthority decides whether MsgHideContent takes the
// council (gov) path or the sentinel path. Ported from x/forum's resolver
// (x/forum/keeper/moderation_authority.go) — same three-way semantics; see
// the authority-selection notes in docs/x-collect-spec.md (MsgHideContent).
//
//   - SENTINEL: force sentinel; surface sentinelErr if ineligible, never
//     fall back to council.
//   - COUNCIL: force council; error if not council-authorized. The
//     deliberate "act as committee" choice, allowed even for an eligible
//     sentinel.
//   - AUTO (default): prefer the accountable (bonded) sentinel path;
//     fall through to council only when the account is not an eligible
//     sentinel; if neither, surface the specific sentinel reason.
func resolveModerationAuthority(
	authority types.ModerationAuthority,
	sentinelEligible bool,
	sentinelErr error,
	isCouncil bool,
) (isGovAuthority bool, err error) {
	switch authority {
	case types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL:
		if !sentinelEligible {
			return false, sentinelErr
		}
		return false, nil
	case types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL:
		if !isCouncil {
			return true, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "not authorized as council")
		}
		return true, nil
	case types.ModerationAuthority_MODERATION_AUTHORITY_AUTO:
		switch {
		case sentinelEligible:
			return false, nil
		case isCouncil:
			return true, nil
		default:
			return false, sentinelErr
		}
	default:
		return false, errorsmod.Wrapf(types.ErrInvalidModerationAuthority, "unknown authority %d", authority)
	}
}
