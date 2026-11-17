package types

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"cosmossdk.io/math"
)

var (
	backendNameRe = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)
	hexHashRe     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	arweaveTxRe   = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	contentTypeRe = regexp.MustCompile(`^[a-z0-9][a-z0-9!#$&^_.+-]{0,62}/[a-z0-9][a-z0-9!#$&^_.+-]{0,62}$`)
)

// urlChars are the characters RFC 3986 allows in a URI. No spaces, quotes,
// angle brackets, backslashes or control characters.
const urlChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~:/?#[]@!$&'()*+,;=%"

// ValidateHash checks a sha256 written as 64 lowercase hex characters.
func ValidateHash(h string) error {
	if !hexHashRe.MatchString(h) {
		return ErrInvalidHash
	}
	return nil
}

// ValidateSourceURL applies the address rules of spec section 3.2.
func ValidateSourceURL(raw string, maxBytes uint32) error {
	if raw == "" || uint64(len(raw)) > uint64(maxBytes) {
		return ErrInvalidURL.Wrapf("length must be in [1, %d]", maxBytes)
	}
	for i := 0; i < len(raw); i++ {
		if !strings.ContainsRune(urlChars, rune(raw[i])) {
			return ErrInvalidURL.Wrap("character not allowed in a URL")
		}
	}
	if strings.Contains(raw, "#") {
		return ErrInvalidURL.Wrap("fragment not allowed")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ErrInvalidURL.Wrap("not a URL")
	}
	if u.Scheme != "https" {
		return ErrInvalidURL.Wrap("scheme must be https")
	}
	if u.Hostname() == "" {
		return ErrInvalidURL.Wrap("host is required")
	}
	if u.User != nil {
		return ErrInvalidURL.Wrap("user or password part not allowed")
	}
	return nil
}

// ValidateLocator checks a locator against the backend's format.
func ValidateLocator(format LocatorFormat, locator string) error {
	switch format {
	case LocatorFormat_LOCATOR_FORMAT_ARWEAVE_TX:
		if !arweaveTxRe.MatchString(locator) {
			return ErrInvalidLocator.Wrap("an Arweave id is 43 base64url characters")
		}
		return nil
	default:
		return ErrInvalidLocator.Wrap("unknown locator format")
	}
}

// FindType returns the backend's entry for a content type.
func (b Backend) FindType(contentType string) (BackendType, bool) {
	for _, t := range b.Types {
		if t.ContentType == contentType {
			return t, true
		}
	}
	return BackendType{}, false
}

// CopyFee is max(min_fee, size x price_per_byte).
func (b Backend) CopyFee(sizeBytes uint64) math.Int {
	fee := b.PricePerByte.Mul(math.NewIntFromUint64(sizeBytes))
	if fee.LT(b.MinFee) {
		return b.MinFee
	}
	return fee
}

func nonNegative(name string, v math.Int) error {
	if v.IsNil() || v.IsNegative() {
		return fmt.Errorf("%s must not be negative", name)
	}
	return nil
}

// Validate checks everything about a backend that needs no other state.
func (b Backend) Validate() error {
	if !backendNameRe.MatchString(b.Name) {
		return fmt.Errorf("name must match [a-z0-9-]{1,32}")
	}
	if b.ServiceType == "" || len(b.ServiceType) > 64 {
		return fmt.Errorf("service_type is required")
	}
	if b.Kind != BackendKind_BACKEND_KIND_PERMANENT {
		return fmt.Errorf("kind must be BACKEND_KIND_PERMANENT")
	}
	if b.LocatorFormat != LocatorFormat_LOCATOR_FORMAT_ARWEAVE_TX {
		return fmt.Errorf("unknown locator_format")
	}
	for name, v := range map[string]math.Int{
		"price_per_byte": b.PricePerByte, "min_fee": b.MinFee,
		"min_price_per_byte": b.MinPricePerByte, "max_price_per_byte": b.MaxPricePerByte,
		"min_min_fee": b.MinMinFee, "max_min_fee": b.MaxMinFee,
	} {
		if err := nonNegative(name, v); err != nil {
			return err
		}
	}
	if b.MinPricePerByte.GT(b.MaxPricePerByte) || b.MinMinFee.GT(b.MaxMinFee) {
		return fmt.Errorf("a price floor is above its ceiling")
	}
	if err := b.ValidatePrices(b.PricePerByte, b.MinFee); err != nil {
		return err
	}
	if b.DeclinesToClose == 0 {
		return fmt.Errorf("declines_to_close must be positive")
	}
	if len(b.Types) > 64 {
		return fmt.Errorf("too many content types")
	}
	seen := make(map[string]bool, len(b.Types))
	for _, t := range b.Types {
		if !contentTypeRe.MatchString(t.ContentType) {
			return fmt.Errorf("invalid content type %q", t.ContentType)
		}
		if seen[t.ContentType] {
			return fmt.Errorf("content type %q listed twice", t.ContentType)
		}
		seen[t.ContentType] = true
		if t.MaxSize == 0 {
			return fmt.Errorf("max_size of %q must be positive", t.ContentType)
		}
	}
	return nil
}

// ValidatePrices checks a price pair against the backend's governance bounds.
func (b Backend) ValidatePrices(pricePerByte, minFee math.Int) error {
	if pricePerByte.IsNil() || minFee.IsNil() {
		return ErrPriceOutOfBounds.Wrap("price is required")
	}
	if pricePerByte.LT(b.MinPricePerByte) || pricePerByte.GT(b.MaxPricePerByte) {
		return ErrPriceOutOfBounds.Wrapf("price_per_byte must be in [%s, %s]", b.MinPricePerByte, b.MaxPricePerByte)
	}
	if minFee.LT(b.MinMinFee) || minFee.GT(b.MaxMinFee) {
		return ErrPriceOutOfBounds.Wrapf("min_fee must be in [%s, %s]", b.MinMinFee, b.MaxMinFee)
	}
	return nil
}

// DefaultArweaveBackend is the launch backend: still PNG and JPEG images,
// scanned, up to 10 MiB each (what one Arweave transaction carries inline).
func DefaultArweaveBackend() Backend {
	const maxSize = uint64(10 << 20)
	return Backend{
		Name:            ArweaveBackend,
		ServiceType:     ArweaveServiceType,
		Kind:            BackendKind_BACKEND_KIND_PERMANENT,
		LocatorFormat:   LocatorFormat_LOCATOR_FORMAT_ARWEAVE_TX,
		Enabled:         true,
		PricePerByte:    math.NewInt(10),
		MinFee:          math.NewInt(1_000_000),
		MinPricePerByte: math.NewInt(1),
		MaxPricePerByte: math.NewInt(1_000_000),
		MinMinFee:       math.NewInt(100_000),
		MaxMinFee:       math.NewInt(1_000_000_000),
		DeclinesToClose: 1,
		Types: []BackendType{
			{ContentType: "image/png", Scanned: true, MaxSize: maxSize},
			{ContentType: "image/jpeg", Scanned: true, MaxSize: maxSize},
		},
	}
}

// IsLive reports whether a status holds the request's hash: one request per
// (backend, sha256) among these.
func (s RequestStatus) IsLive() bool {
	switch s {
	case RequestStatus_REQUEST_STATUS_PENDING, RequestStatus_REQUEST_STATUS_CLAIMED,
		RequestStatus_REQUEST_STATUS_COMPLETED, RequestStatus_REQUEST_STATUS_SETTLED:
		return true
	}
	return false
}

// IsOpen reports whether a request is still being worked on.
func (s RequestStatus) IsOpen() bool {
	switch s {
	case RequestStatus_REQUEST_STATUS_PENDING, RequestStatus_REQUEST_STATUS_CLAIMED,
		RequestStatus_REQUEST_STATUS_COMPLETED:
		return true
	}
	return false
}

// IsTerminal reports whether a request is closed without a copy.
func (s RequestStatus) IsTerminal() bool {
	switch s {
	case RequestStatus_REQUEST_STATUS_REJECTED, RequestStatus_REQUEST_STATUS_EXPIRED,
		RequestStatus_REQUEST_STATUS_CANCELLED:
		return true
	}
	return false
}

// RequiredAttestations is the scanner quorum for a requester's trust level:
// the elevated quorum, when one is set, for NEW and PROVISIONAL members.
func RequiredAttestations(quorum, elevated, trustLevel uint32) uint32 {
	if elevated > 0 && trustLevel <= 1 {
		return elevated
	}
	return quorum
}
