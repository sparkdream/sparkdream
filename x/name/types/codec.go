package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterInterfaces(registrar codectypes.InterfaceRegistry) {
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateName{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateParams{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateOperationalParams{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterName{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetPrimary{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgFileDispute{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgContestDispute{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgResolveDispute{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetDisplayName{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetTarget{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAcceptTarget{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgTransferName{},
	)

	msgservice.RegisterMsgServiceDesc(registrar, &_Msg_serviceDesc)
}
