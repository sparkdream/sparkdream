package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterInterfaces(registrar codectypes.InterfaceRegistry) {
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgPinReply{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgPinPost{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRemoveReaction{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgReact{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnhideReply{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgHideReply{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDeleteReply{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateReply{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateReply{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnhidePost{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgHidePost{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDeletePost{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdatePost{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreatePost{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateParams{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateOperationalParams{},
	)
	msgservice.RegisterMsgServiceDesc(registrar, &_Msg_serviceDesc)
}
