package apcanon

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// AcceptAS2 is the media type both the bridge and the verifier request
// when fetching an object at its canonical URI. The REST Status and the
// AS2 object share neither field names nor values
// (id→uri, attributedTo→account.uri, summary→spoiler_text,
// published→created_at, updated→edited_at, inReplyTo→in_reply_to_id);
// only the AS2 representation is reachable by a party without a follow
// relationship and an app token, so it is the only one hashed.
const AcceptAS2 = "application/activity+json"

// maxAS2Body caps a fetched object. A body at the cap is an ERROR, not a
// truncation: silently hashing the first N bytes of an oversized document
// would make two fetchers with different caps disagree, and on-chain a
// canonicalizer disagreement is indistinguishable from verifier
// misconduct.
const maxAS2Body = 10 << 20

// maxRedirects bounds redirect following. Go's default is 10; AS2 objects
// legitimately redirect at most once or twice (permalink → canonical id,
// http → https).
const maxRedirects = 3

// FetchOptions configures Fetch.
type FetchOptions struct {
	// Signer signs the request (draft-cavage HTTP Signatures, which is
	// what Mastodon's AUTHORIZED_FETCH mode verifies). nil fetches
	// anonymously — correct for instances without secure mode, and
	// preferred for the verifier whenever possible: independence of
	// network vantage point is the anti-fraud property, and a signature
	// changes nothing about the bytes returned.
	Signer *HTTPSigner
	// Timeout bounds the whole request. Default 15s.
	Timeout time.Duration
	// UserAgent identifies the fetcher. Default names the
	// ap-canonical-v1 tooling so instance admins can see what hits them.
	UserAgent string
	// Client overrides the http.Client (tests).
	Client *http.Client
	// MediaMinRate (bytes/second) extends a media download's deadline by
	// the file's declared size (FetchMediaDigest). Default
	// DefaultMediaMinRate. AS2 fetches ignore it.
	MediaMinRate int64
	// AllowPrivateHosts disables the private/loopback/link-local
	// destination guard. Tests set it (httptest binds 127.0.0.1);
	// production must not.
	AllowPrivateHosts bool
}

// Fetch retrieves an AS2 object at uri and returns it parsed, plus the
// raw bytes for diagnostics. The raw bytes are NEVER hashed (rule 5) —
// instances re-serialize JSON-LD differently across versions and even
// across fetches; Canonicalize the parsed object instead.
//
// Both daemons fetch URIs that ultimately come from inbound federation
// traffic, so the destination is attacker-influenced. Fetch therefore
// constrains where it will go: https only, no private/loopback/link-local
// hosts, and a short redirect chain whose every hop is re-checked. A
// signed request additionally drops its signature headers on a cross-host
// redirect rather than presenting a signature made for a different target
// (Go re-sends custom headers across hosts; it only strips Authorization).
func Fetch(ctx context.Context, uri string, opts FetchOptions) (map[string]any, []byte, error) {
	if err := checkFetchTarget(uri, opts.AllowPrivateHosts); err != nil {
		return nil, nil, err
	}

	guarded := guardedClient(opts, uri, 15*time.Second)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("apcanon: build request: %w", err)
	}
	req.Header.Set("Accept", AcceptAS2)
	ua := opts.UserAgent
	if ua == "" {
		ua = "sparkdream-apcanon/" + HashRuleName
	}
	req.Header.Set("User-Agent", ua)
	if opts.Signer != nil {
		if err := opts.Signer.Sign(req); err != nil {
			return nil, nil, err
		}
	}

	resp, err := guarded.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("apcanon: fetch %s: %w", uri, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("apcanon: fetch %s: status %d (wanted 200; 401 means the instance requires signed fetches — configure a signer)", uri, resp.StatusCode)
	}
	// Read one byte past the cap so hitting it is detectable.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAS2Body+1))
	if err != nil {
		return nil, nil, fmt.Errorf("apcanon: read body of %s: %w", uri, err)
	}
	if len(raw) > maxAS2Body {
		return nil, nil, fmt.Errorf("apcanon: %s: object exceeds %d bytes; refusing to hash a truncated document", uri, maxAS2Body)
	}
	obj, err := Parse(raw)
	if err != nil {
		return nil, raw, fmt.Errorf("apcanon: %s: %w", uri, err)
	}
	return obj, raw, nil
}

// guardedClient returns an http.Client whose every redirect hop is
// re-checked against the destination guard, bounded to maxRedirects, and
// stripped of signature headers when it leaves the original host. Shared
// by AS2 fetches and media fetches, which reach equally
// attacker-influenced URLs.
func guardedClient(opts FetchOptions, origin string, defaultTimeout time.Duration) *http.Client {
	client := opts.Client
	if client == nil {
		timeout := opts.Timeout
		if timeout == 0 {
			timeout = defaultTimeout
		}
		client = &http.Client{Timeout: timeout}
	}
	// Install the redirect policy on a shallow copy so an injected test
	// client keeps its transport without inheriting a shared mutation.
	guarded := *client
	guarded.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("apcanon: too many redirects (>%d) fetching %s", maxRedirects, origin)
		}
		if err := checkFetchTarget(req.URL.String(), opts.AllowPrivateHosts); err != nil {
			return err
		}
		// A cavage signature covers (request-target), host and date; it is
		// only valid for the hop it was made for. Carrying it to another
		// host leaks the keyId and a valid signature to whatever the
		// redirector named, and would not authenticate anyway.
		if req.URL.Host != via[0].URL.Host {
			req.Header.Del("Signature")
			req.Header.Del("Digest")
			req.Header.Del("Date")
		}
		return nil
	}
	return &guarded
}

// checkFetchTarget rejects destinations a federation fetcher has no
// business reaching: non-https schemes, and hosts that resolve into
// loopback, link-local (169.254.0.0/16 — cloud metadata), or RFC1918
// space. Without it a remote instance could redirect the daemon into its
// own host's internal network.
func checkFetchTarget(rawURL string, allowPrivate bool) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("apcanon: unparseable fetch target %q: %w", rawURL, err)
	}
	if u.Scheme != "https" && !(allowPrivate && u.Scheme == "http") {
		return fmt.Errorf("apcanon: refusing to fetch %s: scheme must be https", rawURL)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("apcanon: refusing to fetch %s: no host", rawURL)
	}
	if allowPrivate {
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("apcanon: cannot resolve %s: %w", host, err)
	}
	for _, ip := range ips {
		if isDisallowedIP(ip) {
			return fmt.Errorf("apcanon: refusing to fetch %s: %s resolves to non-public address %s",
				rawURL, host, ip)
		}
	}
	return nil
}

func isDisallowedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
}
