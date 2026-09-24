package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
)

// State is the local (uri, hash) → content record. The chain's
// ContentByHash is the dedupe AUTHORITY; this cache exists so the
// daemon does not spend a tx to rediscover what it already anchored.
// Wiping it is safe (see WarmCacheFromChain).
type State struct {
	mu       sync.Mutex
	path     string
	Seen     map[string]*SeenRecord `json:"seen"`     // key: uri + "#" + b64(hash)
	Latest   map[string]string      `json:"latest"`   // uri → key into Seen (for supersedes lookups)
	Accounts map[string]bool        `json:"accounts"` // actor URIs already backfilled
	// Tracked is the edit-revisit list: uri → where to re-check it on the
	// bridge's own instance and which version was last hashed.
	Tracked map[string]*TrackedStatus `json:"tracked"`
	// ConsentSince is when consent was first observed per author actor URI
	// (unix seconds). Posts published before it are never anchored:
	// opting in covers what comes after, not the author's history.
	ConsentSince map[string]int64 `json:"consent_since"`
	// RefusedAt is when a refusal (unfollow, #nobridge) was last observed
	// per author. A later consent start never reaches back before it, so
	// posts made while opted out stay off the chain.
	RefusedAt map[string]int64 `json:"refused_at"`
	// Deferred holds posts whose anchoring failed for a retryable reason,
	// keyed by uri, so discovery can move on instead of pinning the cursor
	// that every peer shares (retry.go).
	Deferred map[string]*DeferredStatus `json:"deferred"`
	// LastSeen is the highest home-timeline status id already processed.
	// Passed as since_id so a burst larger than one page is picked up next
	// cycle instead of falling out of the window. Mastodon ids are
	// numeric-as-string and monotonically increasing per instance.
	LastSeen string `json:"last_seen_status_id"`
}

// SeenRecord is one anchored version of one remote status.
type SeenRecord struct {
	URI       string `json:"uri"`
	HashB64   string `json:"hash_b64"`
	ContentID uint64 `json:"content_id,omitempty"`
	// Operator is who anchored this version. Only the same operator may
	// name it in MsgSubmitFederatedContent.supersedes, so an edit of a post
	// another bridge anchored gets the metadata link but no chain link.
	Operator string `json:"operator,omitempty"`
	TxHash   string `json:"tx_hash,omitempty"`
	At       int64  `json:"at"`
}

// TrackedStatus is what the revisit sweep needs to notice an edit.
type TrackedStatus struct {
	// StatusID is the REST id on the bridge's OWN instance, which learns of
	// remote edits through ordinary AP Update delivery to the follower.
	StatusID string `json:"status_id"`
	// Updated is the AS2 `updated` of the version last hashed, NOT the REST
	// edited_at. Mastodon caches AS2 bodies for 3 minutes without keying on
	// updated_at, so REST can show an edit that AS2 does not serve yet;
	// recording REST's value would mark an edit handled that never was.
	Updated string `json:"updated,omitempty"`
	// Since is when this uri was last anchored; the revisit window runs
	// from here, so every re-anchor extends it.
	Since int64 `json:"since"`
}

func LoadState(path string) (*State, error) {
	s := &State{
		path:     path,
		Seen:     map[string]*SeenRecord{},
		Latest:   map[string]string{},
		Accounts: map[string]bool{},
		Tracked:  map[string]*TrackedStatus{},

		ConsentSince: map[string]int64{},
		RefusedAt:    map[string]int64{},
		Deferred:     map[string]*DeferredStatus{},
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, s); err != nil {
		return nil, fmt.Errorf("state file %s corrupt: %w", path, err)
	}
	if s.Seen == nil {
		s.Seen = map[string]*SeenRecord{}
	}
	if s.Latest == nil {
		s.Latest = map[string]string{}
	}
	if s.Accounts == nil {
		s.Accounts = map[string]bool{}
	}
	if s.Tracked == nil {
		s.Tracked = map[string]*TrackedStatus{}
	}
	if s.ConsentSince == nil {
		s.ConsentSince = map[string]int64{}
	}
	if s.RefusedAt == nil {
		s.RefusedAt = map[string]int64{}
	}
	if s.Deferred == nil {
		s.Deferred = map[string]*DeferredStatus{}
	}
	return s, nil
}

func SeenKey(uri string, hash []byte) string {
	return uri + "#" + base64.StdEncoding.EncodeToString(hash)
}

// Has reports whether (uri, hash) was already anchored.
func (s *State) Has(uri string, hash []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.Seen[SeenKey(uri, hash)]
	return ok
}

// LatestFor returns the most recent anchored version of uri, if any —
// the record an edit supersedes.
func (s *State) LatestFor(uri string) (*SeenRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.Latest[uri]
	if !ok {
		return nil, false
	}
	rec, ok := s.Seen[key]
	return rec, ok
}

// Record remembers a confirmed (uri, hash) anchor. contentID comes from
// the federated_content_received event on the included tx; it is 0 only
// when the chain reported the content as already anchored, in which case
// the next warm-up fills it in.
func (s *State) Record(uri string, hash []byte, contentID uint64, operator, txHash string, at int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := SeenKey(uri, hash)
	s.Seen[key] = &SeenRecord{URI: uri, HashB64: base64.StdEncoding.EncodeToString(hash), ContentID: contentID,
		Operator: operator, TxHash: txHash, At: at}
	s.Latest[uri] = key
}

// Track records the version of uri just hashed. An empty statusID keeps
// the one already known; since is only moved when a new version was
// anchored (anchored=true), so re-seeing an unchanged status does not keep
// it on the revisit list forever.
func (s *State) Track(uri, statusID, updated string, at int64, anchored bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.Tracked[uri]
	if !ok {
		t = &TrackedStatus{Since: at}
		s.Tracked[uri] = t
	}
	if statusID != "" {
		t.StatusID = statusID
	}
	t.Updated = updated
	if anchored {
		t.Since = at
	}
}

// ConsentStart records when consent from actor was first observed, keeping
// an existing start, and returns the start in effect. at may be backdated
// by the caller, but never before the actor's last observed refusal.
func (s *State) ConsentStart(actor string, at int64) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if since, ok := s.ConsentSince[actor]; ok {
		return since
	}
	if refused := s.RefusedAt[actor]; at < refused {
		at = refused
	}
	s.ConsentSince[actor] = at
	return at
}

// ConsentWithdrawn forgets actor's consent start and remembers when the
// refusal was seen: a later opt-in starts afresh and does not reach back
// over the gap.
func (s *State) ConsentWithdrawn(actor string, at int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ConsentSince, actor)
	s.RefusedAt[actor] = at
}

// Tracking returns the revisit record for uri.
func (s *State) Tracking(uri string) (TrackedStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.Tracked[uri]
	if !ok {
		return TrackedStatus{}, false
	}
	return *t, true
}

// Revisitable returns the tracked statuses anchored at or after cutoff
// that have a REST id to re-check by, and drops the ones that have aged
// out of the window.
func (s *State) Revisitable(cutoff int64) map[string]TrackedStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]TrackedStatus)
	for uri, t := range s.Tracked {
		if t.Since < cutoff {
			delete(s.Tracked, uri)
			continue
		}
		if t.StatusID != "" {
			out[uri] = *t
		}
	}
	return out
}

// Backfilled reports whether the actor's outbox pass already ran.
func (s *State) Backfilled(actor string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Accounts[actor]
}

func (s *State) MarkBackfilled(actor string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Accounts[actor] = true
}

// LastSeenStatusID returns the discovery cursor (empty on first run).
func (s *State) LastSeenStatusID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.LastSeen
}

// AdvanceLastSeen moves the discovery cursor forward. Mastodon status ids
// are decimal strings that grow monotonically per instance, so "larger"
// is by numeric value where both parse and lexicographic order otherwise.
// It never moves backwards: pollOnce walks oldest-first and stops at the
// first failure, so a retry must not rewind past statuses already done.
func (s *State) AdvanceLastSeen(statusID string) {
	if statusID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.LastSeen == "" || statusIDLess(s.LastSeen, statusID) {
		s.LastSeen = statusID
	}
}

func statusIDLess(a, b string) bool {
	an, aerr := strconv.ParseUint(a, 10, 64)
	bn, berr := strconv.ParseUint(b, 10, 64)
	if aerr == nil && berr == nil {
		return an < bn
	}
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

// maxSeenRecords caps the local dedupe cache. The chain's ContentByHash
// is the dedupe AUTHORITY, and WarmCacheFromChain rebuilds from it on
// boot, so the file is pure optimisation and may be trimmed freely.
// Without a cap it grew without bound for the life of the daemon.
const maxSeenRecords = 20000

// pruneLocked drops the oldest records once the cache exceeds the cap,
// keeping whatever `Latest` points at so edit chaining still works.
// Caller holds s.mu.
func (s *State) pruneLocked() {
	if len(s.Seen) <= maxSeenRecords {
		return
	}
	pinned := make(map[string]bool, len(s.Latest))
	for _, key := range s.Latest {
		pinned[key] = true
	}
	type aged struct {
		key string
		at  int64
	}
	candidates := make([]aged, 0, len(s.Seen))
	for key, rec := range s.Seen {
		if pinned[key] {
			continue
		}
		candidates = append(candidates, aged{key: key, at: rec.At})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].at < candidates[j].at })

	for _, c := range candidates {
		if len(s.Seen) <= maxSeenRecords {
			break
		}
		delete(s.Seen, c.key)
	}
}

// Save persists atomically (tmp + rename).
func (s *State) Save() error {
	s.mu.Lock()
	s.pruneLocked()
	out, err := json.MarshalIndent(s, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
