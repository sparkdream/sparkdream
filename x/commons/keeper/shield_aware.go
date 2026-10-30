package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/commons/types"
)

// IsShieldCompatible implements the x/shield ShieldAware interface.
// Returns true if the message type is designed for anonymous execution
// through the shield module.
func (k Keeper) IsShieldCompatible(_ context.Context, msg sdk.Msg) bool {
	switch msg.(type) {
	case *types.MsgSubmitAnonymousProposal,
		*types.MsgAnonymousVoteProposal,
		*types.MsgAnonymousVotePoll:
		// The votes are proven against a secret ballot's voter snapshot
		// (ShieldMerkleRoot, secret_ballot.go), not the x/rep trust tree.
		return true
	default:
		return false
	}
}
