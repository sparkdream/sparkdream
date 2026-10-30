package types

// IBC events
const (
	EventTypeTimeout = "timeout"

	// EventTypeAckDropped marks an acknowledgement for a packet this chain
	// sent that could not be applied (undecodable, from a peer no longer
	// bound to the channel, or about a link that changed since). The ack is
	// dropped rather than failed so the packet commitment still clears.
	EventTypeAckDropped = "federation_ack_dropped"

	AttributeKeyAckSuccess = "success"
	AttributeKeyAck        = "acknowledgement"
	AttributeKeyAckError   = "error"
)
