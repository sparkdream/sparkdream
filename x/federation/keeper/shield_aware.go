package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// IsShieldCompatible implements x/shield's ShieldAware interface.
// Returns true for MsgSubmitArbiterHash only when the inner message targets a
// real content record that is currently in CHALLENGED or DISPUTED state and
// whose arbitration round is still open (not escalated, no quorum yet,
// before arbiter_resolution_deadline). This gives FEDERATION-S2-5 a
// defensible second line of defense alongside shield's per-content
// nullifier scope: shield rejects useless submissions before they touch the
// federation handler at all.
//
// Content id 0 is a real record (ContentSeq starts at 0), so it is not
// special-cased: the record lookup decides.
func (k Keeper) IsShieldCompatible(ctx context.Context, msg sdk.Msg) bool {
	arbiter, ok := msg.(*types.MsgSubmitArbiterHash)
	if !ok {
		return false
	}
	content, err := k.Content.Get(ctx, arbiter.ContentId)
	if err != nil {
		return false
	}
	if !isDisputeStatus(content.Status) {
		return false
	}
	record, err := k.VerificationRecords.Get(ctx, arbiter.ContentId)
	if err != nil {
		return false
	}
	return k.arbitrationOpen(ctx, record, sdk.UnwrapSDKContext(ctx).BlockTime().Unix()) == nil
}
