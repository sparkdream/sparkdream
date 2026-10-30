package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterInterfaces(registrar codectypes.InterfaceRegistry) {
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgLeaveVault{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAcceptVaultSigner{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateVault{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCancelProposal{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgExecute{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRevokeApproval{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgApprove{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgPropose{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateVault{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateParams{},
	)
	msgservice.RegisterMsgServiceDesc(registrar, &_Msg_serviceDesc)
}
