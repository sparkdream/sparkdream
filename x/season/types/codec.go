package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterInterfaces(registrar codectypes.InterfaceRegistry) {
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgResolveUnappealedModeration{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgResolveDisplayNameAppeal{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAppealDisplayNameModeration{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgReportDisplayName{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSkipTransitionPhase{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRetrySeasonTransition{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAbortSeasonTransition{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetNextSeasonInfo{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgExtendSeason{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDeactivateQuest{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateQuest{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAbandonQuest{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgClaimQuestReward{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgStartQuest{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgClaimGuildFounder{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgKickFromGuild{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateGuildDescription{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetGuildInviteOnly{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRevokeGuildInvite{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAcceptGuildInvite{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgInviteToGuild{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDemoteOfficer{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgPromoteToOfficer{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDissolveGuild{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgTransferGuildFounder{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgLeaveGuild{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgJoinGuild{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateGuild{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetDisplayTitle{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetUsername{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetDisplayName{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateParams{},
	)
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateOperationalParams{},
	)
	msgservice.RegisterMsgServiceDesc(registrar, &_Msg_serviceDesc)
}
