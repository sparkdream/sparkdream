// Package apcanon implements the ap-canonical-v1 canonicalization rule:
// the deterministic projection of an ActivityPub (AS2) object that both
// the inbound bridge daemon (cmd/sdapbridge) and the independent
// verifier runner (cmd/sdapverify) hash before one anchors it on-chain
// and the other re-computes it for MsgVerifyContent.
//
// The rule (also specified in docs/x-federation-spec.md, SubmitFederatedContent):
//
//	sha256(jcs({
//	  id, attributedTo, content, summary, published, updated,
//	  inReplyTo, attachment: [{mediaType, name}]
//	}))
//
// Five binding decisions, each of which a second implementation would
// otherwise get wrong:
//
//  1. RFC 8785 (JCS) via github.com/gowebpki/jcs — not a hand-rolled
//     "sorted keys, no whitespace". Number formatting (ES6 double
//     serialization), escaping of control characters, and code-point
//     ordering of member names are exactly where independent
//     implementations diverge. Note what JCS does NOT do: it performs no
//     Unicode normalization, so NFC and NFD spellings of the same text
//     hash differently. That is correct for this rule — the chain anchors
//     what the instance actually published, byte for byte — but it means
//     an instance that re-normalizes text on edit will move the hash, and
//     `updated` should be populated when it does.
//  2. Exactly eight top-level keys, always emitted. Absent source fields
//     serialize as JSON null, never omitted. An implementation that
//     omits and one that nulls produce different hashes for the same
//     unedited status — and a mismatch is indistinguishable from
//     operator fraud on-chain, so both sides MUST share this package
//     rather than reimplement the rule.
//  3. No attachment[].url. Signed object-storage URLs are the least
//     stable field in the object and would produce spurious mismatches;
//     `updated` already covers every edit Mastodon can make, media
//     included. `url` stays in protocol_metadata, informational.
//  4. attachment preserves source order and is null when absent, [] when
//     present and empty — those are distinct.
//  5. Never hash the raw fetched bytes. Instances re-serialize JSON-LD
//     differently across versions and even across fetches. Parse, then
//     canonicalize.
package apcanon

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/gowebpki/jcs"
)

// HashRuleName is the version string recorded alongside anchored content
// (protocol_metadata.hash_rule). Bump when the projection changes; the
// chain never interprets it, but operators need it to explain a
// cross-version mismatch.
const HashRuleName = "ap-canonical-v1"

// Canonicalize projects an AS2 object onto the eight hashed fields and
// returns their RFC 8785 canonical serialization. The input must be a
// decoded JSON object (map[string]any — use Parse, which preserves
// number literals via json.Number); any other top-level type is an
// error, because hashing it would be a rule change by accident.
//
// The projection is marshaled to intermediate JSON and then run through
// jcs.Transform, which re-parses and re-serializes numbers per RFC 8785's
// ES6 formatting rules — the step a hand-rolled canonicalizer gets wrong.
func Canonicalize(obj map[string]any) ([]byte, error) {
	projected := map[string]any{
		"id":           valueOrNull(obj, "id"),
		"attributedTo": valueOrNull(obj, "attributedTo"),
		"content":      valueOrNull(obj, "content"),
		"summary":      valueOrNull(obj, "summary"),
		"published":    valueOrNull(obj, "published"),
		"updated":      valueOrNull(obj, "updated"),
		"inReplyTo":    valueOrNull(obj, "inReplyTo"),
		"attachment":   projectAttachment(obj["attachment"]),
	}
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

// Hash returns the ap-canonical-v1 digest of an AS2 object: the sha256
// of its canonical form. This is the value submitted as
// MsgSubmitFederatedContent.content_hash and re-computed by
// MsgVerifyContent.
func Hash(obj map[string]any) ([32]byte, error) {
	var zero [32]byte
	canonical, err := Canonicalize(obj)
	if err != nil {
		return zero, err
	}
	return sha256.Sum256(canonical), nil
}

// Parse decodes fetched AS2 bytes, keeping numbers as json.Number so the
// literal text survives into the JCS pass rather than being rewritten by
// Go's own float64 formatting before jcs.Transform ever sees it.
//
// Note what this does NOT buy: RFC 8785 serializes numbers with
// ECMAScript double semantics, so an integer beyond 2^53 is rounded by
// the canonicalizer regardless (2^53+1 canonicalizes as 2^53). That is
// deterministic — both daemons run this package, so they round the same
// way — and is pinned by TestLargeIntegersRoundDeterministically. It is
// documented rather than rejected because Mastodon puts no such integers
// inside the eight hashed keys.
//
// The raw bytes are never hashed (rule 5).
func Parse(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, fmt.Errorf("apcanon: not a JSON object: %w", err)
	}
	return obj, nil
}

// valueOrNull returns the source field verbatim — whatever its JSON
// type — or nil when absent. Values are never coerced: the rule hashes
// what the instance published.
func valueOrNull(obj map[string]any, key string) any {
	v, ok := obj[key]
	if !ok {
		return nil
	}
	return v
}

// projectAttachment maps the source attachment onto [{mediaType, name}].
// null when absent, [] when present and empty; source order preserved
// (an attachment reorder IS a content change under this rule, which is
// acceptable because Mastodon never reorders an unedited status).
//
// Elements that are not objects project to both-null rather than
// erroring: the rule must be TOTAL — one side erroring where the other
// hashes would look exactly like verifier misconduct on-chain. The cost
// is that bare-string attachments (a plain URI, which AS2 permits)
// collapse to identical {null, null} projections, so swapping two such
// attachments does not move the hash. Accepted: Mastodon always emits
// Document objects with mediaType and name, and hashing attachment URIs
// is precisely what decision 3 rules out.
func projectAttachment(v any) any {
	switch typed := v.(type) {
	case nil:
		return nil
	case []any:
		out := make([]any, 0, len(typed))
		for _, elem := range typed {
			out = append(out, projectAttachmentElement(elem))
		}
		return out
	case map[string]any:
		// AS2 allows a single object where an array is expected.
		return []any{projectAttachmentElement(typed)}
	default:
		// A bare string URI or other shape: keep the element's position
		// with both fields null.
		return []any{map[string]any{"mediaType": nil, "name": nil}}
	}
}

func projectAttachmentElement(elem any) any {
	obj, ok := elem.(map[string]any)
	if !ok {
		return map[string]any{"mediaType": nil, "name": nil}
	}
	return map[string]any{
		"mediaType": valueOrNull(obj, "mediaType"),
		"name":      valueOrNull(obj, "name"),
	}
}
