package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterInterfaces(registrar codectypes.InterfaceRegistry) {
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgVetoGroupProposals{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDeleteGroup{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgForceUpgrade{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateGroupConfig{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateGroupMembers{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRenewGroup{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterGroup{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreatePolicyPermissions{},
		&MsgUpdatePolicyPermissions{},
		&MsgDeletePolicyPermissions{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgEmergencyCancelGovProposal{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSpendFromCommons{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateParams{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSubmitAnonymousProposal{},
		&MsgAnonymousVoteProposal{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateCategory{},
	)

	msgservice.RegisterMsgServiceDesc(registrar, &_Msg_serviceDesc)
}
