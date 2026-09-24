package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"
)

// releaseVerifierCommitment gives the verifier's CommittedAmount back to
// their available bond via x/rep ReleaseBond, exactly once per record.
//
// The CommitmentReleased flag — not the Outcome — is the authority on
// whether a release is owed, because the outcomes cannot distinguish the
// paths: an upheld challenge frees the commitment through SlashBond (which
// decrements TotalCommittedBond itself) and must never also release, while
// every other terminal path owes exactly one release. ReleaseBond is
// saturating, so a stray second release would not error; it would silently
// eat a different concurrent commitment of the same verifier.
//
// Failure semantics match the rest of the verdict pipeline: a ReleaseBond
// error is logged, the flag stays false, and the caller keeps making
// forward progress. Mutates the record through the pointer; the caller
// persists.
func (k Keeper) releaseVerifierCommitment(ctx context.Context, record *types.VerificationRecord) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if record.CommitmentReleased {
		return
	}
	if record.CommittedAmount.IsNil() || !record.CommittedAmount.IsPositive() {
		// Nothing was committed (zero slash amount params). Mark the
		// flag so the record still reads as settled.
		record.CommitmentReleased = true
		return
	}
	if k.late.repKeeper == nil {
		// Standalone-mode tests without rep wired. Consider it settled
		// so the state machine still advances.
		record.CommitmentReleased = true
		return
	}
	if err := k.late.repKeeper.ReleaseBond(ctx,
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER,
		record.Verifier, record.CommittedAmount); err != nil {
		sdkCtx.Logger().Warn("federation: release verifier commitment failed",
			"content_id", record.ContentId, "verifier", record.Verifier,
			"amount", record.CommittedAmount.String(), "error", err)
		return
	}
	record.CommitmentReleased = true
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeVerifierCommitmentReleased,
		sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", record.ContentId)),
		sdk.NewAttribute(types.AttributeKeyVerifier, record.Verifier),
		sdk.NewAttribute(types.AttributeKeyAmount, record.CommittedAmount.String()),
	))
}

// settleNoQuorumContent gives the content a terminal status when the
// arbiter resolution window closes without a quorum. It follows the
// house rule already set by applyJuryVerdictTimeout, which handles the
// same "no finding was reached" condition on the jury side:
//
//   - CHALLENGED reverts to VERIFIED. The content WAS independently
//     verified; a challenge that produced no quorum produced no finding,
//     so the verification stands. Sending it to UNRESOLVED instead would
//     let anyone permanently demote any verified content for the price of
//     one challenge fee, by challenging and then doing nothing.
//   - DISPUTED goes to UNRESOLVED. Here no verification was ever
//     established — the verifier's hash disagreed with the operator's and
//     nobody broke the tie — so there is no earlier state to revert to.
//     UNRESOLVED records exactly that, and is preferred over reverting to
//     PENDING_VERIFICATION because a revived record would have no
//     VerificationWindow queue entry (Phase 5 consumes it unconditionally)
//     and a verification deadline already in the past: unverifiable and
//     unreapable until content_ttl.
//
// Anything else is already terminal and left alone.
func (k Keeper) settleNoQuorumContent(ctx context.Context, sdkCtx sdk.Context, contentID uint64, verifier string) {
	content, err := k.Content.Get(ctx, contentID)
	if err != nil {
		return
	}
	switch content.Status {
	case types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED:
		content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED
	case types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_DISPUTED:
		content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_UNRESOLVED
	default:
		return
	}
	if err := k.Content.Set(ctx, contentID, content); err != nil {
		sdkCtx.Logger().Warn("expire arbiter resolutions: persist content failed",
			"content_id", contentID, "error", err)
		return
	}
	if content.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_UNRESOLVED {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeContentUnresolved,
			sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
			sdk.NewAttribute(types.AttributeKeyVerifier, verifier),
		))
		return
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeChallengeTimeout,
		sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
		sdk.NewAttribute(types.AttributeKeyVerifier, verifier),
	))
}
