package keeper

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
)

func (k msgServer) RegisterPeer(ctx context.Context, msg *types.MsgRegisterPeer) (*types.MsgRegisterPeerResponse, error) {
	authorityBytes, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// 1. Verify authority is governance or Commons Council policy address
	if !bytes.Equal(k.authority, authorityBytes) {
		if k.late.commonsKeeper == nil || !k.late.commonsKeeper.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations") {
			return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be governance or Commons Council")
		}
	}

	// 1b. display_name and metadata are read with the peer record on every
	// federation packet, so their size is capped (also in ValidateBasic).
	if err := types.ValidatePeerInfo(msg.DisplayName, msg.Metadata); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, err.Error())
	}

	// 2. Validate peer ID format
	if !types.ValidatePeerID(msg.PeerId) {
		return nil, errorsmod.Wrapf(types.ErrInvalidPeerID, "peer ID %q must be lowercase alphanumeric + hyphens + dots, 3-64 chars", msg.PeerId)
	}

	// 3. Validate peer type
	if msg.Type == types.PeerType_PEER_TYPE_UNSPECIFIED {
		return nil, errorsmod.Wrap(types.ErrPeerTypeMismatch, "peer type must be specified")
	}

	// 3b. A Spark Dream peer is pinned to the chain behind its federation
	// channel: MsgResumePeer compares the channel's light client with
	// expected_chain_id (FED-106). Other peer types have no chain and must
	// leave it empty (also in ValidateBasic).
	if err := types.ValidateExpectedChainID(msg.Type, msg.ExpectedChainId); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, err.Error())
	}

	// 4. Check peer doesn't already exist (or is REMOVED — allow re-registration)
	existingPeer, err := k.Peers.Get(ctx, msg.PeerId)
	if err == nil {
		if existingPeer.Status != types.PeerStatus_PEER_STATUS_REMOVED {
			return nil, errorsmod.Wrapf(types.ErrPeerAlreadyExists, "peer %q already exists with status %s", msg.PeerId, existingPeer.Status)
		}
		// If REMOVED, check it's not still in cleanup queue
		hasRemoval, getErr := k.PeerRemovalQueue.Has(ctx, msg.PeerId)
		if getErr != nil {
			return nil, getErr
		}
		if hasRemoval {
			return nil, errorsmod.Wrapf(types.ErrPeerCleanupInProgress, "peer %q removal cleanup still in progress", msg.PeerId)
		}
	}

	// 4b. Reject duplicate IBC channel bindings (non-REMOVED peers)
	if msg.IbcChannelId != "" {
		owner, err := k.peerForChannel(ctx, msg.IbcChannelId)
		if err != nil {
			return nil, err
		}
		if owner != "" && owner != msg.PeerId {
			return nil, errorsmod.Wrap(types.ErrInvalidRequest, "channel id already bound to another peer")
		}
	}

	// 4c. The transfer channel, when given, must be a well-formed channel id.
	// It is not checked for uniqueness: ICS-20 vouchers are keyed per
	// channel, so two peers sharing one would only share a metadata entry.
	if msg.IbcTransferChannelId != "" && !channeltypes.IsValidChannelID(msg.IbcTransferChannelId) {
		return nil, errorsmod.Wrapf(types.ErrInvalidRequest, "invalid ibc transfer channel id %q", msg.IbcTransferChannelId)
	}

	// 4d. peer_identity is optional (non-Spark-Dream peers have none), but
	// when supplied it must be a well-formed ChainIdentity: it is served
	// back by queries and drives the voucher metadata below, so a garbage
	// record must be rejected here rather than stored.
	if msg.PeerIdentity != nil {
		if err := msg.PeerIdentity.Validate(); err != nil {
			return nil, errorsmod.Wrap(types.ErrInvalidRequest, fmt.Sprintf("invalid peer_identity: %s", err))
		}
	}

	// 4e. controller_group, when given, must be an x/commons group policy
	//     address (spec §6.1 step 5). It becomes the controller of every
	//     bridge registered for this peer; dropping it would silently hand
	//     those operators to the Operations Committee instead.
	if msg.ControllerGroup != "" {
		if k.late.commonsKeeper == nil || !k.late.commonsKeeper.IsGroupPolicyAddress(ctx, msg.ControllerGroup) {
			return nil, errorsmod.Wrapf(types.ErrInvalidRequest, "controller_group %q is not a group policy address", msg.ControllerGroup)
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// 5. Create peer with PENDING status. peer_identity is optional; when
	// supplied for SPARK_DREAM peers together with a transfer channel, the
	// IBC voucher metadata is pre-registered at activation (ResumePeer),
	// once the transfer channel has been verified against expected_chain_id
	// (FED-201). Registration is 1-of-N and verifies nothing.
	peer := types.Peer{
		Id:           msg.PeerId,
		DisplayName:  msg.DisplayName,
		Type:         msg.Type,
		Status:       types.PeerStatus_PEER_STATUS_PENDING,
		IbcChannelId: msg.IbcChannelId,
		RegisteredAt: blockTime,
		RegisteredBy: msg.Authority,
		Metadata:     msg.Metadata,
		PeerIdentity: msg.PeerIdentity,

		IbcTransferChannelId: msg.IbcTransferChannelId,
		ControllerGroup:      msg.ControllerGroup,
		ExpectedChainId:      msg.ExpectedChainId,
	}

	if err := k.SetPeer(ctx, peer); err != nil {
		return nil, err
	}

	// 6. Create default PeerPolicy
	policy := types.PeerPolicy{
		PeerId: msg.PeerId,
	}
	if err := k.PeerPolicies.Set(ctx, msg.PeerId, policy); err != nil {
		return nil, err
	}

	// 7. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypePeerRegistered,
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyPeerType, msg.Type.String()),
			sdk.NewAttribute(types.AttributeKeyDisplayName, msg.DisplayName),
			sdk.NewAttribute(types.AttributeKeyRegisteredBy, msg.Authority),
		),
	)

	return &types.MsgRegisterPeerResponse{}, nil
}

// preRegisterIBCVoucherMetadata computes the canonical single-hop ICS-20
// voucher denom for peer SPARK arriving on this chain via the registered
// ICS-20 transfer channel, and registers DenomMetadata so wallets render <SYMBOL>.ibc
// instead of ibc/<hash>. See x-identity-spec.md §9.2. Called from
// ResumePeer once the transfer channel is verified (FED-201).
//
// Single-hop only: vouchers arriving via multi-hop relay paths produce a
// different denom hash and are not covered. Deferred to spec §18 future
// extension.
//
// Skips silently if metadata already exists at the computed denom key (e.g.,
// peer was previously registered against the same channel and the metadata
// survived a removal).
func (k Keeper) preRegisterIBCVoucherMetadata(ctx context.Context, peer types.Peer) error {
	id := peer.PeerIdentity
	if id == nil {
		return fmt.Errorf("peer identity not supplied")
	}
	denom := ibctransfertypes.Denom{
		Base:  id.BondDenom,
		Trace: []ibctransfertypes.Hop{{PortId: ibctransfertypes.PortID, ChannelId: peer.IbcTransferChannelId}},
	}
	ibcDenom := denom.IBCDenom()
	if _, ok := k.BankKeeper().GetDenomMetaData(ctx, ibcDenom); ok {
		return nil // already registered, skip
	}
	symbol := id.BondDisplaySymbol + ".ibc"
	display := strings.ToLower(id.BondDisplaySymbol) + ".ibc"
	meta := banktypes.Metadata{
		Description: fmt.Sprintf("%s (IBC voucher), sourced from peer chain %s via %s",
			id.BondDisplayName, peer.Id, peer.IbcTransferChannelId),
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: ibcDenom, Exponent: 0},
			{Denom: display, Exponent: id.BondDisplayDecimals},
		},
		Base:    ibcDenom,
		Display: display,
		Name:    id.BondDisplayName + " (IBC)",
		Symbol:  symbol,
	}
	k.BankKeeper().SetDenomMetaData(ctx, meta)
	return nil
}
