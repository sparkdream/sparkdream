package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"sparkdream/tools/apcanon"
)

// A real Mastodon v4.7.2 AS2 object with one image, captured by the P2
// determinism run.
const mediaFixture = "../../test/federation/mastodon/accepted/p2-mastodon-v4.7.2-20260923/baseline/fixtures/" +
	"http___localhost_3000_ap_users_117318268488994076_statuses_117320681131588860.as2.json"

// readMediaFixture loads the recorded status with a #cc0 dedication added:
// the bridge anchors only public-domain posts. The recorded file stays as
// captured, since its hash is pinned by the determinism harness.
func readMediaFixture() ([]byte, error) {
	raw, err := os.ReadFile(mediaFixture)
	return []byte(strings.Replace(string(raw), "status with a media attachment", "status with a media attachment #cc0", 1)), err
}

func TestAttachmentMetaFromRealMastodonObject(t *testing.T) {
	raw, err := readMediaFixture()
	if err != nil {
		t.Fatal(err)
	}
	obj, err := apcanon.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	fileURL := "http://localhost:3000/system/media_attachments/files/117/320/680/921/542/442/original/1f4ed1090ea8ebfb.png"
	got := attachmentMeta(obj, map[string]string{fileURL: "sha256:0f"})
	if len(got) != 1 {
		t.Fatalf("want 1 attachment, got %d", len(got))
	}
	a := got[0]
	if a["url"] != fileURL || a["digest"] != "sha256:0f" ||
		a["media_type"] != "image/png" || a["name"] != "orange square" ||
		a["width"] != int64(16) || a["height"] != int64(16) || a["blurhash"] == nil {
		t.Fatalf("attachment = %v", a)
	}
}

// Other ActivityPub servers publish url as a Link object or an array; and
// only http(s) links are kept, since clients render them.
func TestAttachmentURLShapes(t *testing.T) {
	obj := map[string]any{"attachment": []any{
		map[string]any{"mediaType": "image/jpeg", "url": map[string]any{"type": "Link", "href": "https://a.test/1.jpg"}},
		map[string]any{"mediaType": "video/mp4", "url": []any{map[string]any{"href": "https://a.test/2.mp4"}}},
		map[string]any{"mediaType": "image/png", "url": "javascript:alert(1)"},
		map[string]any{"mediaType": "image/png"},
		"not an object",
	}}
	hashed := map[string]string{"https://a.test/1.jpg": "sha256:01", "https://a.test/2.mp4": "sha256:02"}
	got := attachmentMeta(obj, hashed)
	if len(got) != 2 || got[0]["url"] != "https://a.test/1.jpg" || got[1]["url"] != "https://a.test/2.mp4" {
		t.Fatalf("got %v", got)
	}
	// A lone attachment object (not wrapped in an array) is valid AS2 too.
	single := map[string]any{"attachment": map[string]any{"url": "https://a.test/3.png"}}
	if got := attachmentMeta(single, nil); len(got) != 1 {
		t.Fatalf("single attachment object: got %v", got)
	}
}

// The chain truncates protocol_metadata past its limit, and truncated JSON
// is unreadable: what does not fit is counted, never cut.
func TestFitAttachmentsStaysValidAndUnderBudget(t *testing.T) {
	var atts []map[string]any
	for i := 0; i < 4; i++ {
		atts = append(atts, map[string]any{
			"url": "https://md.test/system/media/" + strings.Repeat("x", 40), "media_type": "image/png",
			"name": strings.Repeat("long alt text ", 100), // ~1400 bytes, near Mastodon's 1500 cap
		})
	}
	meta := map[string]any{"hash_rule": apcanon.HashRuleName, "object_id": "https://md.test/users/a/statuses/1"}
	budget := 3000
	out := fitAttachments(meta, atts, budget)

	if len(out) > budget {
		t.Fatalf("metadata is %d bytes, over the %d budget", len(out), budget)
	}
	var decoded struct {
		Attachments []map[string]any `json:"attachments"`
		Omitted     int              `json:"attachments_omitted"`
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("metadata is not valid JSON: %v", err)
	}
	if len(decoded.Attachments) == 0 || len(decoded.Attachments)+decoded.Omitted != 4 {
		t.Fatalf("kept %d + omitted %d, want 4 in total with at least one kept", len(decoded.Attachments), decoded.Omitted)
	}
	// The supersedes link appended later still fits in the reserve.
	if withLink := appendProtocolSupersedes(out, 18446744073709551615); len(withLink) > budget+metadataReserve {
		t.Fatalf("supersedes link overflows the reserve: %d bytes", len(withLink))
	}
}

// End to end: an anchored post carries its media list in protocol_metadata.
func TestAnchoredPostCarriesAttachments(t *testing.T) {
	raw, err := readMediaFixture()
	if err != nil {
		t.Fatal(err)
	}
	uri := "https://md.test/users/alice/statuses/20"
	b, sub := testBridge(t, nil)
	b.fetcher = mapFetcher{uri: strings.Replace(string(raw),
		"http://localhost:3000/ap/users/117318268488994076/statuses/117320681131588860", uri, 1)}

	if err := b.IngestURI(t.Context(), uri, &Account{URI: "x"}); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 {
		t.Fatalf("want one submission, got %d", len(sub.msgs))
	}
	var meta struct {
		Attachments []map[string]any `json:"attachments"`
	}
	if err := json.Unmarshal(sub.msgs[0].ProtocolMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	if len(meta.Attachments) != 1 || meta.Attachments[0]["name"] != "orange square" {
		t.Fatalf("attachments in metadata = %v", meta.Attachments)
	}
}

// Under ap-canonical-v2 the anchored hash covers each file's bytes, the
// metadata names the rule, and each attachment carries its digest.
func TestV2AnchorsFileDigests(t *testing.T) {
	raw, err := readMediaFixture()
	if err != nil {
		t.Fatal(err)
	}
	uri := "https://md.test/users/alice/statuses/21"
	b, sub := testBridge(t, nil)
	b.cfg.HashRule = apcanon.HashRuleV2
	fetches := 0
	b.digest = func(context.Context, string) (string, error) { fetches++; return "sha256:ab", nil }
	body := strings.Replace(string(raw), "http://localhost:3000/ap/users/117318268488994076/statuses/117320681131588860", uri, 1)
	// Two attachments on the same file: fetched once.
	body = strings.Replace(body, `"attachment":[{`, `"attachment":[{"type":"Document","mediaType":"image/png","name":"again","url":"http://localhost:3000/system/media_attachments/files/117/320/680/921/542/442/original/1f4ed1090ea8ebfb.png"},{`, 1)
	b.fetcher = mapFetcher{uri: body}

	if err := b.IngestURI(t.Context(), uri, &Account{URI: "x"}); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 1 {
		t.Fatalf("want one submission, got %d", len(sub.msgs))
	}
	obj, _ := apcanon.Parse([]byte(body))
	want, _ := apcanon.HashV2(t.Context(), obj, func(context.Context, string) (string, error) { return "sha256:ab", nil })
	if string(sub.msgs[0].ContentHash) != string(want[:]) {
		t.Fatal("the anchored hash is not the ap-canonical-v2 hash")
	}
	var meta struct {
		HashRule    string           `json:"hash_rule"`
		Attachments []map[string]any `json:"attachments"`
	}
	if err := json.Unmarshal(sub.msgs[0].ProtocolMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.HashRule != apcanon.HashRuleV2 || len(meta.Attachments) != 2 || meta.Attachments[0]["digest"] != "sha256:ab" {
		t.Fatalf("metadata = %+v", meta)
	}
	if fetches != 1 {
		t.Fatalf("the same file was fetched %d times within one ingest", fetches)
	}
}

// A file that cannot be read fails the hash: the post is retried later,
// never anchored with a guessed digest.
func TestV2UnreadableMediaIsDeferredNotAnchored(t *testing.T) {
	raw, err := readMediaFixture()
	if err != nil {
		t.Fatal(err)
	}
	uri := "https://md.test/users/alice/statuses/22"
	b, sub := testBridge(t, nil)
	b.cfg.HashRule = apcanon.HashRuleV2
	b.digest = func(context.Context, string) (string, error) { return "", errors.New("404") }
	b.fetcher = mapFetcher{uri: strings.Replace(string(raw),
		"http://localhost:3000/ap/users/117318268488994076/statuses/117320681131588860", uri, 1)}

	st := &Status{ID: "22", URI: uri, Account: Account{URI: "x"}}
	err = b.IngestStatus(t.Context(), st)
	if err == nil || len(sub.msgs) != 0 {
		t.Fatalf("want an error and no submission, got err=%v, %d submissions", err, len(sub.msgs))
	}
	if !b.deferOrHold(st, err) || b.state.Deferred[uri] == nil {
		t.Fatal("an unreadable file must send the post to the retry queue")
	}
}

// A link is recorded only for a file whose bytes were hashed: an oversize
// file (and any file under v1, which hashes none) is described -- type,
// alt text, the "oversize" marker -- but not linked, because a client
// renders the link under a record that cannot vouch for it.
func TestUnattestedFilesAreDescribedButNotLinked(t *testing.T) {
	obj := map[string]any{"attachment": []any{
		map[string]any{"mediaType": "video/mp4", "name": "a long talk", "url": "https://a.test/talk.mp4", "width": json.Number("1920")},
		map[string]any{"mediaType": "image/png", "name": "a chart", "url": "https://a.test/chart.png"},
	}}
	got := attachmentMeta(obj, map[string]string{
		"https://a.test/talk.mp4":  apcanon.DigestOversize,
		"https://a.test/chart.png": "sha256:cc",
	})
	if len(got) != 2 {
		t.Fatalf("want both attachments described, got %d", len(got))
	}
	if _, linked := got[0]["url"]; linked || got[0]["digest"] != apcanon.DigestOversize || got[0]["name"] != "a long talk" || got[0]["width"] != int64(1920) {
		t.Fatalf("oversize file: %v; want described, marked oversize, no url", got[0])
	}
	if got[1]["url"] != "https://a.test/chart.png" {
		t.Fatalf("hashed file must keep its link: %v", got[1])
	}
	// v1 hashes no file, so nothing is linked.
	for _, a := range attachmentMeta(obj, nil) {
		if _, linked := a["url"]; linked {
			t.Fatalf("v1 record linked a file nobody hashed: %v", a)
		}
	}
}
