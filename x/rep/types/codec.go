package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterInterfaces(registrar codectypes.InterfaceRegistry) {
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterZkPublicKey{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateInterim{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCompleteInterim{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAbandonInterim{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgApproveInterim{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSubmitInterimWork{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAssignInterim{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSubmitExpertTestimony{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSubmitJurorVote{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRespondToChallenge{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateChallenge{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnstake{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgStake{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCompleteInitiative{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAbandonInitiative{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgApproveInitiative{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSubmitInitiativeWork{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAssignInitiative{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateInitiative{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCancelProject{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgApproveProjectBudget{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgProposeProject{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgTransferDream{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAcceptInvitation{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgInviteMember{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateParams{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateOperationalParams{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgChallengeContent{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRespondToContentChallenge{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateTagBudget{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAwardFromTagBudget{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgTopUpTagBudget{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgToggleTagBudget{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgWithdrawTagBudget{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgBondSentinel{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnbondSentinel{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgReportMember{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCosignMemberReport{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgResolveMemberReport{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDefendMemberReport{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAppealGovAction{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgResolveGovActionAppeal{},
	)

	msgservice.RegisterMsgServiceDesc(registrar, &_Msg_serviceDesc)
}
