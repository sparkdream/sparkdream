package types

import (
	"net/url"
	"strings"
)

// AllIdentities in PeerPolicy.allowed_identities admits any author.
const AllIdentities = "*"

// MaxAllowedIdentities caps PeerPolicy.allowed_identities: the list is
// checked on every bridged submission and edited by committee proposal. A
// larger community's list belongs in a curation collection.
const MaxAllowedIdentities = 256

// NormalizeAuthorIdentity reduces the ways an author is written to one form,
// "user@host" in lower case, so an allow-list entry or a curation item
// matches the creator_identity a bridge submits:
//
//	@user@host, user@host        a fediverse handle
//	https://host/@user           a Mastodon profile URL
//	https://host/users/user      an ActivityPub actor id
//
// ok is false for anything else (an empty string, a URL of another shape).
func NormalizeAuthorIdentity(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	if strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") {
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			return "", false
		}
		host := strings.ToLower(u.Hostname())
		path := strings.Trim(u.Path, "/")
		switch {
		case strings.HasPrefix(path, "@") && !strings.Contains(path, "/"):
			return strings.ToLower(strings.TrimPrefix(path, "@")) + "@" + host, true
		case strings.HasPrefix(path, "users/") && strings.Count(path, "/") == 1:
			return strings.ToLower(strings.TrimPrefix(path, "users/")) + "@" + host, true
		}
		return "", false
	}
	h := strings.TrimPrefix(s, "@")
	user, host, found := strings.Cut(h, "@")
	if !found || user == "" || host == "" || strings.ContainsAny(user+host, "@/ ") {
		return "", false
	}
	return strings.ToLower(user) + "@" + strings.ToLower(host), true
}

// ValidateAllowedIdentities checks PeerPolicy.allowed_identities entries.
func ValidateAllowedIdentities(entries []string) error {
	for _, e := range entries {
		if e == AllIdentities {
			continue
		}
		if _, ok := NormalizeAuthorIdentity(e); !ok {
			return ErrInvalidAllowedIdentity.Wrapf("%q", e)
		}
	}
	return nil
}
