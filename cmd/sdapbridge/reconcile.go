package main

import (
	"context"
	"errors"
	"log"
	"strconv"
	"time"
)

// maxReconcilePages caps how far one sweep pages through a single
// account (40 statuses per page).
const maxReconcilePages = 5

// reconcileOnce covers what home-timeline discovery structurally cannot
// see. Mastodon's home feed drops a reply unless the reader follows the
// account being replied to (FeedManager#filter_from_home), so a followed
// author's public reply to anyone the bridge does not follow never reaches
// the timeline — valid blog_reply content, missed every time. This sweep
// reads each followed account's recent public posts directly and anchors
// whatever the timeline did not deliver.
//
// Missed replies are expected. A missed top-level post is not: the home
// feed should have carried it, so that count is logged as a warning. (An
// idle account is not the usual cause: token use refreshes the sign-in at
// most daily, and a lapsed account's feed is rebuilt on its next request.
// The one real gap is a follow made before the token was ever used, whose
// history the follow-time merge skips; the outbox backfill covers that.)
//
// Cost per sweep: one follow-list walk plus one or more requests per
// followed account, all against the bridge's own instance.
func (b *Bridge) reconcileOnce(ctx context.Context) error {
	self, err := b.self(ctx)
	if err != nil {
		return err
	}
	follows, err := b.masto.Following(ctx, self)
	if err != nil {
		return err
	}

	// Look back ReconcileLookback, but never before this process started if
	// the home timeline has not moved at all: a first run's history is the
	// outbox backfill's job, not this sweep's.
	floor := time.Now().Add(-b.cfg.ReconcileLookback)
	if b.state.LastSeenStatusID() == "" && b.startedAt.After(floor) {
		floor = b.startedAt
	}
	// Leave the newest posts to the timeline; this sweep is for what it
	// missed, not a race with the next poll.
	settle := time.Now().Add(-2 * b.cfg.PollInterval)

	missedReplies, missedPosts := 0, 0
	for i := range follows {
		acct := &follows[i]
		cursor := snowflakeAt(floor)
		for page := 0; page < maxReconcilePages; page++ {
			statuses, err := b.masto.AccountStatuses(ctx, acct.ID, cursor)
			if err != nil {
				return err
			}
			for j := range statuses {
				st := &statuses[j]
				cursor = st.ID
				if !b.reconcileCandidate(ctx, st, settle) {
					continue
				}
				if st.InReplyToID != nil {
					missedReplies++
				} else {
					missedPosts++
				}
				if err := b.IngestStatus(ctx, st); err != nil {
					var rl *RateLimitError
					if errors.As(err, &rl) {
						return err
					}
					log.Printf("sdapbridge: reconcile ingest %s: %v", st.URI, err)
				}
			}
			if len(statuses) < 40 {
				break
			}
		}
	}
	if missedReplies > 0 {
		log.Printf("sdapbridge: reconcile: %d reply(ies) to accounts the bridge does not follow, anchored from the account feeds",
			missedReplies)
	}
	if missedPosts > 0 {
		log.Printf("sdapbridge: WARNING reconcile: %d top-level public post(s) from followed accounts never reached the "+
			"home timeline. Check the bridge account on its instance: muted or filtered accounts, or a feed "+
			"still regenerating after a long outage.", missedPosts)
	}
	return b.state.Save()
}

// reconcileCandidate reports whether st is a post the home timeline should
// have delivered and the bridge may and has not anchored: public or
// unlisted, not a boost, on a configured peer's host, from a consenting
// author after their consent, settled, and not already seen.
func (b *Bridge) reconcileCandidate(ctx context.Context, st *Status, settle time.Time) bool {
	if st.Reblog != nil || (st.Visibility != "public" && st.Visibility != "unlisted") {
		return false
	}
	if _, err := b.peers.route(st.URI); err != nil {
		return false
	}
	// Count only what the bridge may anchor: a refused author, or a post
	// from before their consent, is not something the timeline "missed".
	if !b.allowed(ctx, &st.Account) || b.publishedBeforeConsent(&st.Account, st.CreatedAt) {
		return false
	}
	if created, err := time.Parse(time.RFC3339, st.CreatedAt); err == nil && created.After(settle) {
		return false
	}
	_, seen := b.state.LatestFor(st.URI)
	return !seen
}

// snowflakeAt is the smallest Mastodon status id minted at t. Mastodon ids
// are (unix milliseconds << 16) | sequence, for remote statuses too (the
// id comes from the post's created_at), so this turns a time into a
// min_id cursor.
func snowflakeAt(t time.Time) string {
	return strconv.FormatUint(uint64(t.UnixMilli())<<16, 10)
}
