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

	// 2. Validate peer ID format
	if !types.ValidatePeerID(msg.PeerId) {
		return nil, errorsmod.Wrapf(types.ErrInvalidPeerID, "peer ID %q must be lowercase alphanumeric + hyphens + dots, 3-64 chars", msg.PeerId)
	}

	// 3. Validate peer type
	if msg.Type == types.PeerType_PEER_TYPE_UNSPECIFIED {
		return nil, errorsmod.Wrap(types.ErrPeerTypeMismatch, "peer type must be specified")
	}

	// 4. Check peer doesn't already exist (or is REMOVED — allow re-registration)
	existingPeer, err := k.Peers.Get(ctx, msg.PeerId)
	if err == nil {
		if existingPeer.Status != types.PeerStatus_PEER_STATUS_REMOVED {
			return nil, errorsmod.Wrapf(types.ErrPeerAlreadyExists, "peer %q already exists with status %s", msg.PeerId, existingPeer.Status)
		}
		// If REMOVED, check it's not still in cleanup queue
		hasRemoval, _ := k.PeerRemovalQueue.Has(ctx, msg.PeerId)
		if hasRemoval {
			return nil, errorsmod.Wrapf(types.ErrPeerCleanupInProgress, "peer %q removal cleanup still in progress", msg.PeerId)
		}
	}

	// 4b. Reject duplicate IBC channel bindings (non-REMOVED peers)
	if msg.IbcChannelId != "" {
		err := k.Peers.Walk(ctx, nil, func(_ string, p types.Peer) (bool, error) {
			if p.Status != types.PeerStatus_PEER_STATUS_REMOVED && p.IbcChannelId == msg.IbcChannelId && p.Id != msg.PeerId {
				return true, errorsmod.Wrap(types.ErrInvalidRequest, "channel id already bound to another peer")
			}
			return false, nil
		})
		if err != nil {
			return nil, err
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// 5. Create peer with PENDING status. peer_identity is optional; when
	// supplied for SPARK_DREAM peers, we pre-register IBC voucher metadata
	// below (step 6b).
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
	}

	if err := k.Peers.Set(ctx, msg.PeerId, peer); err != nil {
		return nil, err
	}

	// 6b. Pre-register IBC voucher metadata for SPARK_DREAM peers that
	// supplied a peer_identity and an ibc_channel_id (spec §9.2). Skipped for
	// non-Spark-Dream peers or when identity/channel is missing. Errors here
	// do not fail registration — metadata is informational only.
	if msg.Type == types.PeerType_PEER_TYPE_SPARK_DREAM &&
		msg.IbcChannelId != "" &&
		msg.PeerIdentity != nil &&
		msg.PeerIdentity.BondDenom != "" {
		if err := k.preRegisterIBCVoucherMetadata(ctx, peer); err != nil {
			// Log via event but do not fail; the peer record is the source of truth.
			sdkCtx.EventManager().EmitEvent(
				sdk.NewEvent(
					"federation_peer_metadata_skipped",
					sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
					sdk.NewAttribute("reason", err.Error()),
				),
			)
		}
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
// IBC channel, and registers DenomMetadata so wallets render <SYMBOL>.ibc
// instead of ibc/<hash>. See x-identity-spec.md §9.2.
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
		Trace: []ibctransfertypes.Hop{{PortId: "transfer", ChannelId: peer.IbcChannelId}},
	}
	ibcDenom := denom.IBCDenom()
	if _, ok := k.bankKeeper.GetDenomMetaData(ctx, ibcDenom); ok {
		return nil // already registered, skip
	}
	symbol := id.BondDisplaySymbol + ".ibc"
	display := strings.ToLower(id.BondDisplaySymbol) + ".ibc"
	meta := banktypes.Metadata{
		Description: fmt.Sprintf("%s (IBC voucher), sourced from peer chain %s via %s",
			id.BondDisplayName, peer.Id, peer.IbcChannelId),
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: ibcDenom, Exponent: 0},
			{Denom: display, Exponent: id.BondDisplayDecimals},
		},
		Base:    ibcDenom,
		Display: display,
		Name:    id.BondDisplayName + " (IBC)",
		Symbol:  symbol,
	}
	k.bankKeeper.SetDenomMetaData(ctx, meta)
	return nil
}
