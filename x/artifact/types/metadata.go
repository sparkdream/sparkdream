package types

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	errorsmod "cosmossdk.io/errors"

	commontypes "sparkdream/x/common/types"
)

var (
	attrKeyPattern = regexp.MustCompile(`^[a-z0-9_]+$`)
	symbolPattern  = regexp.MustCompile(`^[A-Z0-9]*$`)
	hexHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ValidateText checks a metadata string: valid UTF-8, within maxLen bytes,
// no control characters (newline and tab allowed only when multiline), and no
// data URI anywhere (docs/x-artifact-spec.md §7.9).
func ValidateText(field, s string, maxLen uint32, multiline bool) error {
	if uint32(len(s)) > maxLen {
		return errorsmod.Wrapf(ErrInvalidMetadata, "%s exceeds %d bytes", field, maxLen)
	}
	if !utf8.ValidString(s) {
		return errorsmod.Wrapf(ErrInvalidMetadata, "%s is not valid UTF-8", field)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			if multiline && (r == '\n' || r == '\t') {
				continue
			}
			return errorsmod.Wrapf(ErrInvalidMetadata, "%s contains a control character", field)
		}
	}
	if commontypes.ContainsDataURI(s) {
		return errorsmod.Wrapf(ErrInlineData, "%s contains a data URI", field)
	}
	return nil
}

// ValidateSymbol checks a class symbol: [A-Z0-9], within maxLen.
func ValidateSymbol(s string, maxLen uint32) error {
	if uint32(len(s)) > maxLen {
		return errorsmod.Wrapf(ErrInvalidMetadata, "symbol exceeds %d characters", maxLen)
	}
	if !symbolPattern.MatchString(s) {
		return errorsmod.Wrap(ErrInvalidMetadata, "symbol must match [A-Z0-9]")
	}
	return nil
}

// ValidateURIHash checks an optional lowercase hex SHA-256.
func ValidateURIHash(field, h string) error {
	if h == "" {
		return nil
	}
	if !hexHashPattern.MatchString(h) {
		return errorsmod.Wrapf(ErrInvalidMetadata, "%s must be 64 lowercase hex characters", field)
	}
	return nil
}

// ValidateURI checks an optional media URI: absolute, allowed scheme, no
// whitespace, no userinfo, ASCII non-IP host for hierarchical schemes, and
// no data URI (docs/x-artifact-spec.md §7.9).
func ValidateURI(field, raw string, p Params) error {
	if raw == "" {
		return nil
	}
	if uint32(len(raw)) > p.MaxUriLength {
		return errorsmod.Wrapf(ErrInvalidMetadata, "%s exceeds %d bytes", field, p.MaxUriLength)
	}
	if commontypes.ContainsDataURI(raw) {
		return errorsmod.Wrapf(ErrInlineData, "%s contains a data URI", field)
	}
	for _, r := range raw {
		if r > unicode.MaxASCII || unicode.IsSpace(r) || unicode.IsControl(r) {
			return errorsmod.Wrapf(ErrInvalidMetadata, "%s must be printable ASCII without whitespace", field)
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errorsmod.Wrapf(ErrInvalidMetadata, "%s: %v", field, err)
	}
	// url.Parse lowercases the scheme, so check the raw prefix.
	scheme := u.Scheme
	if scheme == "" || !strings.HasPrefix(raw, scheme+":") {
		return errorsmod.Wrapf(ErrInvalidMetadata, "%s must be an absolute URI with a lowercase scheme", field)
	}
	allowed := false
	for _, s := range p.AllowedUriSchemes {
		if s == scheme {
			allowed = true
			break
		}
	}
	if !allowed {
		return errorsmod.Wrapf(ErrInvalidMetadata, "%s scheme %q is not allowed", field, scheme)
	}
	if u.User != nil {
		return errorsmod.Wrapf(ErrInvalidMetadata, "%s must not contain userinfo", field)
	}
	if u.Opaque != "" {
		return errorsmod.Wrapf(ErrInvalidMetadata, "%s must be of the form scheme://...", field)
	}
	host := u.Hostname()
	if host == "" {
		return errorsmod.Wrapf(ErrInvalidMetadata, "%s must have a host or content identifier", field)
	}
	if net.ParseIP(host) != nil || strings.HasPrefix(u.Host, "[") {
		return errorsmod.Wrapf(ErrInvalidMetadata, "%s must not use an IP-literal host", field)
	}
	return nil
}

// ValidateTokenMetadata checks every field of a token's metadata.
func ValidateTokenMetadata(m TokenMetadata, p Params) error {
	if err := ValidateText("name", m.Name, p.MaxNameLength, false); err != nil {
		return err
	}
	if err := ValidateText("description", m.Description, p.MaxDescriptionLength, true); err != nil {
		return err
	}
	if err := ValidateURI("uri", m.Uri, p); err != nil {
		return err
	}
	if err := ValidateURIHash("uri_hash", m.UriHash); err != nil {
		return err
	}
	if uint32(len(m.Attributes)) > p.MaxAttributes {
		return errorsmod.Wrapf(ErrInvalidMetadata, "at most %d attributes", p.MaxAttributes)
	}
	seen := make(map[string]bool, len(m.Attributes))
	for _, a := range m.Attributes {
		if a.Key == "" || uint32(len(a.Key)) > p.MaxAttributeKeyLength || !attrKeyPattern.MatchString(a.Key) {
			return errorsmod.Wrapf(ErrInvalidMetadata, "attribute key %q must match [a-z0-9_]{1,%d}", a.Key, p.MaxAttributeKeyLength)
		}
		if seen[a.Key] {
			return errorsmod.Wrapf(ErrInvalidMetadata, "duplicate attribute key %q", a.Key)
		}
		seen[a.Key] = true
		if err := ValidateText("attribute "+a.Key, a.Value, p.MaxAttributeValueLength, false); err != nil {
			return err
		}
	}
	return nil
}

// MetadataHash is the lowercase hex SHA-256 of the deterministic proto
// encoding of m. MsgBuy and MsgAcceptIncoming can pin it (§12.2).
func MetadataHash(m TokenMetadata) string {
	bz, err := m.Marshal()
	if err != nil {
		// TokenMetadata has only scalar and repeated message fields; Marshal
		// cannot fail on a well-formed value.
		panic(err)
	}
	sum := sha256.Sum256(bz)
	return hex.EncodeToString(sum[:])
}

// LabelClass computes the media flags for a class (§7.9).
func LabelClass(c Class) uint32 {
	if c.Uri != "" || c.TokenUriBase != "" {
		return uint32(commontypes.MediaFlag_MEDIA_FLAG_EXTERNAL_URI)
	}
	return uint32(commontypes.MediaFlag_MEDIA_FLAG_NONE)
}

// LabelToken computes the media flags for token metadata (§7.9).
func LabelToken(m TokenMetadata) uint32 {
	if m.Uri != "" {
		return uint32(commontypes.MediaFlag_MEDIA_FLAG_EXTERNAL_URI)
	}
	return uint32(commontypes.MediaFlag_MEDIA_FLAG_NONE)
}

// ScrubbedMetadata is what a scrubbed token's metadata becomes.
func ScrubbedMetadata() TokenMetadata { return TokenMetadata{} }
