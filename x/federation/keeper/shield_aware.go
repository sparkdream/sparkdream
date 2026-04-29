package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// IsShieldCompatible implements x/shield's ShieldAware interface.
// Returns true for MsgSubmitArbiterHash only when the inner message targets a
// real content record that is currently in CHALLENGED or DISPUTED state. This
// gives FEDERATION-S2-5 a defensible second line of defense alongside shield's
// per-content nullifier scope: shield rejects useless submissions before they
// touch the federation handler at all.
func (k Keeper) IsShieldCompatible(ctx context.Context, msg sdk.Msg) bool {
	arbiter, ok := msg.(*types.MsgSubmitArbiterHash)
	if !ok {
		return false
	}
	if arbiter.ContentId == 0 {
		return false
	}
	content, err := k.Content.Get(ctx, arbiter.ContentId)
	if err != nil {
		return false
	}
	return content.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED ||
		content.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_DISPUTED
}
