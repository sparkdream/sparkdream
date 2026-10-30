package types

import (
	commontypes "sparkdream/x/common/types"
	"unicode/utf8"
)

// Compiled caps on inbound IBC packet fields that have no governance param.
// Body, content_uri and protocol_metadata are bounded by the params
// (max_content_body_size and friends); these cover the remaining
// free-text fields a sister chain controls, so one packet can never write
// an unbounded state row.
const (
	// MaxInboundTitleSize caps ContentPacket.title (bytes, truncated).
	MaxInboundTitleSize = 512
	// MaxInboundCreatorNameSize caps ContentPacket.creator_name (bytes,
	// truncated).
	MaxInboundCreatorNameSize = 256
	// MaxInboundIdentifierSize caps ContentPacket.creator and
	// remote_content_id. These are identifiers, so an oversized one is
	// refused rather than truncated: a cut identifier would name someone
	// else.
	MaxInboundIdentifierSize = 512
	// ContentHashSize is the length of the SHA-256 digest every content
	// record must carry.
	ContentHashSize = 32
	// MaxPendingChallengesPerPeer caps the identity challenges one peer can
	// have pending at once. Each challenge is a state row that lives for
	// challenge_ttl, and a peer chain's users (or a misbehaving peer) could
	// otherwise plant them without bound.
	MaxPendingChallengesPerPeer = 10_000
	// MaxVerificationAttempts is how many Phase 1 challenge deliveries an
	// identity link gets before a timeout stops being retried (spec §8.4).
	MaxVerificationAttempts = 3
	// MaxReputationResponseTags caps the per-tag reputations returned in a
	// ReputationResponseData, keeping the acknowledgement bounded.
	MaxReputationResponseTags = 32
	// MaxInboundTagSize caps a tag name in a received ReputationResponseData;
	// a longer one is dropped from the stored attestation.
	MaxInboundTagSize = 64
)

// TruncateUTF8 returns s cut to at most max bytes without splitting a
// multi-byte rune, so a truncated proto string stays valid UTF-8.
func TruncateUTF8(s string, max uint64) string {
	if uint64(len(s)) <= max {
		return s
	}
	cut := commontypes.Saturate[int](max)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
