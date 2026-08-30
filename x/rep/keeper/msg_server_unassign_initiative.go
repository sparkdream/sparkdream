package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UnassignInitiative releases an assignment and returns the initiative to OPEN.
//
// Authorization is deliberately narrower than AssignInitiative, which also
// accepts the parent project's creator. The creator benefits from taking work
// back or moving it to someone else, so letting them unassign would be a
// rug-pull on work already in flight; their lever is CloseInitiative, which
// retires the item outright rather than quietly changing hands. That leaves the
// assignee stepping down voluntarily, and the Operations Committee as the
// neutral body able to free work that has stalled.
func (k msgServer) UnassignInitiative(ctx context.Context, msg *types.MsgUnassignInitiative) (*types.MsgUnassignInitiativeResponse, error) {
	creatorBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	initiative, err := k.Keeper.GetInitiative(ctx, msg.InitiativeId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get initiative")
	}

	// The signer is the party being unassigned. Only the assignee holds an
	// assignment today — Initiative.Apprentice is declared but no message ever
	// populates it, so there is no second holder to disambiguate between. If
	// apprentice pairing is wired up later, this is where the signer would
	// select which of the two assignments is being released, and clearing an
	// apprentice would leave the status at ASSIGNED rather than reopening.
	forced := msg.Creator != initiative.Assignee
	if forced {
		isOps, opsErr := k.Keeper.commonsKeeper.IsCommitteeMember(ctx, sdk.AccAddress(creatorBytes), "commons", "operations")
		if opsErr != nil {
			return nil, errorsmod.Wrap(opsErr, "failed to check operations committee membership")
		}
		if !isOps {
			return nil, errorsmod.Wrap(types.ErrUnauthorized, "only the assignee or the operations committee may unassign an initiative")
		}
	}

	if err := k.Keeper.UnassignInitiative(ctx, msg.InitiativeId, msg.Reason, forced); err != nil {
		return nil, errorsmod.Wrap(err, "failed to unassign initiative")
	}

	return &types.MsgUnassignInitiativeResponse{}, nil
}
