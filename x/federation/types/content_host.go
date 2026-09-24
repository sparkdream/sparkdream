package types

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// MaxContentHosts caps PeerPolicy.content_hosts. The list exists for the
// rare instance whose AS2 ids live on a host other than its account
// domain; a long list would mean the peer is really several instances,
// each of which should be its own peer.
const MaxContentHosts = 8

// ContentURIHostAllowed is the provenance rule for inbound ActivityPub
// content: content_uri must be an http(s) URL whose hostname is the peer
// id itself or one of the policy's content_hosts. The port and any
// userinfo are ignored, so "https://peer@elsewhere.example/" is judged
// by its real host, elsewhere.example. The chain enforces this on
// MsgSubmitFederatedContent; the bridge and verifier daemons apply the
// same function so all three agree on what belongs to a peer.
func ContentURIHostAllowed(peerID string, contentHosts []string, contentURI string) error {
	u, err := url.Parse(contentURI)
	if err != nil {
		return fmt.Errorf("content_uri %q is not a URL: %w", contentURI, err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("content_uri %q must be http(s), got scheme %q", contentURI, u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("content_uri %q has no host", contentURI)
	}
	if host == peerID || slices.Contains(contentHosts, host) {
		return nil
	}
	return fmt.Errorf("content_uri host %q is neither peer %q nor one of its content_hosts", host, peerID)
}

// CreatorIdentityHostAllowed applies the same provenance rule to
// creator_identity, the WebFinger handle a post is attributed to: in
// "@user@host" (the leading "@" optional) the host must be the peer id or
// one of its content_hosts. Checking content_uri alone left the attribution
// free: a bridge could anchor the peer's own post as "@anyone@elsewhere".
// A port on the host is ignored, like the URL rule.
func CreatorIdentityHostAllowed(peerID string, contentHosts []string, identity string) error {
	handle := strings.TrimPrefix(identity, "@")
	at := strings.LastIndex(handle, "@")
	if at <= 0 || at == len(handle)-1 {
		return fmt.Errorf("creator_identity %q is not @user@host", identity)
	}
	host := strings.ToLower(handle[at+1:])
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	if host == peerID || slices.Contains(contentHosts, host) {
		return nil
	}
	return fmt.Errorf("creator_identity host %q is neither peer %q nor one of its content_hosts", host, peerID)
}

// ValidateContentHosts checks a PeerPolicy.content_hosts list: at most
// MaxContentHosts entries, each a lowercase hostname under the same
// grammar as a peer id, and no duplicates.
func ValidateContentHosts(hosts []string) error {
	if len(hosts) > MaxContentHosts {
		return fmt.Errorf("content_hosts has %d entries, max %d", len(hosts), MaxContentHosts)
	}
	seen := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		if !ValidatePeerID(h) {
			return fmt.Errorf("content_hosts entry %q is not a lowercase hostname", h)
		}
		if seen[h] {
			return fmt.Errorf("content_hosts entry %q is duplicated", h)
		}
		seen[h] = true
	}
	return nil
}
