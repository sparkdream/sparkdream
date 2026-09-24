package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"sparkdream/tools/apcanon"
)

const mediaNote = `{
  "id":"https://md.test/users/alice/statuses/60",
  "type":"Note",
  "attributedTo":"https://md.test/users/alice",
  "content":"<p>a picture</p>",
  "summary":"cw",
  "published":"2026-09-12T10:00:00Z",
  "url":"https://md.test/@alice/60",
  "to":["https://www.w3.org/ns/activitystreams#Public"],
  "attachment":[{"type":"Document","mediaType":"image/png","name":"orange square",
    "url":"https://md.test/system/1.png","blurhash":"U1M_","width":16,"height":16}]
}`

func b64(v any) string {
	raw, _ := json.Marshal(v)
	return base64.StdEncoding.EncodeToString(raw)
}

// faithful is a record that shows exactly mediaNote.
func faithful() PendingContent {
	return PendingContent{
		ID: "9", ContentURI: "https://md.test/users/alice/statuses/60", PeerID: "md.test",
		Body: "<p>a picture</p>", Title: "cw", ContentType: "blog_post", RemoteCreatedAt: "1789207200",
		CreatorIdentity: "@alice@md.test",
		ProtocolMetadataB64: b64(map[string]any{
			"hash_rule": apcanon.HashRuleV2, "object_id": "https://md.test/users/alice/statuses/60",
			"actor": "https://md.test/users/alice", "in_reply_to": nil, "updated": nil,
			"content_url": "https://md.test/@alice/60",
			"attachments": []any{map[string]any{"url": "https://md.test/system/1.png", "media_type": "image/png",
				"name": "orange square", "blurhash": "U1M_", "width": 16, "height": 16, "digest": "sha256:aa"}},
		}),
	}
}

func withMeta(c PendingContent, edit func(m map[string]any)) PendingContent {
	m, _ := c.recordMeta()
	edit(m)
	c.ProtocolMetadataB64 = b64(m)
	return c
}

func TestCheckDisplay(t *testing.T) {
	obj, err := apcanon.Parse([]byte(mediaNote))
	if err != nil {
		t.Fatal(err)
	}
	digests := map[string]string{"https://md.test/system/1.png": "sha256:aa"}
	if bad := checkDisplay(faithful(), apcanon.HashRuleV2, obj, digests, 4096); len(bad) != 0 {
		t.Fatalf("a faithful record was flagged: %v", bad)
	}

	att := func(edit func(a map[string]any)) func(m map[string]any) {
		return func(m map[string]any) { edit(m["attachments"].([]any)[0].(map[string]any)) }
	}
	cases := []struct {
		name string
		rec  PendingContent
	}{
		{"rewritten body", func() PendingContent { c := faithful(); c.Body = "<p>something else</p>"; return c }()},
		{"empty body", func() PendingContent { c := faithful(); c.Body = ""; return c }()},
		{"body cut before the limit", func() PendingContent { c := faithful(); c.Body = "<p>a pic"; return c }()},
		{"made-up title", func() PendingContent { c := faithful(); c.Title = "breaking news"; return c }()},
		{"reply type on a non-reply", func() PendingContent { c := faithful(); c.ContentType = "blog_reply"; return c }()},
		{"wrong created time", func() PendingContent { c := faithful(); c.RemoteCreatedAt = "1"; return c }()},
		{"object_id elsewhere", withMeta(faithful(), func(m map[string]any) { m["object_id"] = "https://md.test/users/alice/statuses/61" })},
		{"actor swapped", withMeta(faithful(), func(m map[string]any) { m["actor"] = "https://md.test/users/bob" })},
		{"permalink swapped", withMeta(faithful(), func(m map[string]any) { m["content_url"] = "https://evil.test/x" })},
		{"attachment link to the operator's server", withMeta(faithful(), att(func(a map[string]any) { a["url"] = "https://evil.test/1.png" }))},
		{"alt text rewritten", withMeta(faithful(), att(func(a map[string]any) { a["name"] = "nothing to see" }))},
		{"blurhash swapped", withMeta(faithful(), att(func(a map[string]any) { a["blurhash"] = "LEHV6n" }))},
		{"digest of another file", withMeta(faithful(), att(func(a map[string]any) { a["digest"] = "sha256:bb" }))},
		{"invented extra attachment", withMeta(faithful(), func(m map[string]any) {
			m["attachments"] = append(m["attachments"].([]any), map[string]any{"url": "https://evil.test/2.png"})
		})},
		{"attachments hidden without the omitted count", withMeta(faithful(), func(m map[string]any) { m["attachments"] = []any{} })},
	}
	for _, c := range cases {
		if bad := checkDisplay(c.rec, apcanon.HashRuleV2, obj, digests, 4096); len(bad) == 0 {
			t.Errorf("%s: not flagged", c.name)
		}
	}

	// Legitimate variations pass: attachments counted as omitted, and a
	// body cut exactly at the chain's limit.
	omitted := withMeta(faithful(), func(m map[string]any) { m["attachments"] = []any{}; m["attachments_omitted"] = 1 })
	if bad := checkDisplay(omitted, apcanon.HashRuleV2, obj, digests, 4096); len(bad) != 0 {
		t.Errorf("omitted attachments flagged: %v", bad)
	}
	cut := faithful()
	cut.Body = "<p>a pic"
	if bad := checkDisplay(cut, apcanon.HashRuleV2, obj, digests, len(cut.Body)); len(bad) != 0 {
		t.Errorf("a body cut at the chain limit flagged: %v", bad)
	}
}

// A handle on the right host can still name the wrong person.
func TestCheckAuthorUsername(t *testing.T) {
	obj, _ := apcanon.Parse([]byte(mediaNote))
	fetches := 0
	v := &Verifier{fetch: func(ctx context.Context, uri string) (map[string]any, error) {
		fetches++
		return map[string]any{"preferredUsername": "alice"}, nil
	}}
	rec := faithful()
	if p, err := v.checkAuthor(t.Context(), rec, obj); err != nil || p != "" {
		t.Fatalf("the real author was flagged: %q %v", p, err)
	}
	rec.CreatorIdentity = "@bob@md.test"
	if p, _ := v.checkAuthor(t.Context(), rec, obj); p == "" {
		t.Fatal("another user's handle must be flagged")
	}
	if fetches != 1 {
		t.Fatalf("author fetched %d times, want once (cached)", fetches)
	}
}

// End to end: a record whose hash matches but which displays something
// else is alarmed on, not verified. And a v2 record verifies only when
// the files hash to what was anchored.
func TestVerifyRefusesMisrepresentedAndChecksMedia(t *testing.T) {
	obj, _ := apcanon.Parse([]byte(mediaNote))
	digest := "sha256:aa"
	v2hash, _ := apcanon.HashV2(t.Context(), obj, func(context.Context, string) (string, error) { return digest, nil })

	run := func(rec PendingContent, fileDigest string) *harness {
		h := newHarness(t, base64.StdEncoding.EncodeToString(v2hash[:]))
		rec.ContentHashB64 = base64.StdEncoding.EncodeToString(v2hash[:])
		h.v.list = func(context.Context, string) ([]PendingContent, error) { return []PendingContent{rec}, nil }
		h.v.fetch = func(_ context.Context, uri string) (map[string]any, error) {
			if uri == "https://md.test/users/alice" {
				return map[string]any{"preferredUsername": "alice"}, nil
			}
			return apcanon.Parse([]byte(mediaNote))
		}
		h.v.digest = func(context.Context, string) (string, error) { return fileDigest, nil }
		if err := h.v.poll(t.Context()); err != nil {
			t.Fatal(err)
		}
		return h
	}

	if h := run(faithful(), digest); len(h.submitted) != 1 || len(h.alarms) != 0 {
		t.Fatalf("faithful v2 record: %d submissions, alarms %q", len(h.submitted), h.alarms)
	}
	lie := faithful()
	lie.Body = "<p>something else</p>"
	if h := run(lie, digest); len(h.submitted) != 0 || len(h.alarms) != 1 || !strings.Contains(h.alarms[0], "MISREPRESENTED") {
		t.Fatalf("misrepresented record: %d submissions, alarms %q", len(h.submitted), h.alarms)
	}
	// The file behind the same URL was swapped: v2 no longer matches.
	if h := run(faithful(), "sha256:swapped"); len(h.submitted) != 0 || len(h.alarms) != 1 || !strings.Contains(h.alarms[0], "HASH MISMATCH") {
		t.Fatalf("swapped file: %d submissions, alarms %q", len(h.submitted), h.alarms)
	}
}

// Under v2 a record links only files it attests. An oversize file is
// described (type, alt text, "oversize") but a link to it is refused.
func TestV2RecordMayNotLinkAnUnattestedFile(t *testing.T) {
	obj, _ := apcanon.Parse([]byte(mediaNote))
	oversize := map[string]string{"https://md.test/system/1.png": apcanon.DigestOversize}

	linked := withMeta(faithful(), func(m map[string]any) {
		m["attachments"].([]any)[0].(map[string]any)["digest"] = apcanon.DigestOversize
	})
	if bad := checkDisplay(linked, apcanon.HashRuleV2, obj, oversize, 4096); len(bad) == 0 {
		t.Fatal("a link to an oversize (unattested) file must be flagged")
	}
	described := withMeta(linked, func(m map[string]any) {
		delete(m["attachments"].([]any)[0].(map[string]any), "url")
	})
	if bad := checkDisplay(described, apcanon.HashRuleV2, obj, oversize, 4096); len(bad) != 0 {
		t.Fatalf("an oversize file described without a link must pass: %v", bad)
	}
	// A link with no digest at all is refused too.
	noDigest := withMeta(faithful(), func(m map[string]any) {
		delete(m["attachments"].([]any)[0].(map[string]any), "digest")
	})
	if bad := checkDisplay(noDigest, apcanon.HashRuleV2, obj, map[string]string{"https://md.test/system/1.png": "sha256:aa"}, 4096); len(bad) == 0 {
		t.Fatal("a v2 link without its digest must be flagged")
	}
	// v1 records predate the rule: their links are left as they were.
	if bad := checkDisplay(noDigest, apcanon.HashRuleName, obj, nil, 4096); len(bad) != 0 {
		t.Fatalf("a v1 record's link was flagged: %v", bad)
	}
}

// A file that keeps failing to download gets its own alarm, naming the
// likely remedy, rather than the generic unfetchable-post one.
func TestPersistentMediaFailureRaisesMediaAlarm(t *testing.T) {
	obj, _ := apcanon.Parse([]byte(mediaNote))
	v2hash, _ := apcanon.HashV2(t.Context(), obj, func(context.Context, string) (string, error) { return "sha256:aa", nil })
	h := newHarness(t, base64.StdEncoding.EncodeToString(v2hash[:]))
	rec := faithful()
	rec.ContentHashB64 = base64.StdEncoding.EncodeToString(v2hash[:])
	h.v.list = func(context.Context, string) ([]PendingContent, error) { return []PendingContent{rec}, nil }
	h.v.fetch = func(context.Context, string) (map[string]any, error) { return apcanon.Parse([]byte(mediaNote)) }
	h.v.digest = func(context.Context, string) (string, error) {
		return "", fmt.Errorf("%w: https://md.test/system/1.png: timed out after 2m0s", apcanon.ErrMediaUnavailable)
	}
	for i := 0; i < defaultFetchFailureAlarmAt; i++ {
		if err := h.v.poll(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.submitted) != 0 || len(h.alarms) != 1 || !strings.Contains(h.alarms[0], "MEDIA UNFETCHABLE") ||
		!strings.Contains(h.alarms[0], "SDA_MEDIA_TIMEOUT") {
		t.Fatalf("want one MEDIA UNFETCHABLE alarm naming the remedy, got %d submissions, alarms %q", len(h.submitted), h.alarms)
	}
}
