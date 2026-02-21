package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

func RegisterInterfaces(registrar codectypes.InterfaceRegistry) {
	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgStoreSRS{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterTLEShare{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSubmitDecryptionShare{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRevealVote{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSealedVote{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgVote{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCancelProposal{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateProposal{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateAnonymousProposal{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRotateVoterKey{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDeactivateVoter{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterVoter{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateParams{},
	)
	msgservice.RegisterMsgServiceDesc(registrar, &_Msg_serviceDesc)
}
