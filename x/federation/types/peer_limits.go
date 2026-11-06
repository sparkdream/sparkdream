package types

import (
	"fmt"
	"unicode"

	errorsmod "cosmossdk.io/errors"
	cmttypes "github.com/cometbft/cometbft/types"
)

// Compiled caps on the descriptive peer fields. A peer record is read on
// every federation packet, ack and timeout and by the app's relay-refund
// ante decorator, and a single Operations Committee member may write these
// fields (MsgRegisterPeer, MsgUpdatePeerInfo), so they must stay small.
const (
	// MaxPeerDisplayNameSize caps Peer.display_name (bytes).
	MaxPeerDisplayNameSize = 128
	// MaxPeerMetadataSize caps Peer.metadata (bytes).
	MaxPeerMetadataSize = 4096
)

// ValidatePeerInfo checks a peer's display_name and metadata against the
// compiled caps.
func ValidatePeerInfo(displayName, metadata string) error {
	if len(displayName) > MaxPeerDisplayNameSize {
		return fmt.Errorf("display_name is %d bytes, max %d", len(displayName), MaxPeerDisplayNameSize)
	}
	if len(metadata) > MaxPeerMetadataSize {
		return fmt.Errorf("metadata is %d bytes, max %d", len(metadata), MaxPeerMetadataSize)
	}
	return nil
}

// MaxPeerChainIdSize caps Peer.expected_chain_id (bytes); CometBFT refuses
// longer chain ids in a genesis document, so a longer pin can never match.
const MaxPeerChainIdSize = cmttypes.MaxChainIDLen

// ValidateChainID checks that id is a usable CometBFT chain id: non-empty,
// at most MaxPeerChainIdSize bytes, no whitespace or control characters.
func ValidateChainID(id string) error {
	if id == "" {
		return fmt.Errorf("chain id is empty")
	}
	if len(id) > MaxPeerChainIdSize {
		return fmt.Errorf("chain id is %d bytes, max %d", len(id), MaxPeerChainIdSize)
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("chain id %q contains whitespace or control characters", id)
		}
	}
	return nil
}

// ValidateExpectedChainID checks a peer's expected_chain_id against its
// type: a Spark Dream (IBC) peer must carry a valid chain id, since
// MsgResumePeer pins the federation channel's light client to it
// (FED-106); every other peer type has no chain and must leave it empty.
func ValidateExpectedChainID(peerType PeerType, expected string) error {
	if peerType == PeerType_PEER_TYPE_SPARK_DREAM {
		if err := ValidateChainID(expected); err != nil {
			return fmt.Errorf("expected_chain_id is required for a Spark Dream peer: %w", err)
		}
		return nil
	}
	if expected != "" {
		return fmt.Errorf("expected_chain_id is only for Spark Dream peers (got %q for %s)", expected, peerType)
	}
	return nil
}

// ValidateBasic refuses an oversized display_name or metadata and a
// missing, malformed or misplaced expected_chain_id before the message
// reaches the handler (which checks again).
func (msg *MsgRegisterPeer) ValidateBasic() error {
	if err := ValidatePeerInfo(msg.DisplayName, msg.Metadata); err != nil {
		return errorsmod.Wrap(ErrInvalidRequest, err.Error())
	}
	if err := ValidateExpectedChainID(msg.Type, msg.ExpectedChainId); err != nil {
		return errorsmod.Wrap(ErrInvalidRequest, err.Error())
	}
	return nil
}

// ValidateBasic refuses an oversized display_name or metadata and a
// malformed expected_chain_id before the message reaches the handler
// (which checks again). The peer's type is not known here, so only the
// chain id's format is checked; the handler decides whether a non-empty
// value is allowed for this peer.
func (msg *MsgUpdatePeerInfo) ValidateBasic() error {
	if err := ValidatePeerInfo(msg.DisplayName, msg.Metadata); err != nil {
		return errorsmod.Wrap(ErrInvalidRequest, err.Error())
	}
	if msg.ExpectedChainId != "" {
		if err := ValidateChainID(msg.ExpectedChainId); err != nil {
			return errorsmod.Wrap(ErrInvalidRequest, err.Error())
		}
	}
	return nil
}
