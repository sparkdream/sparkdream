package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterInterfaces(registrar codectypes.InterfaceRegistry) {
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgResolveDispute{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCancel{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgVerify{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgReveal{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgWithdraw{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgStake{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgReject{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgApprove{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgPropose{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateParams{},
	)
	msgservice.RegisterMsgServiceDesc(registrar, &_Msg_serviceDesc)
}
