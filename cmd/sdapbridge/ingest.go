package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"sparkdream/internal/sdaptx"
	"sparkdream/tools/apcanon"
	"sparkdream/x/federation/types"
)

// Submitter is the seam between the ingestion pipeline and the chain.
// Production wires chainSubmitter; tests wire a fake and assert on the
// exact MsgSubmitFederatedContent produced.
type Submitter interface {
	Submit(ctx context.Context, msg *types.MsgSubmitFederatedContent) (contentID uint64, txHash string, err error)
}

// AS2Source is the ingestion-side fetch seam (tests rewrite hostnames).
type AS2Source interface {
	Fetch(ctx context.Context, uri string) (map[string]any, error)
}

// AS2Fetcher is the production AS2Source over apcanon.Fetch.
type AS2Fetcher struct {
	Signer *apcanon.HTTPSigner
	// AllowPrivateHosts relaxes apcanon's destination guard for a local
	// test instance (GoToSocial on localhost, httptest). Never set in
	// production: the URIs being fetched come from inbound federation
	// traffic, so the guard is what keeps a remote instance from
	// redirecting the daemon into its own host's network.
	AllowPrivateHosts bool
}

func (f *AS2Fetcher) Fetch(ctx context.Context, uri string) (map[string]any, error) {
	obj, _, err := apcanon.Fetch(ctx, uri, apcanon.FetchOptions{
		Signer:            f.Signer,
		AllowPrivateHosts: f.AllowPrivateHosts,
	})
	return obj, err
}

// Bridge wires the pipeline together.
type Bridge struct {
	cfg     Config
	masto   *MastodonClient
	fetcher AS2Source
	chain   Submitter
	state   *State

	lastRevisit time.Time // when the edit-revisit sweep last ran

	// operator is the bridge's own address: only records it anchored may be
	// named in a supersedes link.
	operator string

	// peers routes each post to the configured peer that owns its host.
	peers *peerSet

	// Consent state (consent.go): the bridge's followers for opt-in mode,
	// and when each refused author was last logged.
	followers   map[string]bool
	followersAt time.Time
	refusedAt   map[string]time.Time

	// digest fetches and hashes an attachment's file for ap-canonical-v2.
	digest apcanon.MediaDigester

	// heldUntil pauses new attempts per peer after a retryable failure
	// (retry.go). In memory: a restart just probes each peer once.
	heldUntil map[string]time.Time

	startedAt     time.Time // floor for the reconcile window on a fresh start
	lastReconcile time.Time // when the reconcile sweep last ran
	selfID        string    // the bridge's own Mastodon account id, cached

	// Author curation (curation.go): each peer's gate as the chain states
	// it, and when follows were last synced to it.
	gateSource func(context.Context, string) (authorGate, error)
	gates      map[string]cachedGate

	// members answers whether an address is an active member of this
	// chain, for follow-back (followback.go); nil disables follow-back.
	members             memberSource
	followBackOffLogged time.Time
	lastFollowSync      time.Time
}

// Run polls the home timeline forever, backing off on rate limits from
// either side, until ctx is cancelled.
func (b *Bridge) Run(ctx context.Context) error {
	for {
		if err := b.pollOnceSafe(ctx); err != nil {
			var rl *RateLimitError
			if errors.As(err, &rl) {
				wait := rl.RetryAfter
				if wait <= 0 {
					wait = 2 * time.Minute
				}
				// Jitter so a fleet of bridges told to back off by the
				// same instance does not return in lockstep. RetryAfter is
				// already capped at the parse site.
				wait += time.Duration(rand.Int63n(int64(wait/4) + 1))
				log.Printf("sdapbridge: rate limited, backing off %s", wait)
				if !sleepCtx(ctx, wait) {
					return ctx.Err()
				}
				continue
			}
			log.Printf("sdapbridge: poll failed: %v", err)
			if !sleepCtx(ctx, b.cfg.PollInterval) {
				return ctx.Err()
			}
			continue
		}
		if !sleepCtx(ctx, b.cfg.PollInterval) {
			return ctx.Err()
		}
	}
}

// pollOnceSafe turns a panic in one cycle into an error instead of
// killing the daemon. A malformed object from a remote instance should
// cost one cycle, not require an operator to notice the process is gone.
func (b *Bridge) pollOnceSafe(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in poll cycle: %v", r)
		}
	}()
	b.peers.refresh(ctx)
	if err := b.pollOnce(ctx); err != nil {
		return err
	}
	if err := b.retryDeferred(ctx); err != nil {
		return err
	}
	if b.cfg.RevisitInterval > 0 && time.Since(b.lastRevisit) >= b.cfg.RevisitInterval {
		b.lastRevisit = time.Now()
		if err := b.revisitOnce(ctx); err != nil {
			return err
		}
	}
	if b.cfg.FollowSyncInterval > 0 && time.Since(b.lastFollowSync) >= b.cfg.FollowSyncInterval {
		b.lastFollowSync = time.Now()
		// a failed sync (a read-only token, the instance down) costs this
		// pass, not the anchoring
		if err := b.syncFollows(ctx); err != nil {
			log.Printf("sdapbridge: follow sync: %v", err)
		}
		if err := b.syncFollowBacks(ctx); err != nil {
			log.Printf("sdapbridge: follow-back: %v", err)
		}
	}
	if b.cfg.ReconcileInterval > 0 && time.Since(b.lastReconcile) >= b.cfg.ReconcileInterval {
		b.lastReconcile = time.Now()
		return b.reconcileOnce(ctx)
	}
	return nil
}

func (b *Bridge) pollOnce(ctx context.Context) error {
	// since_id resumes where the last cycle stopped. Without it a burst of
	// more than one page between polls was dropped permanently — the
	// window simply moved past it.
	statuses, err := b.masto.HomeTimeline(ctx, homeTimelinePageSize, b.state.LastSeenStatusID())
	if err != nil {
		return err
	}
	// Mastodon returns newest-first. Walk oldest-first so the cursor only
	// ever moves forward over statuses actually processed.
	for i := len(statuses) - 1; i >= 0; i-- {
		st := statuses[i]
		if st.Reblog != nil {
			// Boosts would anchor other people's posts under the booster.
			b.state.AdvanceLastSeen(st.ID)
			continue
		}
		if peerID, held := b.peerHeld(st.URI); held {
			// Its peer is throttled: queue this post behind the earlier ones
			// instead of spending a transaction on a certain rejection.
			if !b.state.Defer(st, peerID, errPeerHeld, time.Now()) {
				log.Printf("sdapbridge: retry queue full (%d); holding the cursor at %s", maxDeferred, st.URI)
				break
			}
			b.state.AdvanceLastSeen(st.ID)
			continue
		}
		if err := b.IngestStatus(ctx, &st); err != nil {
			// A retryable failure is deferred and discovery moves on, so one
			// peer's trouble cannot stall the others. Only a condition that
			// affects every peer (the bridge's own instance throttling), or a
			// full retry queue, leaves the cursor here for the next cycle.
			if !b.deferOrHold(&st, err) {
				log.Printf("sdapbridge: ingest %s: %v", st.URI, err)
				break
			}
		}
		b.state.AdvanceLastSeen(st.ID)
		// First sight of an account: one outbox pass. The home timeline
		// only carries posts that arrived after the follow.
		if !b.state.Backfilled(st.Account.URI) && b.cfg.Backfill > 0 {
			b.backfill(ctx, &st.Account)
		}
	}
	return b.state.Save()
}

// errPeerHeld is the recorded cause for a post queued without an attempt
// because its peer was already held.
var errPeerHeld = errors.New("peer held after a throttled submission")

// homeTimelinePageSize is Mastodon's documented maximum for
// /api/v1/timelines/home. Paired with the since_id cursor it bounds how
// much one cycle can fall behind, rather than silently dropping the
// overflow.
const homeTimelinePageSize = 40

func (b *Bridge) backfill(ctx context.Context, acct *Account) {
	// No outbox walk for an author who has not consented. Not marked done:
	// if they opt in later, their next post triggers the pass.
	if !b.allowed(ctx, acct) {
		return
	}
	// Mark AFTER a successful pass, not before: marking first meant a
	// remote instance being briefly unreachable permanently cost that
	// account its history, with no retry for the life of the state file.
	uris, err := b.masto.Outbox(ctx, acct.URI)
	if err != nil {
		log.Printf("sdapbridge: outbox pass for %s failed (will retry next sighting): %v", acct.URI, err)
		return
	}
	b.state.MarkBackfilled(acct.URI)
	if int64(len(uris)) > b.cfg.Backfill {
		uris = uris[len(uris)-int(b.cfg.Backfill):]
	}
	for _, uri := range uris {
		if err := b.IngestURI(ctx, uri, acct); err != nil {
			if !b.deferOrHold(&Status{URI: uri, Account: *acct}, err) {
				log.Printf("sdapbridge: backfill ingest %s: %v", uri, err)
			}
		}
	}
}

// IngestStatus anchors a discovered REST status. Visibility comes from
// the AS2 audience, not the REST field: followers-only posts reach the
// home timeline once the bridge is an accepted follower, and the REST
// visibility string is the instance's claim, not the object's property.
func (b *Bridge) IngestStatus(ctx context.Context, st *Status) error {
	return b.ingest(ctx, st.URI, &st.Account, st.ID)
}

// IngestURI is the pipeline core: fetch AS2 → assert public → hash →
// dedupe on (uri, hash) → map fields → submit. body/creator/title come
// from the REST status when discovery supplied it; a backfilled uri
// with no Status falls back to the AS2 object itself.
func (b *Bridge) IngestURI(ctx context.Context, uri string, acct *Account) error {
	return b.ingest(ctx, uri, acct, "")
}

// ingest fetches uri and hands the object to ingestObject. statusID is the
// bridge instance's REST id when discovery supplied one ("" otherwise).
func (b *Bridge) ingest(ctx context.Context, uri string, acct *Account, statusID string) error {
	// Route to the peer that owns the post's host, and check the author's
	// consent, both before fetching anything of theirs.
	peerID, err := b.peers.route(uri)
	if err != nil {
		log.Printf("sdapbridge: skip %s", err)
		return nil
	}
	if !b.allowed(ctx, acct) {
		return nil
	}
	obj, err := b.fetcher.Fetch(ctx, uri)
	if err != nil {
		return fmt.Errorf("AS2 fetch: %w", err)
	}
	return b.ingestObject(ctx, uri, peerID, obj, acct, statusID)
}

// ingestObject anchors an already-fetched AS2 object and puts it on the
// edit-revisit list. Split from the fetch so the revisit sweep can check
// the object's `updated` before committing to it.
func (b *Bridge) ingestObject(ctx context.Context, uri, peerID string, obj map[string]any, acct *Account, statusID string) error {
	if !as2Public(obj) {
		log.Printf("sdapbridge: skip %s: not public per AS2 audience", uri)
		return nil
	}
	if b.publishedBeforeConsent(acct, as2String(obj["published"], "")) {
		log.Printf("sdapbridge: skip %s: published before its author's consent", uri)
		return nil
	}
	// Open content: the chain accepts only posts their author dedicated to
	// the public domain with #cc0 or #publicdomain. Checked before hashing,
	// so no media is downloaded for a post that will not be anchored. A
	// timeline status stays on the revisit list, so an author who adds the
	// tag in an edit is picked up; an anchored post edited to drop it keeps
	// its anchored version (a dedication cannot be withdrawn) and the new
	// version is not anchored.
	if apcanon.License(obj) == "" {
		log.Printf("sdapbridge: skip %s: no #cc0 or #publicdomain hashtag", uri)
		if statusID != "" {
			b.state.Track(uri, statusID, as2String(obj["updated"], ""), time.Now().Unix(), false)
		}
		return b.state.Save()
	}
	// Media digests are recorded while hashing, so the metadata can carry
	// them without fetching every file twice.
	digests := map[string]string{}
	hash, err := apcanon.HashFor(ctx, b.cfg.HashRule, obj, b.recordingDigester(digests))
	if err != nil {
		return fmt.Errorf("hash: %w", err)
	}
	if b.state.Has(uri, hash[:]) {
		b.track(ctx, uri, statusID, obj, false)
		return nil // already anchored this exact version
	}

	msg := b.buildMsg(uri, peerID, obj, acct, hash[:], digests)
	// The chain holds the attribution to the peer's hosts too. An author
	// whose handle domain differs from the post's host (a WEB_DOMAIN split
	// the policy does not list) would be refused anyway: skip without
	// paying for the rejection.
	if msg.CreatorIdentity != "" {
		if err := types.CreatorIdentityHostAllowed(peerID, b.peers.hosts[peerID], msg.CreatorIdentity); err != nil {
			log.Printf("sdapbridge: skip %s: %v (list the host in the peer's content_hosts if it is the same instance)", uri, err)
			return nil
		}
	}
	// An edit: same uri, new hash. Chain the versions for readers, and when
	// this bridge anchored the previous version, on-chain too: supersedes
	// retires a still-pending predecessor, which can no longer be verified,
	// instead of letting it expire as unverified against this operator.
	// Content id 0 doubles as "unknown" in the local cache, so the chain's
	// very first record never gets a chain link, only the metadata one.
	isEdit := false
	if prev, ok := b.state.LatestFor(uri); ok && prev.HashB64 != base64.StdEncoding.EncodeToString(hash[:]) {
		isEdit = true
		if prev.ContentID != 0 {
			msg.ProtocolMetadata = appendProtocolSupersedes(msg.ProtocolMetadata, prev.ContentID)
			if prev.Operator != "" && prev.Operator == b.operator {
				msg.Supersedes = &types.ContentRef{ContentId: prev.ContentID}
			}
		}
	}

	contentID, txHash, err := b.chain.Submit(ctx, msg)
	if errors.Is(err, ErrSupersedeRejected) {
		// Usually a concurrent edit got there first. The new version still
		// has to be anchored; it just goes without the chain link.
		log.Printf("sdapbridge: %s: %v; resubmitting without the link", uri, err)
		msg.Supersedes = nil
		contentID, txHash, err = b.chain.Submit(ctx, msg)
	}
	switch {
	case errors.Is(err, ErrPermanentRejection):
		// Policy refuses this post outright. Nothing is recorded, and the
		// caller moves past it instead of retrying forever.
		log.Printf("sdapbridge: skip %s: %v", uri, err)
		return nil
	case errors.Is(err, ErrDuplicateContent):
		// The chain already holds this exact (peer, hash): an earlier
		// attempt landed and the local cache lost track. Record it so the
		// daemon stops retrying, but with no content id — nothing here
		// knows which record it collided with.
		b.state.Record(uri, hash[:], 0, "", txHash, time.Now().Unix())
		b.track(ctx, uri, statusID, obj, true)
		log.Printf("sdapbridge: %s already anchored on chain, caching", uri)
		return b.state.Save()
	case err != nil:
		// Nothing is recorded: an unconfirmed submission must stay
		// retryable, which is the whole reason Submit waits for DeliverTx.
		// The caller defers it (ErrPeerThrottled included) per peer.
		return fmt.Errorf("submit: %w", err)
	}
	b.state.Record(uri, hash[:], contentID, b.operator, txHash, time.Now().Unix())
	b.track(ctx, uri, statusID, obj, true)
	if n := countOversize(digests); n > 0 {
		log.Printf("sdapbridge: %s: %d attachment(s) over %d MiB: described but not attested, and not linked on-chain",
			uri, n, apcanon.MaxMediaBytes>>20)
	}
	log.Printf("sdapbridge: anchored %s id=%d hash=%s tx=%s (edit=%v supersedes=%v)",
		uri, contentID, shortB64(hash[:]), txHash, isEdit, msg.Supersedes != nil)
	return b.state.Save()
}

// track puts uri on the edit-revisit list at the version just hashed. A
// status that arrived by outbox backfill has no REST id, so resolve one
// once through the bridge's own instance; without it the status cannot
// be revisited and its edits would never be anchored.
func (b *Bridge) track(ctx context.Context, uri, statusID string, obj map[string]any, anchored bool) {
	if statusID == "" && b.masto != nil {
		if t, ok := b.state.Tracking(uri); !ok || t.StatusID == "" {
			id, err := b.masto.LookupStatusID(ctx, uri)
			switch {
			case err != nil:
				log.Printf("sdapbridge: no REST id for %s, its edits will not be revisited: %v", uri, err)
			case id == "":
				log.Printf("sdapbridge: instance does not know %s, its edits will not be revisited", uri)
			}
			statusID = id
		}
	}
	b.state.Track(uri, statusID, as2String(obj["updated"], ""), time.Now().Unix(), anchored)
}

// revisitOnce re-checks recently anchored statuses for edits. Discovery
// walks the home timeline forward with since_id, and an edited status
// keeps its id and position there, so this sweep is the only thing that
// ever sees an edit. It costs one REST request per maxStatusesPerLookup
// statuses against the bridge's own instance, and an AS2 fetch only for a
// status whose edited_at moved past the version last hashed.
func (b *Bridge) revisitOnce(ctx context.Context) error {
	cutoff := time.Now().Add(-b.cfg.RevisitWindow).Unix()
	tracked := b.state.Revisitable(cutoff)
	uriByID := make(map[string]string, len(tracked))
	ids := make([]string, 0, len(tracked))
	for uri, t := range tracked {
		uriByID[t.StatusID] = uri
		ids = append(ids, t.StatusID)
	}
	sort.Strings(ids)

	for start := 0; start < len(ids); start += maxStatusesPerLookup {
		end := min(start+maxStatusesPerLookup, len(ids))
		statuses, err := b.masto.Statuses(ctx, ids[start:end])
		if err != nil {
			return err
		}
		for i := range statuses {
			st := &statuses[i]
			uri, ok := uriByID[st.ID]
			if !ok || !editedSince(st.EditedAt, tracked[uri].Updated) {
				continue
			}
			peerID, held := b.peerHeld(uri)
			if peerID == "" || held || !b.allowed(ctx, &st.Account) {
				continue // peer dropped or throttled, or consent withdrawn; next sweep
			}
			obj, err := b.fetcher.Fetch(ctx, uri)
			if err != nil {
				log.Printf("sdapbridge: revisit %s: AS2 fetch: %v", uri, err)
				continue
			}
			// Mastodon caches AS2 bodies for up to 3 minutes, keyed without
			// updated_at, so REST can report an edit AS2 does not serve yet.
			// Hashing now would re-see the old version; leave it for the
			// next sweep instead, having recorded nothing.
			if editedSince(st.EditedAt, as2String(obj["updated"], "")) {
				log.Printf("sdapbridge: %s edited at %s but AS2 still serves updated=%q; retrying next sweep",
					uri, *st.EditedAt, as2String(obj["updated"], ""))
				continue
			}
			if err := b.ingestObject(ctx, uri, peerID, obj, &st.Account, st.ID); err != nil {
				var rl *RateLimitError
				if errors.As(err, &rl) {
					return err
				}
				if errors.Is(err, ErrPeerThrottled) {
					// Tracking was not advanced, so the next sweep retries it;
					// meanwhile spend nothing more on this peer.
					b.holdPeer(peerID, time.Now().Add(retryDelay(1)))
				}
				log.Printf("sdapbridge: revisit %s: %v", uri, err)
			}
		}
	}
	return b.state.Save()
}

// editedSince reports whether REST edited_at is later than an AS2
// `updated` value. AS2 carries whole seconds and REST carries
// milliseconds, so the comparison truncates to the second. An unparseable
// value counts as an edit: a wasted fetch is cheaper than a missed one.
func editedSince(editedAt *string, updated string) bool {
	if editedAt == nil || *editedAt == "" {
		return false
	}
	e, err := time.Parse(time.RFC3339, *editedAt)
	if err != nil {
		return true
	}
	if updated == "" {
		return true
	}
	u, err := time.Parse(time.RFC3339, updated)
	if err != nil {
		return true
	}
	return e.Truncate(time.Second).After(u.Truncate(time.Second))
}

// buildMsg maps the AS2 object (+ discovery REST context) onto the
// nine MsgSubmitFederatedContent positionals.
func (b *Bridge) buildMsg(uri, peerID string, obj map[string]any, acct *Account, hash []byte, digests map[string]string) *types.MsgSubmitFederatedContent {
	contentType := "blog_post"
	if v, ok := obj["inReplyTo"].(string); ok && v != "" {
		contentType = "blog_reply"
	}

	creatorIdentity := handleFromActor(obj["attributedTo"], b.cfg.instanceHost())
	if acct != nil && acct.Acct != "" {
		creatorIdentity = handleFromAcct(acct.Acct, b.cfg.instanceHost())
	}
	creatorName := as2String(obj["name"], "")
	if acct != nil && acct.DisplayName != "" {
		creatorName = acct.DisplayName
	}
	title := as2String(obj["summary"], "")
	body := as2String(obj["content"], "")
	if int64(len(body)) > b.cfg.BodyLimit {
		body = body[:b.cfg.BodyLimit]
	}
	createdAt := time.Now().Unix()
	if p := as2String(obj["published"], ""); p != "" {
		if t, err := time.Parse(time.RFC3339, p); err == nil {
			createdAt = t.Unix()
		}
	}
	remoteID := uri
	if u := uri; strings.Contains(u, "/statuses/") {
		remoteID = u[strings.LastIndex(u, "/")+1:]
	}

	meta := map[string]any{
		"hash_rule":   b.hashRule(),
		"ap_type":     as2String(obj["type"], "Note"),
		"actor":       as2String(obj["attributedTo"], ""),
		"object_id":   uri,
		"in_reply_to": nullableString(obj["inReplyTo"]),
		"visibility":  "public",
		"updated":     nullableString(obj["updated"]),
		"content_url": as2String(obj["url"], ""),
	}
	metaJSON := fitAttachments(meta, attachmentMeta(obj, digests), b.metadataBudget())

	return &types.MsgSubmitFederatedContent{
		PeerId:          peerID,
		RemoteContentId: remoteID,
		ContentType:     contentType,
		CreatorIdentity: creatorIdentity,
		CreatorName:     creatorName,
		Title:           title,
		Body:            body,
		// content_uri is the AS2 id (the uri we fetched and hashed), NOT
		// the web permalink in obj["url"]. Three reasons: the verifier
		// re-fetches content_uri and must reach the exact representation
		// that was hashed; a permalink only resolves to AS2 because
		// Mastodon happens to content-negotiate, which no other AP server
		// guarantees; and a signed fetch of a permalink 302s, invalidating
		// the signature over (request-target). The permalink is kept in
		// protocol_metadata.content_url for human readers.
		ContentUri:       uri,
		ProtocolMetadata: metaJSON,
		RemoteCreatedAt:  createdAt,
		ContentHash:      hash,
		License:          apcanon.License(obj),
	}
}

// hashRule is the rule this bridge anchors under; empty means v1.
func (b *Bridge) hashRule() string {
	if b.cfg.HashRule == "" {
		return apcanon.HashRuleName
	}
	return b.cfg.HashRule
}

// recordingDigester wraps the media digester, remembering each file's
// digest by URL for the metadata. nil when the bridge has none (v1).
func (b *Bridge) recordingDigester(into map[string]string) apcanon.MediaDigester {
	if b.digest == nil {
		return nil
	}
	return func(ctx context.Context, url string) (string, error) {
		if d, ok := into[url]; ok {
			return d, nil
		}
		d, err := b.digest(ctx, url)
		if err == nil {
			into[url] = d
		}
		return d, err
	}
}

// countOversize counts the files that digested as oversize.
func countOversize(digests map[string]string) int {
	n := 0
	for _, d := range digests {
		if d == apcanon.DigestOversize {
			n++
		}
	}
	return n
}

// metadataReserve is left free in protocol_metadata for what is appended
// after buildMsg (supersedes_content_id).
const metadataReserve = 128

// metadataBudget is how large buildMsg's protocol_metadata may be. The
// chain truncates anything over max_protocol_metadata_size, and a JSON
// document cut mid-way is unreadable, so the bridge must fit it itself.
func (b *Bridge) metadataBudget() int {
	limit := b.cfg.MetadataLimit
	if limit <= 0 {
		limit = 8192 // the chain's default max_protocol_metadata_size
	}
	return int(limit) - metadataReserve
}

// attachmentMeta lists a post's media for readers: where each file is,
// its type and alt text, and the layout hints a client needs before it
// loads the file. Presentation only — the hash covers type and alt text,
// not the URL or the bytes, so these links are as good as the origin
// instance keeps them. Only http(s) links are kept: clients render them.
func attachmentMeta(obj map[string]any, digests map[string]string) []map[string]any {
	list, _ := obj["attachment"].([]any)
	if single, ok := obj["attachment"].(map[string]any); ok {
		list = []any{single}
	}
	var out []map[string]any
	for _, item := range list {
		a, ok := item.(map[string]any)
		if !ok {
			continue
		}
		href := apcanon.AttachmentURL(a["url"])
		if !strings.HasPrefix(href, "https://") && !strings.HasPrefix(href, "http://") {
			continue
		}
		entry := map[string]any{
			"media_type": as2String(a["mediaType"], ""),
			"name":       as2String(a["name"], ""),
		}
		// The link is recorded only for a file whose bytes were hashed. A
		// record vouches for what it shows, and a client renders the link:
		// an oversize file (or any file under v1) is described but not
		// linked. The post itself, at object_id, still has the link.
		if d := digests[href]; strings.HasPrefix(d, "sha256:") {
			entry["url"] = href
		}
		for _, k := range []string{"width", "height"} {
			if v := positiveInt(a[k]); v > 0 {
				entry[k] = v
			}
		}
		if bh := as2String(a["blurhash"], ""); bh != "" {
			entry["blurhash"] = bh
		}
		// Under ap-canonical-v2 the digest is part of the anchored hash; a
		// client can check the file it displays against it. "oversize"
		// says the file exists but is not attested.
		if d := digests[href]; d != "" {
			entry["digest"] = d
		}
		out = append(out, entry)
	}
	return out
}

// positiveInt reads a JSON number whichever way it was decoded: apcanon
// keeps json.Number (exact form matters for hashing), plain decoding gives
// float64. Anything else, or a non-positive value, is 0.
func positiveInt(v any) int64 {
	switch n := v.(type) {
	case json.Number:
		if i, err := n.Int64(); err == nil && i > 0 {
			return i
		}
	case float64:
		if n > 0 {
			return int64(n)
		}
	}
	return 0
}

// fitAttachments adds attachments to meta in order while the encoded
// document stays within budget. Whatever does not fit is counted in
// attachments_omitted rather than cut mid-entry; the full list is always
// one AS2 fetch away at object_id.
func fitAttachments(meta map[string]any, attachments []map[string]any, budget int) []byte {
	base, _ := json.Marshal(meta)
	if len(attachments) == 0 {
		return base
	}
	kept := attachments
	for {
		meta["attachments"] = kept
		if omitted := len(attachments) - len(kept); omitted > 0 {
			meta["attachments_omitted"] = omitted
		}
		out, _ := json.Marshal(meta)
		if len(out) <= budget || len(kept) == 0 {
			if len(kept) == 0 {
				delete(meta, "attachments")
				out, _ = json.Marshal(meta)
				if len(out) > budget {
					return base
				}
			}
			return out
		}
		kept = kept[:len(kept)-1]
	}
}

// chainContent is the slice of FederatedContent the daemon reads back over
// the LCD. Every scalar is a string because that is what grpc-gateway
// emits: proto uint64 fields serialize as QUOTED strings ("id":"7") and
// proto bytes fields as BASE64 ("content_hash":"3q2+7w=="). Declaring
// Id as a Go uint64 made encoding/json reject the whole response, and
// hex-decoding the hash skipped every row that survived.
type chainContent struct {
	ID               string          `json:"id"`
	ContentURI       string          `json:"content_uri"`
	RemoteContentID  string          `json:"remote_content_id"`
	ContentHashB64   string          `json:"content_hash"`
	ReceivedAt       string          `json:"received_at"`
	SubmittedBy      string          `json:"submitted_by"`
	ProtocolMetadata json.RawMessage `json:"protocol_metadata"`
}

// objectID returns the AS2 object id the bridge anchored this record
// under. content_uri is the canonical AS2 id (see buildMsg), and
// protocol_metadata.object_id repeats it; the metadata is consulted only
// as a fallback for records written before content_uri carried the id.
func (c chainContent) objectID() string {
	if c.ContentURI != "" {
		return c.ContentURI
	}
	var meta struct {
		ObjectID string `json:"object_id"`
	}
	if len(c.ProtocolMetadata) > 0 {
		_ = json.Unmarshal(c.ProtocolMetadata, &meta)
	}
	return meta.ObjectID
}

// WarmCacheFromChain rebuilds the (uri, hash) cache from the peer's
// anchored content after a restart or state-file loss, so the daemon
// cannot re-anchor history.
//
// The cache is keyed by the AS2 object id, which is what IngestURI looks
// up — so this must read back the same id the bridge anchored, not a
// different representation of the same post. That is why content_uri is
// the AS2 id rather than the web permalink.
func WarmCacheFromChain(ctx context.Context, chain *sdaptx.Client, peerID string, state *State) error {
	const pageSize = 100
	offset := uint64(0)
	for {
		var out struct {
			Content    []chainContent `json:"content"`
			Pagination struct {
				Total string `json:"total"`
			} `json:"pagination"`
		}
		path := fmt.Sprintf(
			"/sparkdream/federation/v1/list_federated_content?peer_id=%s&pagination.offset=%d&pagination.limit=%d",
			url.QueryEscape(peerID), offset, pageSize)
		if err := chain.GetJSON(ctx, path, &out); err != nil {
			return err
		}
		for _, c := range out.Content {
			hash, err := base64.StdEncoding.DecodeString(c.ContentHashB64)
			if err != nil || len(hash) != sha256.Size {
				log.Printf("sdapbridge: warm cache: content %s has an undecodable hash %q, skipping",
					c.ID, c.ContentHashB64)
				continue
			}
			uri := c.objectID()
			if uri == "" {
				continue
			}
			if state.Has(uri, hash) {
				continue
			}
			contentID, _ := strconv.ParseUint(c.ID, 10, 64)
			at, _ := strconv.ParseInt(c.ReceivedAt, 10, 64)
			if at == 0 {
				at = time.Now().Unix()
			}
			state.Record(uri, hash, contentID, c.SubmittedBy, "", at)
		}
		if uint64(len(out.Content)) < pageSize {
			return state.Save()
		}
		offset += pageSize
	}
}

// as2Public asserts the AS2 audience includes as#Public — in `to` or
// `cc`. Missing audience arrays count as not public: the daemon cannot
// verify what the object does not state.
func as2Public(obj map[string]any) bool {
	const public = "https://www.w3.org/ns/activitystreams#Public"
	for _, key := range []string{"to", "cc"} {
		switch v := obj[key].(type) {
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok && s == public {
					return true
				}
			}
		case string:
			if v == public {
				return true
			}
		}
	}
	return false
}

// handleFromActor derives @user@host from an actor id of the form
// https://host/users/<name>. Mastodon 4.7+ mints numeric actor ids
// (https://host/ap/users/<id>) that carry no username at all; those yield
// "" rather than a handle built from the number, and callers fall back to
// the REST account's acct.
func handleFromActor(v any, instanceHost string) string {
	switch a := v.(type) {
	case string:
		if i := strings.Index(a, "://"); i > 0 {
			rest := a[i+3:]
			if j := strings.Index(rest, "/users/"); j > 0 && !strings.Contains(rest[:j], "/") {
				return "@" + rest[j+7:] + "@" + rest[:j]
			}
		}
	case map[string]any:
		if id, ok := a["id"].(string); ok {
			return handleFromActor(id, instanceHost)
		}
	}
	return ""
}

func as2String(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

func nullableString(v any) any {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return nil
}

func shortB64(b []byte) string {
	s := base64.StdEncoding.EncodeToString(b)
	if len(s) > 12 {
		return s[:12] + "…"
	}
	return s
}

// appendProtocolSupersedes stamps supersedes_content_id onto the
// protocol_metadata JSON (P5.3): a re-anchor of an edited
// status points back at the content id it replaces.
func appendProtocolSupersedes(meta []byte, prevContentID uint64) []byte {
	var m map[string]any
	if err := json.Unmarshal(meta, &m); err != nil || m == nil {
		m = map[string]any{}
	}
	m["supersedes_content_id"] = strconv.FormatUint(prevContentID, 10)
	out, err := json.Marshal(m)
	if err != nil {
		return meta
	}
	return out
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
