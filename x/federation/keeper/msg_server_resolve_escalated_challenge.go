package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ResolveEscalatedChallenge applies a Phase 2 (human jury) verdict to
// an open EscalatedChallenge. Operations Committee only — the signer
// must be the council policy address, identical to the gate on
// MsgModerateContent. The verdict (CHALLENGE_UPHELD / REJECTED /
// TIMEOUT) drives the verifier-side accounting, content status flip,
// challenge fee disposition, and escalation fee disposition (refund
// to the escalator if the jury overturned the auto-verdict; burned
// otherwise).
//
// Calling this on jury_deadline expiry is acceptable but unnecessary;
// the EndBlocker stamps TIMEOUT automatically. The handler is mostly
// for the case where Operations Committee reaches a verdict early.
func (k msgServer) ResolveEscalatedChallenge(ctx context.Context, msg *types.MsgResolveEscalatedChallenge) (*types.MsgResolveEscalatedChallengeResponse, error) {
	if !k.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be Operations Committee")
	}
	switch msg.Verdict {
	case types.JuryVerdict_JURY_VERDICT_CHALLENGE_UPHELD,
		types.JuryVerdict_JURY_VERDICT_CHALLENGE_REJECTED,
		types.JuryVerdict_JURY_VERDICT_CHALLENGE_TIMEOUT:
	default:
		return nil, errorsmod.Wrapf(types.ErrInvalidJuryVerdict, "got %s", msg.Verdict)
	}

	if _, err := k.EscalatedChallenges.Get(ctx, msg.ContentId); err != nil {
		return nil, errorsmod.Wrapf(types.ErrEscalatedChallengeNotFound,
			"content_id=%d", msg.ContentId)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	k.Keeper.applyJuryVerdict(ctx, msg.ContentId, msg.Verdict, sdkCtx.BlockTime().Unix(), params)

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeJuryVerdictApplied,
			sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", msg.ContentId)),
			sdk.NewAttribute("verdict", msg.Verdict.String()),
			sdk.NewAttribute("authority", msg.Authority),
			sdk.NewAttribute("reasoning", msg.Reasoning),
		),
	)

	return &types.MsgResolveEscalatedChallengeResponse{}, nil
}
