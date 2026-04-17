package federation

import (
	"fmt"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
)

// IBCModule implements the ICS26 interface for interchain accounts host chains
type IBCModule struct {
	cdc    codec.Codec
	keeper keeper.Keeper
}

// NewIBCModule creates a new IBCModule given the associated keeper
func NewIBCModule(cdc codec.Codec, k keeper.Keeper) IBCModule {
	return IBCModule{
		cdc:    cdc,
		keeper: k,
	}
}

// OnChanOpenInit implements the IBCModule interface
func (im IBCModule) OnChanOpenInit(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionHops []string,
	portID string,
	channelID string,
	counterparty channeltypes.Counterparty,
	version string,
) (string, error) {
	if version != types.Version {
		return "", errorsmod.Wrapf(types.ErrInvalidVersion, "got %s, expected %s", version, types.Version)
	}

	return version, nil
}

// OnChanOpenTry implements the IBCModule interface
func (im IBCModule) OnChanOpenTry(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionHops []string,
	portID,
	channelID string,
	counterparty channeltypes.Counterparty,
	counterpartyVersion string,
) (string, error) {
	if counterpartyVersion != types.Version {
		return "", errorsmod.Wrapf(types.ErrInvalidVersion, "invalid counterparty version: got: %s, expected %s", counterpartyVersion, types.Version)
	}

	return counterpartyVersion, nil
}

// OnChanOpenAck implements the IBCModule interface
func (im IBCModule) OnChanOpenAck(
	ctx sdk.Context,
	portID,
	channelID,
	counterpartyChannelID,
	counterpartyVersion string,
) error {
	if counterpartyVersion != types.Version {
		return errorsmod.Wrapf(types.ErrInvalidVersion, "invalid counterparty version: %s, expected %s", counterpartyVersion, types.Version)
	}
	return nil
}

// OnChanOpenConfirm implements the IBCModule interface
func (im IBCModule) OnChanOpenConfirm(
	ctx sdk.Context,
	portID,
	channelID string,
) error {
	return nil
}

// OnChanCloseInit implements the IBCModule interface
func (im IBCModule) OnChanCloseInit(
	ctx sdk.Context,
	portID,
	channelID string,
) error {
	// Disallow user-initiated channel closing for channels
	return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "user cannot close channel")
}

// OnChanCloseConfirm implements the IBCModule interface
func (im IBCModule) OnChanCloseConfirm(
	ctx sdk.Context,
	portID,
	channelID string,
) error {
	return nil
}

// OnRecvPacket implements the IBCModule interface
func (im IBCModule) OnRecvPacket(
	ctx sdk.Context,
	channelVersion string,
	modulePacket channeltypes.Packet,
	relayer sdk.AccAddress,
) ibcexported.Acknowledgement {
	var modulePacketData types.FederationPacketData
	if err := modulePacketData.Unmarshal(modulePacket.GetData()); err != nil {
		return channeltypes.NewErrorAcknowledgement(errorsmod.Wrapf(sdkerrors.ErrUnknownRequest, "cannot unmarshal packet data: %s", err.Error()))
	}

	switch packet := modulePacketData.Packet.(type) {
	case *types.FederationPacketData_Content:
		if err := im.keeper.OnRecvContentPacket(ctx, modulePacket.GetSourcePort(), modulePacket.GetSourceChannel(), packet.Content); err != nil {
			return channeltypes.NewErrorAcknowledgement(err)
		}
		return channeltypes.NewResultAcknowledgement([]byte{0x01})

	case *types.FederationPacketData_ReputationQuery:
		resp, err := im.keeper.OnRecvReputationQueryPacket(ctx, packet.ReputationQuery)
		if err != nil {
			return channeltypes.NewErrorAcknowledgement(err)
		}
		respBytes, err := resp.Marshal()
		if err != nil {
			return channeltypes.NewErrorAcknowledgement(errorsmod.Wrap(err, "failed to marshal reputation response"))
		}
		return channeltypes.NewResultAcknowledgement(respBytes)

	case *types.FederationPacketData_IdentityVerification:
		ack, err := im.keeper.OnRecvIdentityVerificationPacket(ctx, modulePacket.GetSourceChannel(), packet.IdentityVerification)
		if err != nil {
			return channeltypes.NewErrorAcknowledgement(err)
		}
		ackBytes, _ := ack.Marshal()
		return channeltypes.NewResultAcknowledgement(ackBytes)

	case *types.FederationPacketData_IdentityConfirmation:
		if err := im.keeper.OnRecvIdentityConfirmPacket(ctx, modulePacket.GetSourceChannel(), packet.IdentityConfirmation); err != nil {
			return channeltypes.NewErrorAcknowledgement(err)
		}
		return channeltypes.NewResultAcknowledgement([]byte{0x01})

	default:
		err := fmt.Errorf("unrecognized %s packet type: %T", types.ModuleName, packet)
		return channeltypes.NewErrorAcknowledgement(err)
	}
}

// OnAcknowledgementPacket implements the IBCModule interface
func (im IBCModule) OnAcknowledgementPacket(
	ctx sdk.Context,
	channelVersion string,
	modulePacket channeltypes.Packet,
	acknowledgement []byte,
	relayer sdk.AccAddress,
) error {
	var modulePacketData types.FederationPacketData
	if err := modulePacketData.Unmarshal(modulePacket.GetData()); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrUnknownRequest, "cannot unmarshal packet data: %s", err.Error())
	}

	// Parse acknowledgement
	var ack channeltypes.Acknowledgement
	if err := im.cdc.UnmarshalJSON(acknowledgement, &ack); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrUnknownRequest, "cannot unmarshal acknowledgement: %s", err.Error())
	}

	switch packet := modulePacketData.Packet.(type) {
	case *types.FederationPacketData_ReputationQuery:
		// Successful ack carries ReputationResponseData in the result bytes
		resp := ack.GetResult()
		if resp == nil {
			ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeReputationQueryTimeout,
				sdk.NewAttribute("error", "acknowledgement was an error")))
			return nil
		}
		return im.keeper.OnAckReputationQuery(ctx, packet.ReputationQuery, resp)

	case *types.FederationPacketData_Content:
		// Content ack is a simple success/error — emit event on error
		if ack.GetError() != "" {
			ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeContentSendTimeout,
				sdk.NewAttribute("error", ack.GetError())))
		}
		return nil

	case *types.FederationPacketData_IdentityVerification:
		// Phase 1 ack carries IdentityVerificationAck
		if ack.GetError() != "" {
			ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeIdentityVerificationFailed,
				sdk.NewAttribute(types.AttributeKeyLocalAddress, packet.IdentityVerification.ClaimantAddress),
				sdk.NewAttribute("error", ack.GetError())))
		}
		return nil

	case *types.FederationPacketData_IdentityConfirmation:
		// Phase 2 confirmation ack — no special handling needed
		return nil

	default:
		errMsg := fmt.Sprintf("unrecognized %s packet type: %T", types.ModuleName, packet)
		return errorsmod.Wrap(sdkerrors.ErrUnknownRequest, errMsg)
	}
}

// OnTimeoutPacket implements the IBCModule interface
func (im IBCModule) OnTimeoutPacket(
	ctx sdk.Context,
	channelVersion string,
	modulePacket channeltypes.Packet,
	relayer sdk.AccAddress,
) error {
	var modulePacketData types.FederationPacketData
	if err := modulePacketData.Unmarshal(modulePacket.GetData()); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrUnknownRequest, "cannot unmarshal packet data: %s", err.Error())
	}

	switch packet := modulePacketData.Packet.(type) {
	case *types.FederationPacketData_Content:
		ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeContentSendTimeout,
			sdk.NewAttribute(types.AttributeKeyContentType, packet.Content.ContentType),
			sdk.NewAttribute(types.AttributeKeyCreator, packet.Content.Creator)))
		return nil

	case *types.FederationPacketData_ReputationQuery:
		ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeReputationQueryTimeout,
			sdk.NewAttribute(types.AttributeKeyLocalAddress, packet.ReputationQuery.Requester),
			sdk.NewAttribute("queried_address", packet.ReputationQuery.QueriedAddress)))
		return nil

	case *types.FederationPacketData_IdentityVerification:
		ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeIdentityVerificationTimeout,
			sdk.NewAttribute(types.AttributeKeyLocalAddress, packet.IdentityVerification.ClaimantAddress),
			sdk.NewAttribute(types.AttributeKeyRemoteIdentity, packet.IdentityVerification.ClaimedAddress)))
		return nil

	case *types.FederationPacketData_IdentityConfirmation:
		ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeIdentityConfirmationTimeout,
			sdk.NewAttribute(types.AttributeKeyLocalAddress, packet.IdentityConfirmation.ClaimantAddress)))
		return nil

	default:
		errMsg := fmt.Sprintf("unrecognized %s packet type: %T", types.ModuleName, packet)
		return errorsmod.Wrap(sdkerrors.ErrUnknownRequest, errMsg)
	}
}
