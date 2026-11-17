package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterInterfaces(registrar codectypes.InterfaceRegistry) {
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateOperationalParams{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateBackend{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRemoveScanner{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgApproveScanner{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRemoveOperator{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgApproveOperator{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgVoidCompletion{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnflagRemoved{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgFlagRemoved{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDeclineRequest{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCompleteRequest{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgExtendClaim{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgClaimRequest{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDisputeCompletion{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCancelRequest{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAddSource{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRequestPermanentCopy{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateParams{},
	)
	msgservice.RegisterMsgServiceDesc(registrar, &_Msg_serviceDesc)
}
