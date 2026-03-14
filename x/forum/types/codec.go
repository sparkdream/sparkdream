package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterInterfaces(registrar codectypes.InterfaceRegistry) {
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAppealGovAction{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDefendMemberReport{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgResolveMemberReport{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCosignMemberReport{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgReportMember{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnbondSentinel{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgBondSentinel{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgResolveTagReport{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgReportTag{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetModerationPaused{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetForumPaused{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRejectProposedReply{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgConfirmProposedReply{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgMarkAcceptedReply{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDisputePin{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnpinReply{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgPinReply{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgWithdrawTagBudget{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgToggleTagBudget{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgTopUpTagBudget{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAwardFromTagBudget{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateTagBudget{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAssignBountyToReply{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCancelBounty{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgIncreaseBounty{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAwardBounty{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateBounty{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAppealThreadMove{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAppealThreadLock{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAppealPost{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgHidePost{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDismissFlags{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgFlagPost{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDownvotePost{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpvotePost{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnfollowThread{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgFollowThread{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgMoveThread{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnlockThread{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgLockThread{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnpinPost{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgPinPost{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnarchiveThread{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgFreezeThread{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDeletePost{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgEditPost{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreatePost{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateCategory{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateParams{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateOperationalParams{},
	)

	msgservice.RegisterMsgServiceDesc(registrar, &_Msg_serviceDesc)
}
