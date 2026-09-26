package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"time"
)

// ConsentMode decides whose posts the bridge may anchor. Anchoring puts a
// post on a public chain, a bigger step than federating it, and fediverse
// norms around bridging are opt-in; every mode also honours the #nobridge
// and #nobot profile conventions.
type ConsentMode string

const (
	// ConsentOptIn: the author follows the bridge account. Explicit, and
	// revocable by unfollowing. The default.
	ConsentOptIn ConsentMode = "opt-in"
	// ConsentIndexable: the author has Mastodon's "indexable" setting on
	// (public posts may be included in search), the closest standard
	// signal for machine processing of their posts.
	ConsentIndexable ConsentMode = "indexable"
	// ConsentNone anchors every followed author. Test instances only;
	// logged loudly at startup.
	ConsentNone ConsentMode = "none"
)

func (m ConsentMode) valid() bool {
	return m == ConsentOptIn || m == ConsentIndexable || m == ConsentNone
}

// followersTTL bounds how long an unfollow (consent withdrawn) takes to
// stop anchoring in opt-in mode.
const followersTTL = 10 * time.Minute

// refusalLogEvery rate-limits the per-author refusal log line.
const refusalLogEvery = 6 * time.Hour

// optOutTag matches #nobridge / #nobot as whole hashtags.
var optOutTag = regexp.MustCompile(`(?i)#no(bridge|bot)\b`)

// htmlTag strips markup: Mastodon renders a hashtag in a bio as
// #<span>nobridge</span>, which a plain search would miss.
var htmlTag = regexp.MustCompile(`<[^>]*>`)

// optedOut reports whether the account's bio or profile fields carry a
// #nobridge or #nobot tag.
func optedOut(a *Account) bool {
	texts := []string{a.Note}
	for _, f := range a.Fields {
		texts = append(texts, f.Name, f.Value)
	}
	for _, t := range texts {
		if optOutTag.MatchString(htmlTag.ReplaceAllString(t, "")) {
			return true
		}
	}
	return false
}

// consents decides whether acct's posts may be anchored, before anything
// of theirs is fetched. The REST account comes from the bridge's own
// instance, which keeps it in sync with the author's actor. A missing
// account fails closed. A non-nil error means the answer could not be
// determined (the followers list was unreadable): not anchored now, but
// not a withdrawal of consent either.
func (b *Bridge) consents(ctx context.Context, acct *Account) (bool, string, error) {
	mode := b.cfg.Consent
	if mode == ConsentNone {
		return true, "", nil
	}
	if acct == nil || acct.URI == "" {
		return false, "no account to check consent against", nil
	}
	if optedOut(acct) {
		return false, "profile carries #nobridge or #nobot", nil
	}
	switch mode {
	case ConsentIndexable:
		if acct.Indexable == nil || !*acct.Indexable {
			return false, "account is not indexable", nil
		}
		return true, "", nil
	default: // ConsentOptIn
		followers, err := b.bridgeFollowers(ctx)
		if err != nil {
			return false, "", fmt.Errorf("could not read the bridge's followers: %w", err)
		}
		if !followers[acct.URI] {
			return false, "account does not follow the bridge (opt-in)", nil
		}
		return true, "", nil
	}
}

// consentGrace backdates a newly observed consent by the followers cache
// TTL, so posts made between the follow and the bridge noticing it are
// still covered.
const consentGrace = followersTTL

// allowed wraps consents: it records when consent was first observed,
// forgets it when consent is withdrawn, and logs each refused author at a
// limited rate.
func (b *Bridge) allowed(ctx context.Context, acct *Account) bool {
	// the community's curation first: an author the chain would refuse is
	// skipped before consent is even looked at, and without touching the
	// consent record
	if ok, reason := b.curated(ctx, acct); !ok {
		b.logRefusal(acct, reason)
		return false
	}
	ok, reason, err := b.consents(ctx, acct)
	if err != nil {
		log.Printf("sdapbridge: consent undetermined, not anchoring this cycle: %v", err)
		return false
	}
	if ok {
		if acct != nil && b.cfg.Consent != ConsentNone {
			b.state.ConsentStart(acct.URI, time.Now().Add(-consentGrace).Unix())
		}
		return true
	}
	if acct != nil {
		b.state.ConsentWithdrawn(acct.URI, time.Now().Unix())
	}
	b.logRefusal(acct, reason)
	return false
}

// logRefusal logs a refused author at a limited rate.
func (b *Bridge) logRefusal(acct *Account, reason string) {
	key := "?"
	if acct != nil {
		key = acct.URI
	}
	if b.refusedAt == nil {
		b.refusedAt = map[string]time.Time{}
	}
	if time.Since(b.refusedAt[key]) >= refusalLogEvery {
		b.refusedAt[key] = time.Now()
		log.Printf("sdapbridge: not anchoring %s: %s", key, reason)
	}
}

// publishedBeforeConsent reports whether a post predates its author's
// observed consent. Opting in covers what comes after it; the author's
// earlier posts, outbox backfill included, stay off the chain.
func (b *Bridge) publishedBeforeConsent(acct *Account, published string) bool {
	if b.cfg.Consent == ConsentNone || acct == nil {
		return false
	}
	b.state.mu.Lock()
	since, ok := b.state.ConsentSince[acct.URI]
	b.state.mu.Unlock()
	if !ok {
		return true // no observed consent at all
	}
	t, err := time.Parse(time.RFC3339, published)
	if err != nil {
		return true // cannot tell: fail closed
	}
	return t.Unix() < since
}

// bridgeFollowers returns the actor URIs following the bridge account,
// refreshed every followersTTL. A failed refresh keeps the previous set
// only while it is still fresh: stale consent must not outlive its TTL.
func (b *Bridge) bridgeFollowers(ctx context.Context) (map[string]bool, error) {
	if b.followers != nil && time.Since(b.followersAt) < followersTTL {
		return b.followers, nil
	}
	self, err := b.self(ctx)
	if err != nil {
		return nil, err
	}
	accts, err := b.masto.Followers(ctx, self)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(accts))
	for _, a := range accts {
		set[a.URI] = true
	}
	b.followers, b.followersAt = set, time.Now()
	return set, nil
}

// self returns the bridge's own Mastodon account id, cached.
func (b *Bridge) self(ctx context.Context) (string, error) {
	if b.selfID == "" {
		acct, err := b.masto.VerifyCredentials(ctx)
		if err != nil {
			return "", err
		}
		b.selfID = acct.ID
	}
	return b.selfID, nil
}
