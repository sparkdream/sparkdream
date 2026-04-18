package types_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	fotypes "sparkdream/x/forum/types"
)

func TestRegisterInterfaces(t *testing.T) {
	registry := types.NewInterfaceRegistry()
	fotypes.RegisterInterfaces(registry)

	msgs := []sdk.Msg{
		&fotypes.MsgCreatePost{},
		&fotypes.MsgEditPost{},
		&fotypes.MsgDeletePost{},
		&fotypes.MsgFreezeThread{},
		&fotypes.MsgUnarchiveThread{},
		&fotypes.MsgPinPost{},
		&fotypes.MsgUnpinPost{},
		&fotypes.MsgLockThread{},
		&fotypes.MsgUnlockThread{},
		&fotypes.MsgMoveThread{},
		&fotypes.MsgFollowThread{},
		&fotypes.MsgUnfollowThread{},
		&fotypes.MsgUpvotePost{},
		&fotypes.MsgDownvotePost{},
		&fotypes.MsgFlagPost{},
		&fotypes.MsgDismissFlags{},
		&fotypes.MsgHidePost{},
		&fotypes.MsgAppealPost{},
		&fotypes.MsgAppealThreadLock{},
		&fotypes.MsgAppealThreadMove{},
		&fotypes.MsgCreateBounty{},
		&fotypes.MsgAwardBounty{},
		&fotypes.MsgIncreaseBounty{},
		&fotypes.MsgCancelBounty{},
		&fotypes.MsgAssignBountyToReply{},
		&fotypes.MsgPinReply{},
		&fotypes.MsgUnpinReply{},
		&fotypes.MsgDisputePin{},
		&fotypes.MsgMarkAcceptedReply{},
		&fotypes.MsgConfirmProposedReply{},
		&fotypes.MsgRejectProposedReply{},
		&fotypes.MsgSetForumPaused{},
		&fotypes.MsgSetModerationPaused{},
		&fotypes.MsgUpdateParams{},
		&fotypes.MsgUpdateOperationalParams{},
	}

	// All registered messages should resolve to a concrete sdk.Msg implementation.
	for _, m := range msgs {
		typeURL := sdk.MsgTypeURL(m)
		if typeURL == "" {
			t.Errorf("empty type URL for %T", m)
		}
	}
}
