package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"sparkdream/tools/contentscan"
)

// Subject kinds the worker scans (docs/content-scanning.md §5.1).
const (
	kindBody = "body" // a media-labelled on-chain body, read through a body query
	kindRef  = "ref"  // an off-chain reference body (IPFS/Arweave/Filecoin/Jackal id)
	kindURI  = "uri"  // an external URL: a collect URI field or a URL inside a rendered body
)

// subject is one thing to judge.
type subject struct {
	kind        string
	record      string // module/type/id
	contentType string // chain enum name, for bodies
	bodyHashHex string // the chain's body_hash, for blog/forum bodies
	bodyPath    string // LCD path of the body query
	bodyField   string // JSON field holding the body in that response
	uri         string // for kindURI / resolved kindRef
	cid         string // for kindRef
}

// key identifies a subject for the done-set: a body is done per stored
// version (its hash), a URI per URI (re-scanned after its verdict expires).
func (s subject) key() string {
	switch s.kind {
	case kindBody:
		return s.record + "#" + s.bodyHashHex
	case kindRef:
		return s.record + "#" + s.cid
	default:
		return "uri#" + s.uri
	}
}

// lister is the read side of the chain client.
type lister interface {
	GetJSON(ctx context.Context, path string, out any) error
}

const pageSize = 100

// pageAll walks an LCD list endpoint by offset until a short page.
func pageAll(ctx context.Context, c lister, path string, decode func(raw json.RawMessage) (int, error)) error {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	for offset := 0; ; offset += pageSize {
		var raw json.RawMessage
		if err := c.GetJSON(ctx, fmt.Sprintf("%s%spagination.offset=%d&pagination.limit=%d", path, sep, offset, pageSize), &raw); err != nil {
			return err
		}
		n, err := decode(raw)
		if err != nil {
			return err
		}
		if n < pageSize {
			return nil
		}
	}
}

// flagged reports whether an LCD media_flags value (number or quoted
// number, absent when zero) is non-zero.
func flagged(v json.Number) bool { return v != "" && v != "0" }

func b64ToHex(s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

type blogPost struct {
	ID          string      `json:"id"`
	Body        string      `json:"body"`
	ContentType string      `json:"content_type"`
	MediaFlags  json.Number `json:"media_flags"`
	BodyHash    string      `json:"body_hash"`
	ReplyCount  string      `json:"reply_count"`
}

type forumPost struct {
	PostID      string      `json:"post_id"`
	Content     string      `json:"content"`
	ContentType string      `json:"content_type"`
	MediaFlags  json.Number `json:"media_flags"`
	BodyHash    string      `json:"body_hash"`
}

type fedContent struct {
	ID         string      `json:"id"`
	Body       string      `json:"body"`
	MediaFlags json.Number `json:"media_flags"`
}

type collectItem struct {
	ID       string `json:"id"`
	ImageURI string `json:"image_uri"`
	Link     *struct {
		URI string `json:"uri"`
	} `json:"link"`
	Nft *struct {
		TokenURI string `json:"token_uri"`
	} `json:"nft"`
}

type collection struct {
	ID       string `json:"id"`
	CoverURI string `json:"cover_uri"`
}

// isOffchainType reports whether a content type's body is an id resolved
// through a storage gateway.
func isOffchainType(ct string) bool {
	switch ct {
	case "CONTENT_TYPE_IPFS", "CONTENT_TYPE_ARWEAVE", "CONTENT_TYPE_FILECOIN", "CONTENT_TYPE_JACKAL":
		return true
	}
	return false
}

// bodySubject builds the subject for one labelled blog/forum record.
func (w *Worker) bodySubject(record, contentType, bodyHashB64, bodyPath, bodyField string) subject {
	s := subject{
		kind: kindBody, record: record, contentType: contentType,
		bodyHashHex: b64ToHex(bodyHashB64), bodyPath: bodyPath, bodyField: bodyField,
	}
	if isOffchainType(contentType) {
		// List queries withhold labelled bodies, so the id comes from the
		// body query at judge time; keyed by body hash until then.
		s.kind = kindRef
		s.cid = s.bodyHashHex
	}
	return s
}

// collect gathers every subject currently on chain. Plain-text bodies arrive
// in list responses, so the URLs inside them are collected without a body
// query; labelled bodies are listed as body/ref subjects.
func (w *Worker) collect(ctx context.Context) ([]subject, error) {
	var out []subject
	addURLs := func(text string) {
		for _, u := range contentscan.ExtractURLs([]byte(text)) {
			out = append(out, subject{kind: kindURI, uri: u})
		}
	}

	// Blog posts and their replies.
	var postsWithReplies []string
	if err := pageAll(ctx, w.chain, "/sparkdream/blog/v1/list_post", func(raw json.RawMessage) (int, error) {
		var resp struct {
			Post []blogPost `json:"post"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return 0, err
		}
		for _, p := range resp.Post {
			if flagged(p.MediaFlags) {
				out = append(out, w.bodySubject("blog/post/"+p.ID, p.ContentType, p.BodyHash, "/sparkdream/blog/v1/post_body/"+url.PathEscape(p.ID), "body"))
			} else {
				addURLs(p.Body)
			}
			if p.ReplyCount != "" && p.ReplyCount != "0" {
				postsWithReplies = append(postsWithReplies, p.ID)
			}
		}
		return len(resp.Post), nil
	}); err != nil {
		return nil, fmt.Errorf("blog posts: %w", err)
	}
	for _, id := range postsWithReplies {
		if err := pageAll(ctx, w.chain, "/sparkdream/blog/v1/list_replies/"+url.PathEscape(id), func(raw json.RawMessage) (int, error) {
			var resp struct {
				Replies []blogPost `json:"replies"`
			}
			if err := json.Unmarshal(raw, &resp); err != nil {
				return 0, err
			}
			for _, r := range resp.Replies {
				if flagged(r.MediaFlags) {
					out = append(out, w.bodySubject("blog/reply/"+r.ID, r.ContentType, r.BodyHash, "/sparkdream/blog/v1/reply_body/"+url.PathEscape(r.ID), "body"))
				} else {
					addURLs(r.Body)
				}
			}
			return len(resp.Replies), nil
		}); err != nil {
			return nil, fmt.Errorf("blog replies of %s: %w", id, err)
		}
	}

	// Forum posts.
	if err := pageAll(ctx, w.chain, "/sparkdream/forum/v1/post", func(raw json.RawMessage) (int, error) {
		var resp struct {
			Post []forumPost `json:"post"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return 0, err
		}
		for _, p := range resp.Post {
			if flagged(p.MediaFlags) {
				out = append(out, w.bodySubject("forum/post/"+p.PostID, p.ContentType, p.BodyHash, "/sparkdream/forum/v1/post_content/"+url.PathEscape(p.PostID), "content"))
			} else {
				addURLs(p.Content)
			}
		}
		return len(resp.Post), nil
	}); err != nil {
		return nil, fmt.Errorf("forum posts: %w", err)
	}

	// Federated content: labelled bodies are scanned as text bodies; the
	// record carries no chain body_hash, so the hash of the fetched body is
	// used (resolved at judge time).
	if err := pageAll(ctx, w.chain, "/sparkdream/federation/v1/list_federated_content", func(raw json.RawMessage) (int, error) {
		var resp struct {
			Content []fedContent `json:"content"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return 0, err
		}
		for _, c := range resp.Content {
			if flagged(c.MediaFlags) {
				out = append(out, subject{
					kind: kindBody, record: "federation/content/" + c.ID, contentType: "CONTENT_TYPE_HTML",
					bodyPath: "/sparkdream/federation/v1/federated_content_body/" + url.PathEscape(c.ID), bodyField: "body",
				})
			} else {
				addURLs(c.Body)
			}
		}
		return len(resp.Content), nil
	}); err != nil {
		return nil, fmt.Errorf("federated content: %w", err)
	}

	// Collect: URI fields of public collections and their items.
	var collIDs []string
	if err := pageAll(ctx, w.chain, "/sparkdream/collect/v1/public_collections", func(raw json.RawMessage) (int, error) {
		var resp struct {
			Collections []collection `json:"collections"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return 0, err
		}
		for _, c := range resp.Collections {
			collIDs = append(collIDs, c.ID)
			if c.CoverURI != "" {
				out = append(out, subject{kind: kindURI, record: "collect/collection/" + c.ID, uri: c.CoverURI})
			}
		}
		return len(resp.Collections), nil
	}); err != nil {
		return nil, fmt.Errorf("collections: %w", err)
	}
	for _, id := range collIDs {
		if err := pageAll(ctx, w.chain, "/sparkdream/collect/v1/items/"+url.PathEscape(id), func(raw json.RawMessage) (int, error) {
			var resp struct {
				Items []collectItem `json:"items"`
			}
			if err := json.Unmarshal(raw, &resp); err != nil {
				return 0, err
			}
			for _, it := range resp.Items {
				rec := "collect/item/" + it.ID
				for _, u := range []string{it.ImageURI, linkURI(it), nftURI(it)} {
					if u != "" {
						out = append(out, subject{kind: kindURI, record: rec, uri: u})
					}
				}
			}
			return len(resp.Items), nil
		}); err != nil {
			return nil, fmt.Errorf("items of collection %s: %w", id, err)
		}
	}
	return dedupe(out), nil
}

func linkURI(it collectItem) string {
	if it.Link == nil {
		return ""
	}
	return it.Link.URI
}

func nftURI(it collectItem) string {
	if it.Nft == nil {
		return ""
	}
	return it.Nft.TokenURI
}

func dedupe(in []subject) []subject {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		k := s.key()
		if s.kind == kindURI {
			k += "@" + s.record
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}
