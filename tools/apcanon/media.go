package apcanon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gowebpki/jcs"
)

// HashRuleV2 is ap-canonical-v1 plus the media itself: every attachment
// element also carries the digest of the file its url points at. v1 binds
// an attachment's type and alt text but not its bytes, so a file swapped
// behind the same URL — by the instance, whoever controls its storage, or
// an attacker in either — changed nothing a verifier could see. Under v2 it
// changes the hash.
//
// v1 stays valid: a record says which rule it was hashed under in
// protocol_metadata.hash_rule, and the verifier recomputes with that rule.
const HashRuleV2 = "ap-canonical-v2"

// MaxMediaBytes is PART OF THE v2 RULE, not a tuning knob: two fetchers
// with different caps would disagree about a file between them, and a
// disagreement is indistinguishable from verifier misconduct on-chain. It
// sits above Mastodon's largest upload (99 MB video), so every Mastodon
// attachment is hashed in full. A file past it digests as DigestOversize.
// Changing it is a new rule version.
const MaxMediaBytes = 128 << 20

// DigestOversize is the digest of a file larger than MaxMediaBytes.
const DigestOversize = "oversize"

// MediaDigester returns the v2 digest of the file at url: "sha256:<hex>"
// or DigestOversize. An error means the file could not be read (both
// daemons retry; neither hashes a guess).
type MediaDigester func(ctx context.Context, url string) (string, error)

// AttachmentURL reads an AS2 attachment's url: a plain string (Mastodon),
// a Link object, or an array of either (the first usable href). "" when
// there is none.
func AttachmentURL(v any) string {
	switch u := v.(type) {
	case string:
		return u
	case map[string]any:
		if href, ok := u["href"].(string); ok {
			return href
		}
	case []any:
		for _, item := range u {
			if href := AttachmentURL(item); href != "" {
				return href
			}
		}
	}
	return ""
}

// mediaURL is the attachment url v2 digests: http(s) only. Anything else
// digests as null, deterministically on both sides.
func mediaURL(elem any) string {
	obj, ok := elem.(map[string]any)
	if !ok {
		return ""
	}
	u := AttachmentURL(obj["url"])
	if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
		return u
	}
	return ""
}

// CanonicalizeV2 is Canonicalize with each attachment element extended by
// "digest": the digester's value for its http(s) url, or null when it has
// none. Everything else, including v1's totality guarantees, is unchanged.
func CanonicalizeV2(ctx context.Context, obj map[string]any, digest MediaDigester) ([]byte, error) {
	projected := map[string]any{
		"id":           valueOrNull(obj, "id"),
		"attributedTo": valueOrNull(obj, "attributedTo"),
		"content":      valueOrNull(obj, "content"),
		"summary":      valueOrNull(obj, "summary"),
		"published":    valueOrNull(obj, "published"),
		"updated":      valueOrNull(obj, "updated"),
		"inReplyTo":    valueOrNull(obj, "inReplyTo"),
	}
	att, err := projectAttachmentV2(ctx, obj["attachment"], digest)
	if err != nil {
		return nil, err
	}
	projected["attachment"] = att

	intermediate, err := json.Marshal(projected)
	if err != nil {
		return nil, fmt.Errorf("apcanon: projection marshal failed: %w", err)
	}
	canonical, err := jcs.Transform(intermediate)
	if err != nil {
		return nil, fmt.Errorf("apcanon: JCS transform failed: %w", err)
	}
	return canonical, nil
}

// projectAttachmentV2 mirrors projectAttachment, adding each element's
// media digest.
func projectAttachmentV2(ctx context.Context, v any, digest MediaDigester) (any, error) {
	var elems []any
	switch typed := v.(type) {
	case nil:
		return nil, nil
	case []any:
		elems = typed
	case map[string]any:
		elems = []any{typed}
	default:
		elems = []any{typed}
	}
	out := make([]any, 0, len(elems))
	for _, elem := range elems {
		projected := map[string]any{"mediaType": nil, "name": nil, "digest": nil}
		if obj, ok := elem.(map[string]any); ok {
			projected["mediaType"] = valueOrNull(obj, "mediaType")
			projected["name"] = valueOrNull(obj, "name")
		}
		if u := mediaURL(elem); u != "" {
			if digest == nil {
				return nil, fmt.Errorf("apcanon: %s needs a media digester for %s", HashRuleV2, u)
			}
			d, err := digest(ctx, u)
			if err != nil {
				return nil, fmt.Errorf("apcanon: media %s: %w", u, err)
			}
			projected["digest"] = d
		}
		out = append(out, projected)
	}
	return out, nil
}

// HashV2 is the ap-canonical-v2 digest of an AS2 object.
func HashV2(ctx context.Context, obj map[string]any, digest MediaDigester) ([32]byte, error) {
	var zero [32]byte
	canonical, err := CanonicalizeV2(ctx, obj, digest)
	if err != nil {
		return zero, err
	}
	return sha256.Sum256(canonical), nil
}

// HashFor hashes obj under the named rule. An empty rule is v1, the rule
// every record predating hash_rule was anchored under.
func HashFor(ctx context.Context, rule string, obj map[string]any, digest MediaDigester) ([32]byte, error) {
	switch rule {
	case "", HashRuleName:
		return Hash(obj)
	case HashRuleV2:
		return HashV2(ctx, obj, digest)
	}
	var zero [32]byte
	return zero, fmt.Errorf("apcanon: unknown hash rule %q", rule)
}

// ErrMediaUnavailable wraps every failure to read a file for v2: a
// status other than 200, a network error, or running out of time. Callers
// tell it apart from an unfetchable post, because the remedy differs: a
// large file on a slow link needs a longer timeout, not a human.
var ErrMediaUnavailable = errors.New("media file unavailable")

// Media download time budget. Neither value is part of the rule: running
// out of time is an error (retried), never a digest, so fetchers with
// different budgets still never disagree about a hash.
const (
	// DefaultMediaTimeout is the base budget: the response headers must
	// arrive within it, and it is the floor for the whole download.
	DefaultMediaTimeout = 2 * time.Minute
	// DefaultMediaMinRate is the slowest transfer, in bytes per second, the
	// budget allows for: a file's declared size extends the deadline by
	// size / rate. 512 KiB/s gives a 99 MB video about five minutes.
	DefaultMediaMinRate = 512 << 10
)

// mediaBudget is the whole-download deadline for a file of size bytes
// (MaxMediaBytes when the server does not declare it).
func mediaBudget(base time.Duration, rate, size int64) time.Duration {
	if size < 0 || size > MaxMediaBytes {
		size = MaxMediaBytes
	}
	return base + time.Duration(size/rate)*time.Second
}

// FetchMediaDigest streams the file at url through sha256 and returns its
// v2 digest. Same destination guard and redirect policy as Fetch, since
// attachment URLs are as attacker-influenced as object ids. The request
// pins Accept-Encoding: identity so no transparent decompression or CDN
// re-encoding can make two fetchers hash different bytes.
//
// Time: opts.Timeout (default DefaultMediaTimeout) for the headers, then
// extended by the declared size at opts.MediaMinRate, so a large file on
// a modest link finishes instead of failing at a fixed two minutes.
func FetchMediaDigest(ctx context.Context, url string, opts FetchOptions) (string, error) {
	if err := checkFetchTarget(url, opts.AllowPrivateHosts); err != nil {
		return "", err
	}
	base := opts.Timeout
	if base <= 0 {
		base = DefaultMediaTimeout
	}
	rate := opts.MediaMinRate
	if rate <= 0 {
		rate = DefaultMediaMinRate
	}
	// The deadline moves once the size is known, which a fixed
	// http.Client.Timeout cannot do: drive it with a timer instead.
	noTimeout := opts
	noTimeout.Timeout = 0
	client := guardedClient(noTimeout, url, 0)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var timedOut atomic.Bool
	budget := base
	timer := time.AfterFunc(budget, func() { timedOut.Store(true); cancel() })
	defer timer.Stop()
	fail := func(err error) (string, error) {
		if timedOut.Load() {
			return "", fmt.Errorf("%w: %s: timed out after %s", ErrMediaUnavailable, url, budget)
		}
		return "", fmt.Errorf("%w: %s: %v", ErrMediaUnavailable, url, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("apcanon: build media request: %w", err)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")
	ua := opts.UserAgent
	if ua == "" {
		ua = "sparkdream-apcanon/" + HashRuleV2
	}
	req.Header.Set("User-Agent", ua)

	resp, err := client.Do(req)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fail(fmt.Errorf("status %d", resp.StatusCode))
	}
	if resp.ContentLength > MaxMediaBytes {
		return DigestOversize, nil
	}
	budget = mediaBudget(base, rate, resp.ContentLength)
	timer.Reset(budget)

	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(resp.Body, MaxMediaBytes+1))
	if err != nil {
		return fail(err)
	}
	if n > MaxMediaBytes {
		return DigestOversize, nil
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
