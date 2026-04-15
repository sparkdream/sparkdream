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
		&MsgEscalateChallenge{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSubmitArbiterHash{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgChallengeVerification{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgVerifyContent{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnbondVerifier{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgBondVerifier{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRequestReputationAttestation{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgConfirmIdentityLink{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnlinkIdentity{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgLinkIdentity{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgModerateContent{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAttestOutbound{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgFederateContent{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSubmitFederatedContent{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgTopUpBridgeStake{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUnbondBridge{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateBridge{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSlashBridge{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRevokeBridge{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterBridge{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdatePeerPolicy{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgResumePeer{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSuspendPeer{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRemovePeer{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterPeer{},
	)

	registrar.RegisterImplementations((*sdk.Msg)(nil),
		&MsgUpdateParams{},
	)
	msgservice.RegisterMsgServiceDesc(registrar, &_Msg_serviceDesc)
}
