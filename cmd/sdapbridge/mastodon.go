package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"sparkdream/tools/apcanon"
)

// MastodonClient is the discovery half: an OAuth app token against the
// daemon's own instance. The token is a second credential with a
// different blast radius than the chain key — scope it read-only and
// rotate it independently.
type MastodonClient struct {
	base  string
	token string
	http  *http.Client
}

func NewMastodonClient(base, token string) *MastodonClient {
	return &MastodonClient{
		base:  strings.TrimSuffix(base, "/"),
		token: token,
		http:  &http.Client{Timeout: 20 * time.Second},
	}
}

// Status is the slice of the REST Status shape discovery needs. The
// AS2 object — not this — is what gets hashed.
type Status struct {
	ID          string  `json:"id"`
	URI         string  `json:"uri"` // the AP object id, e.g. https://host/users/x/statuses/1
	URL         string  `json:"url"` // canonical web URL, e.g. https://host/@x/1
	Content     string  `json:"content"`
	SpoilerText string  `json:"spoiler_text"`
	Visibility  string  `json:"visibility"` // triage only; the ingest gate re-checks the AS2 audience
	CreatedAt   string  `json:"created_at"`
	EditedAt    *string `json:"edited_at"` // null until the first edit
	InReplyToID *string `json:"in_reply_to_id"`
	Reblog      *Status `json:"reblog"` // non-null ⇒ boost ⇒ never anchor
	Account     Account `json:"account"`
}

type Account struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Acct        string `json:"acct"` // "user" locally, "user@remote" for remote
	DisplayName string `json:"display_name"`
	URI         string `json:"uri"` // AP actor id
	URL         string `json:"url"`
	// Consent inputs (see consent.go), as the bridge's own instance last
	// synced them from the author's actor.
	Note      string  `json:"note"`      // bio, HTML
	Fields    []Field `json:"fields"`    // profile metadata
	Indexable *bool   `json:"indexable"` // Mastodon 4.2+; nil when the server does not say
}

// Field is one profile metadata row (name/value, HTML).
type Field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// RateLimitError lets the run loop back off instead of hammering the
// instance when it starts refusing.
type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("mastodon: rate limited (retry after %s)", e.RetryAfter)
}

// HomeTimeline fetches the bridge account's home timeline. This is the
// HOME timeline, not local: local is public posts originating on the
// bridge's own instance, useless for mirroring accounts elsewhere.
// sinceID, when non-empty, resumes from the last status the daemon
// processed instead of re-reading (and re-dropping) the same window.
func (m *MastodonClient) HomeTimeline(ctx context.Context, limit int, sinceID string) ([]Status, error) {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	if sinceID != "" {
		q.Set("since_id", sinceID)
	}
	var out []Status
	if err := m.getJSON(ctx, "/api/v1/timelines/home?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// maxStatusesPerLookup is Mastodon's cap on GET /api/v1/statuses?id[]=
// (DEFAULT_STATUSES_LIMIT); a larger batch is rejected outright.
const maxStatusesPerLookup = 20

// Statuses fetches up to maxStatusesPerLookup statuses by REST id in one
// request against the bridge's own instance. Ids the instance no longer
// has (deleted, or no longer visible) are simply absent from the result.
func (m *MastodonClient) Statuses(ctx context.Context, ids []string) ([]Status, error) {
	if len(ids) > maxStatusesPerLookup {
		return nil, fmt.Errorf("mastodon: %d status ids exceeds the per-request cap of %d", len(ids), maxStatusesPerLookup)
	}
	q := url.Values{}
	for _, id := range ids {
		q.Add("id[]", id)
	}
	var out []Status
	if err := m.getJSON(ctx, "/api/v1/statuses?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// LookupStatusID maps an AS2 object id to the bridge instance's REST id,
// for statuses that arrived by outbox backfill rather than the timeline
// and so have no REST id of their own. URL lookups only happen with
// resolve=true, which needs the app token; the instance may fetch the
// object from its origin to answer. Returns "" when it has no match.
func (m *MastodonClient) LookupStatusID(ctx context.Context, uri string) (string, error) {
	q := url.Values{}
	q.Set("q", uri)
	q.Set("type", "statuses")
	q.Set("resolve", "true")
	q.Set("limit", "1")
	var out struct {
		Statuses []Status `json:"statuses"`
	}
	if err := m.getJSON(ctx, "/api/v2/search?"+q.Encode(), &out); err != nil {
		return "", err
	}
	for _, st := range out.Statuses {
		if st.URI == uri {
			return st.ID, nil
		}
	}
	return "", nil
}

// Outbox fetches an actor's recent object ids from their AS2 outbox.
// Used once per newly-seen account: the home timeline only carries what
// arrived after the follow, so the first pass on an account walks its
// recent history.
//
// Two things this must NOT do, both of which it used to:
//
//   - Route through getJSON. actorURI is an ABSOLUTE remote URL, and
//     getJSON builds m.base+path, which produced
//     "https://my.instance/https://remote.example/users/bob/outbox".
//     Worse, getJSON attaches the bridge's own OAuth bearer token to
//     every request — once the URL was correct, that token would have
//     been handed to every followed remote instance. Remote fetches go
//     out unauthenticated (or HTTP-signature-signed), never with the
//     local app token.
//   - Read orderedItems off the collection root. A Mastodon /outbox is an
//     OrderedCollection carrying totalItems and a `first` link; the items
//     live on the page, which is what ?page=true returns.
func (m *MastodonClient) Outbox(ctx context.Context, actorURI string) ([]string, error) {
	u, err := url.Parse(actorURI)
	if err != nil || !u.IsAbs() {
		return nil, fmt.Errorf("mastodon: outbox: %q is not an absolute actor URI", actorURI)
	}
	outboxURL := strings.TrimSuffix(actorURI, "/") + "/outbox?page=true"

	page, err := m.fetchOutboxPage(ctx, outboxURL)
	if err != nil {
		return nil, err
	}
	// Some servers answer the ?page=true form with the collection root
	// anyway. Follow `first` once rather than returning nothing.
	if len(page.OrderedItems) == 0 && page.First != "" {
		if page, err = m.fetchOutboxPage(ctx, page.First); err != nil {
			return nil, err
		}
	}

	var uris []string
	for _, item := range page.OrderedItems {
		switch v := item.(type) {
		case string:
			uris = append(uris, v)
		case map[string]any:
			// Only Create activities anchor. Announce is a boost, and
			// anchoring one would attribute someone else's post to this
			// actor — the same rule pollOnce applies to reblogs.
			if t, _ := v["type"].(string); t != "" && t != "Create" {
				continue
			}
			if obj, ok := v["object"].(map[string]any); ok {
				if id, ok := obj["id"].(string); ok {
					uris = append(uris, id)
				}
			} else if id, ok := v["object"].(string); ok {
				uris = append(uris, id)
			}
		}
	}
	return uris, nil
}

type outboxPage struct {
	First        string `json:"-"`
	OrderedItems []any  `json:"orderedItems"`
}

// fetchOutboxPage does an unauthenticated AS2 GET against a remote actor's
// outbox. No bearer token: see the note on Outbox.
func (m *MastodonClient) fetchOutboxPage(ctx context.Context, pageURL string) (outboxPage, error) {
	var page outboxPage
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return page, err
	}
	req.Header.Set("Accept", apcanon.AcceptAS2)
	req.Header.Set("User-Agent", "sparkdream-sdapbridge/"+apcanon.HashRuleName)

	resp, err := m.http.Do(req)
	if err != nil {
		return page, fmt.Errorf("mastodon: outbox %s: %w", pageURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusTooManyRequests {
		return page, &RateLimitError{RetryAfter: retryAfter(resp)}
	}
	if resp.StatusCode != http.StatusOK {
		return page, fmt.Errorf("mastodon: outbox %s: status %d: %.200s",
			pageURL, resp.StatusCode, body)
	}

	var raw struct {
		First        any   `json:"first"`
		OrderedItems []any `json:"orderedItems"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return page, fmt.Errorf("mastodon: outbox %s: decode: %w", pageURL, err)
	}
	page.OrderedItems = raw.OrderedItems
	switch f := raw.First.(type) {
	case string:
		page.First = f
	case map[string]any:
		// An inline OrderedCollectionPage: take its items directly.
		if id, ok := f["id"].(string); ok {
			page.First = id
		}
		if items, ok := f["orderedItems"].([]any); ok && len(page.OrderedItems) == 0 {
			page.OrderedItems = items
			page.First = ""
		}
	}
	return page, nil
}

// retryAfter reads the Retry-After header, capped so a hostile or
// misconfigured instance cannot park the daemon for days.
func retryAfter(resp *http.Response) time.Duration {
	const maxBackoff = 15 * time.Minute
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	secs, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || secs <= 0 {
		return 0
	}
	d := time.Duration(secs) * time.Second
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

// VerifyCredentials returns the bridge's own account.
func (m *MastodonClient) VerifyCredentials(ctx context.Context) (Account, error) {
	var out Account
	err := m.getJSON(ctx, "/api/v1/accounts/verify_credentials", &out)
	return out, err
}

// maxFollowingPages caps one reconcile sweep's walk of the follow list
// (80 per page).
const maxFollowingPages = 25

// Following lists the accounts accountID follows.
func (m *MastodonClient) Following(ctx context.Context, accountID string) ([]Account, error) {
	return m.accountList(ctx, "/api/v1/accounts/"+url.PathEscape(accountID)+"/following?limit=80")
}

// Followers lists the accounts following accountID: in opt-in consent
// mode, the authors who asked to be bridged.
func (m *MastodonClient) Followers(ctx context.Context, accountID string) ([]Account, error) {
	return m.accountList(ctx, "/api/v1/accounts/"+url.PathEscape(accountID)+"/followers?limit=80")
}

// accountList walks Mastodon's Link header pagination over an account
// list (the cursor is a relationship id, not an account id, so it cannot
// be computed from the page).
func (m *MastodonClient) accountList(ctx context.Context, first string) ([]Account, error) {
	var all []Account
	next := first
	for page := 0; next != "" && page < maxFollowingPages; page++ {
		var out []Account
		link, err := m.getJSONLink(ctx, next, &out)
		if err != nil {
			return nil, err
		}
		all = append(all, out...)
		next = nextLink(link, m.base)
	}
	return all, nil
}

// AccountStatuses returns up to 40 of an account's statuses right after
// minID, OLDEST first. min_id pages forward from the cursor, where since_id
// would return the newest 40 and silently drop anything older in a burst.
func (m *MastodonClient) AccountStatuses(ctx context.Context, accountID, minID string) ([]Status, error) {
	q := url.Values{}
	q.Set("min_id", minID)
	q.Set("limit", "40")
	q.Set("exclude_reblogs", "true")
	var out []Status
	if err := m.getJSON(ctx, "/api/v1/accounts/"+url.PathEscape(accountID)+"/statuses?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	// Mastodon returns newest-first even for min_id; walk oldest-first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// nextLink extracts the rel="next" target of a Link header as a path on
// base. Links pointing anywhere else are ignored: the header is read with
// the app token attached, and following it off-instance would leak it.
func nextLink(header, base string) string {
	for _, part := range strings.Split(header, ",") {
		segs := strings.Split(part, ";")
		if len(segs) < 2 || !strings.Contains(strings.Join(segs[1:], ";"), `rel="next"`) {
			continue
		}
		target := strings.Trim(strings.TrimSpace(segs[0]), "<>")
		if !strings.HasPrefix(target, base+"/") {
			return ""
		}
		return strings.TrimPrefix(target, base)
	}
	return ""
}

func (m *MastodonClient) getJSON(ctx context.Context, path string, out any) error {
	_, err := m.getJSONLink(ctx, path, out)
	return err
}

// getJSONLink is getJSON that also returns the Link response header.
func (m *MastodonClient) getJSONLink(ctx context.Context, path string, out any) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.base+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+m.token)
	resp, err := m.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("mastodon: get %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return "", &RateLimitError{RetryAfter: retryAfter(resp)}
	// 206 is Mastodon's home timeline while it rebuilds a returning
	// account's feed: a partial page, still valid.
	case resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent:
		return "", fmt.Errorf("mastodon: get %s: status %d: %.200s", path, resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return "", fmt.Errorf("mastodon: get %s: decode: %w", path, err)
	}
	return resp.Header.Get("Link"), nil
}

// handleFromAcct builds the @user@domain WebFinger identity
// creator_identity keys on (identity-link and resolve-remote-identity
// paths). Local accts carry no domain — append the instance host.
func handleFromAcct(acct, instanceHost string) string {
	if strings.Contains(acct, "@") {
		return "@" + acct
	}
	return "@" + acct + "@" + instanceHost
}

// instanceHost extracts the host from the instance base URL.
func (c Config) instanceHost() string {
	u, err := url.Parse(c.MastodonURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
