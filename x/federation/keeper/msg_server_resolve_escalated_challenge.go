package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ResolveEscalatedChallenge applies a Phase 2 (human jury) verdict to
// an open EscalatedChallenge. The signer must be x/gov or the Commons
// Council / Operations Committee POLICY address — an executed proposal.
// A single committee member cannot sign it: the verdict slashes a
// verifier's bond or pays them, and decides who keeps the fees. The verdict (CHALLENGE_UPHELD / REJECTED /
// TIMEOUT) drives the verifier-side accounting, content status flip,
// challenge fee disposition, and escalation fee disposition (refund
// to the escalator if the jury overturned the auto-verdict; burned
// otherwise).
//
// Calling this on jury_deadline expiry is acceptable but unnecessary;
// the EndBlocker stamps TIMEOUT automatically. The handler is mostly
// for the case where Operations Committee reaches a verdict early.
//
// Any failed bank or x/rep side effect fails the tx and rolls everything
// back (the EndBlocker likewise commits a settlement only when it fully
// succeeds and retries it otherwise).
func (k msgServer) ResolveEscalatedChallenge(ctx context.Context, msg *types.MsgResolveEscalatedChallenge) (*types.MsgResolveEscalatedChallengeResponse, error) {
	if !k.isCouncilPolicy(ctx, msg.Authority) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be x/gov or the Operations Committee policy address (not authorized)")
	}
	switch msg.Verdict {
	case types.JuryVerdict_JURY_VERDICT_CHALLENGE_UPHELD,
		types.JuryVerdict_JURY_VERDICT_CHALLENGE_REJECTED,
		types.JuryVerdict_JURY_VERDICT_CHALLENGE_TIMEOUT:
	default:
		return nil, errorsmod.Wrapf(types.ErrInvalidJuryVerdict, "got %s", msg.Verdict)
	}

	esc, err := k.EscalatedChallenges.Get(ctx, msg.ContentId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrEscalatedChallengeNotFound,
			"content_id=%d", msg.ContentId)
	}
	// Settlement is single-shot: a dispute whose content already left
	// CHALLENGED / DISPUTED was settled, and a second verdict would pay,
	// slash and mint again out of other users' escrow.
	if content, cerr := k.Content.Get(ctx, msg.ContentId); cerr == nil && !isDisputeStatus(content.Status) {
		return nil, errorsmod.Wrapf(types.ErrArbitrationClosed,
			"content %d is %s; the dispute was already settled", msg.ContentId, content.Status)
	}
	// A party to the dispute never resolves it.
	if record, rerr := k.VerificationRecords.Get(ctx, msg.ContentId); rerr == nil {
		for _, party := range []string{record.Challenger, record.Verifier, esc.Escalator} {
			if k.sameMember(ctx, msg.Authority, party) {
				return nil, errorsmod.Wrap(types.ErrConflictOfInterest, "resolver is a party to the dispute")
			}
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// A failed side effect (bank send, x/rep slash / mint / release) fails
	// the tx, so nothing is half-applied and the verdict can be resubmitted;
	// the jury-deadline TIMEOUT still settles the dispute if it never is.
	if err := k.Keeper.applyJuryVerdict(ctx, msg.ContentId, msg.Verdict, sdkCtx.BlockTime().Unix(), params); err != nil {
		return nil, errorsmod.Wrap(types.ErrSettlementFailed, err.Error())
	}

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
