package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"sparkdream/tools/apcanon"
)

// MsgVerifyContent binds only the hash. The body, title, content type,
// timestamps and the whole protocol_metadata block (attachment links
// included) are the bridge operator's claims, and nothing on-chain checks
// them. A dishonest operator could submit a real post's hash with made-up
// text or links, and the record would verify and display them. So after
// the hash matches, the runner checks that the record shows what it
// fetched, and refuses to verify one that does not.

// defaultBodyLimit is the chain's default max_content_body_size.
const defaultBodyLimit = 4096

// recordMeta decodes protocol_metadata. ok is false when there is none or
// it does not decode; then there is nothing in it to misrepresent.
func (c PendingContent) recordMeta() (map[string]any, bool) {
	raw, err := base64.StdEncoding.DecodeString(c.ProtocolMetadataB64)
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	var meta map[string]any
	if json.Unmarshal(raw, &meta) != nil || meta == nil {
		return nil, false
	}
	return meta, true
}

// hashRule is the rule the record says it was hashed under; "" is v1.
func (c PendingContent) hashRule() string {
	meta, ok := c.recordMeta()
	if !ok {
		return ""
	}
	rule, _ := meta["hash_rule"].(string)
	return rule
}

// checkDisplay lists every way the record's displayed fields differ from
// the fetched object. digests holds the file digests computed while
// hashing (v2); bodyLimit is the chain's max_content_body_size.
func checkDisplay(c PendingContent, rule string, obj map[string]any, digests map[string]string, bodyLimit int) []string {
	var bad []string
	str := func(v any) string { s, _ := v.(string); return s }

	// Body: the chain stores a byte-prefix of the post's HTML content. It
	// may be cut short only at the chain's own limit, so an operator
	// cannot pick where it ends.
	content := str(obj["content"])
	switch {
	case !strings.HasPrefix(content, c.Body):
		bad = append(bad, "body is not the post's content")
	case len(c.Body) < len(content) && len(c.Body) < min(len(content), bodyLimit):
		bad = append(bad, fmt.Sprintf("body cut at %d bytes, before the chain's %d-byte limit", len(c.Body), bodyLimit))
	}
	// License: the chain admits only public-domain content, on the
	// operator's word that the post carries #cc0 or #publicdomain. The
	// hashtag sits in `content`, which the hash binds, so the post read
	// here is the one that was anchored.
	if want := apcanon.License(obj); c.License != want {
		if want == "" {
			bad = append(bad, fmt.Sprintf("license %q claimed but the post carries no #cc0 or #publicdomain hashtag", c.License))
		} else {
			bad = append(bad, fmt.Sprintf("license %q is not the post's %q", c.License, want))
		}
	}
	if c.Title != str(obj["summary"]) {
		bad = append(bad, "title is not the post's content warning")
	}
	isReply := str(obj["inReplyTo"]) != ""
	if isReply != (c.ContentType == "blog_reply") {
		bad = append(bad, fmt.Sprintf("content_type %q does not match inReplyTo", c.ContentType))
	}
	if published, err := time.Parse(time.RFC3339, str(obj["published"])); err == nil {
		if c.RemoteCreatedAt != strconv.FormatInt(published.Unix(), 10) {
			bad = append(bad, "remote_created_at is not the post's published time")
		}
	}

	meta, ok := c.recordMeta()
	if !ok {
		return bad
	}
	// Each metadata field that is present must say what the object says.
	for key, want := range map[string]any{
		"object_id":   c.ContentURI,
		"actor":       obj["attributedTo"],
		"in_reply_to": obj["inReplyTo"],
		"updated":     obj["updated"],
		"content_url": obj["url"],
	} {
		got, present := meta[key]
		if !present {
			continue
		}
		if key == "actor" || key == "content_url" {
			if str(got) == "" {
				continue // written as "" when the object has none
			}
		}
		if !reflect.DeepEqual(got, want) {
			bad = append(bad, fmt.Sprintf("metadata %s is %v, the post says %v", key, got, want))
		}
	}
	bad = append(bad, checkAttachments(meta, rule == apcanon.HashRuleV2, obj, digests)...)
	return bad
}

// checkAttachments requires protocol_metadata.attachments to be, in
// order, the object's own http(s) attachments (a prefix of them, with the
// rest counted in attachments_omitted), with matching url, type, alt text,
// layout hints and, where this runner computed it, file digest.
//
// Under ap-canonical-v2 a record may link only a file whose bytes it
// attests: an entry with a url must carry the file's sha256 digest. An
// oversize file is described, never linked; a client would render the link
// under a record that cannot vouch for it. (v1 records predate the rule.)
func checkAttachments(meta map[string]any, v2 bool, obj map[string]any, digests map[string]string) []string {
	listed, present := meta["attachments"]
	if !present {
		return nil
	}
	entries, ok := listed.([]any)
	if !ok {
		return []string{"metadata attachments is not a list"}
	}
	var actual []map[string]any
	var src []any
	switch a := obj["attachment"].(type) {
	case []any:
		src = a
	case map[string]any:
		src = []any{a}
	}
	for _, item := range src {
		if m, ok := item.(map[string]any); ok {
			u := apcanon.AttachmentURL(m["url"])
			if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
				actual = append(actual, m)
			}
		}
	}
	omitted := 0
	if n, ok := meta["attachments_omitted"].(float64); ok {
		omitted = int(n)
	}
	if len(entries)+omitted != len(actual) || len(entries) > len(actual) {
		return []string{fmt.Sprintf("metadata lists %d attachment(s) (+%d omitted), the post has %d", len(entries), omitted, len(actual))}
	}
	var bad []string
	for i, e := range entries {
		entry, _ := e.(map[string]any)
		a := actual[i]
		u := apcanon.AttachmentURL(a["url"])
		checks := map[string]any{"url": u, "media_type": a["mediaType"], "name": a["name"], "blurhash": a["blurhash"]}
		for key, want := range checks {
			if got, present := entry[key]; present && fmt.Sprint(got) != fmt.Sprint(want) {
				bad = append(bad, fmt.Sprintf("attachment %d %s is %v, the post says %v", i, key, got, want))
			}
		}
		for _, key := range []string{"width", "height"} {
			if got, present := entry[key]; present && fmt.Sprint(got) != fmt.Sprint(a[key]) {
				bad = append(bad, fmt.Sprintf("attachment %d %s is %v, the post says %v", i, key, got, a[key]))
			}
		}
		if got, present := entry["digest"]; present {
			if want, computed := digests[u]; computed && got != want {
				bad = append(bad, fmt.Sprintf("attachment %d digest is %v, the file hashes to %s", i, got, want))
			}
		}
		if _, linked := entry["url"]; linked && v2 {
			digest, _ := entry["digest"].(string)
			if !strings.HasPrefix(digest, "sha256:") || !strings.HasPrefix(digests[u], "sha256:") {
				bad = append(bad, fmt.Sprintf("attachment %d links a file the record does not attest (digest %q)", i, digests[u]))
			}
		}
	}
	return bad
}

// checkAuthor requires creator_identity's username to be the author's: a
// handle on the right host can still name the wrong person. The actor is
// fetched once and cached.
func (v *Verifier) checkAuthor(ctx context.Context, c PendingContent, obj map[string]any) (string, error) {
	if c.CreatorIdentity == "" {
		return "", nil
	}
	actor, _ := obj["attributedTo"].(string)
	if actor == "" {
		return "creator_identity is set but the post names no author", nil
	}
	username, ok := v.usernames[actor]
	if !ok {
		actorObj, err := v.fetch(ctx, actor)
		if err != nil {
			return "", fmt.Errorf("fetch author %s: %w", actor, err)
		}
		username, _ = actorObj["preferredUsername"].(string)
		if v.usernames == nil {
			v.usernames = map[string]string{}
		}
		v.usernames[actor] = username
	}
	handle := strings.TrimPrefix(c.CreatorIdentity, "@")
	user := handle
	if at := strings.LastIndex(handle, "@"); at >= 0 {
		user = handle[:at]
	}
	if !strings.EqualFold(user, username) {
		return fmt.Sprintf("creator_identity names %q, the post's author is %q", user, username), nil
	}
	return "", nil
}
