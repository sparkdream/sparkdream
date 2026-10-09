package types

import (
	"crypto/sha256"
	"fmt"
	"regexp"

	"cosmossdk.io/math"
)

// Media labelling, rules version 1. The chain labels every content record at
// write time from information it already has (content_type, which URI fields
// are set) plus one deterministic data-URI scan of uncompressed text. It never
// decompresses GZIP/ZSTD bodies and never parses or fetches links: that is the
// off-chain scanners' job. See docs/content-scanning.md §3.

// MediaRulesVersion is the labelling rules version stamped into every
// record's media_rules_version. Bump it only together with a change to the
// rules below, and relabel existing records when you do.
const MediaRulesVersion uint32 = 1

// dataURIPattern matches the RFC 2397 shape data:[<mediatype>][;base64],<data>
// rather than the bare substring "data:", so prose like "metadata:" or
// "Data: 5 records" is not flagged. "data:" must not follow a URI-scheme
// character and must reach a comma with no whitespace in between. Go's
// regexp is RE2 (linear time, no backtracking), so the result and cost are
// identical on every node.
var dataURIPattern = regexp.MustCompile(`(?i)(^|[^a-z0-9+.-])data:[^\s,]{0,256},`)

// ContainsDataURI reports whether s contains a data URI under rules version 1.
func ContainsDataURI(s string) bool {
	return dataURIPattern.MatchString(s)
}

// MediaLabels is the chain-computed label set written onto a content record.
type MediaLabels struct {
	Flags        uint32
	RulesVersion uint32
	BodyHash     []byte
}

// BodyHash returns the sha256 of body exactly as stored (post-encoding,
// pre-decompression), or nil for an empty body.
func BodyHash(body string) []byte {
	if body == "" {
		return nil
	}
	h := sha256.Sum256([]byte(body))
	return h[:]
}

// LabelBody labels a blog/forum body by its content type: compressed and
// off-chain types are flagged from content_type alone, everything else is
// scanned as text for data URIs. An empty body carries no flags.
func LabelBody(contentType ContentType, body string) MediaLabels {
	l := MediaLabels{RulesVersion: MediaRulesVersion, BodyHash: BodyHash(body)}
	if body == "" {
		return l
	}
	switch contentType {
	case ContentType_CONTENT_TYPE_GZIP, ContentType_CONTENT_TYPE_ZSTD:
		l.Flags = uint32(MediaFlag_MEDIA_FLAG_COMPRESSED)
	case ContentType_CONTENT_TYPE_IPFS, ContentType_CONTENT_TYPE_ARWEAVE,
		ContentType_CONTENT_TYPE_FILECOIN, ContentType_CONTENT_TYPE_JACKAL:
		l.Flags = uint32(MediaFlag_MEDIA_FLAG_OFFCHAIN_REF)
	default:
		if ContainsDataURI(body) {
			l.Flags = uint32(MediaFlag_MEDIA_FLAG_INLINE_DATA)
		}
	}
	return l
}

// LabelTombstone labels a body the chain itself wrote on deletion or expiry
// (empty, or a fixed placeholder). It is never media, whatever the record's
// content_type says.
func LabelTombstone(body string) MediaLabels {
	return MediaLabels{RulesVersion: MediaRulesVersion, BodyHash: BodyHash(body)}
}

// LabelFederatedContent labels inbound federated content: the body is
// scanned as text and a set content_uri marks an external reference.
// Federated records carry the peer's content_hash, so no body hash is
// computed here.
func LabelFederatedContent(body, contentURI string) MediaLabels {
	l := MediaLabels{RulesVersion: MediaRulesVersion}
	if ContainsDataURI(body) {
		l.Flags |= uint32(MediaFlag_MEDIA_FLAG_INLINE_DATA)
	}
	if contentURI != "" {
		l.Flags |= uint32(MediaFlag_MEDIA_FLAG_EXTERNAL_URI)
	}
	return l
}

// Media posting rules (docs/content-scanning.md §9). Content modules that
// withhold media bodies (blog, forum) gate media writes with the same three
// params: media_min_trust_level, media_author_bond_min and media_scan_fee.

// DefaultMediaMinTrustLevel is TRUST_LEVEL_PROVISIONAL: invited members have
// to earn a little standing (or bond) before their media reaches scanners.
const DefaultMediaMinTrustLevel uint32 = 1

// DefaultMediaAuthorBondMin is 100 DREAM (micro-DREAM).
var DefaultMediaAuthorBondMin = math.NewInt(100_000_000)

// MediaGate is the input to CheckMediaPermitted.
type MediaGate struct {
	Flags      uint32   // media labels of the write
	Anonymous  bool     // author is the shield module (anonymous content)
	Member     bool     // author is an active member
	TrustLevel uint32   // author's trust level (meaningful only for members)
	AuthorBond math.Int // DREAM author bond attached to the write (nil = none)
	MinTrust   uint32   // media_min_trust_level
	BondMin    math.Int // media_author_bond_min (nil/zero = bond path off)
}

// CheckMediaPermitted returns "" when the write may proceed, otherwise the
// reason it may not. Plain text always passes. Media needs an active member
// at media_min_trust_level, or one who attaches an author bond of at least
// media_author_bond_min. Anonymous media is never permitted: it would
// combine the riskiest content with the least accountability.
func CheckMediaPermitted(g MediaGate) string {
	if g.Flags == 0 {
		return ""
	}
	if g.Anonymous {
		return "anonymous content cannot carry media"
	}
	if !g.Member {
		return "only active members can post media"
	}
	if g.TrustLevel >= g.MinTrust {
		return ""
	}
	if !g.BondMin.IsNil() && g.BondMin.IsPositive() && !g.AuthorBond.IsNil() && g.AuthorBond.GTE(g.BondMin) {
		return ""
	}
	if !g.BondMin.IsNil() && g.BondMin.IsPositive() {
		return fmt.Sprintf("media needs trust level %d or an author bond of at least %s", g.MinTrust, g.BondMin)
	}
	return fmt.Sprintf("media needs trust level %d", g.MinTrust)
}

// ValidateMediaParams checks the shared media params.
func ValidateMediaParams(minTrust uint32, bondMin, scanFee math.Int) error {
	if minTrust > 4 {
		return fmt.Errorf("media_min_trust_level must be 0-4, got %d", minTrust)
	}
	if !bondMin.IsNil() && bondMin.IsNegative() {
		return fmt.Errorf("media_author_bond_min must be >= 0, got %s", bondMin)
	}
	if !scanFee.IsNil() && scanFee.IsNegative() {
		return fmt.Errorf("media_scan_fee must be >= 0, got %s", scanFee)
	}
	return nil
}
