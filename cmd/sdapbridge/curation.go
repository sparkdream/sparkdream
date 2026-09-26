package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"sparkdream/internal/sdaptx"
	"sparkdream/x/federation/types"
)

// Author curation (x/federation PeerPolicy.allowed_identities / curation):
// the chain's members decide whose content a bridge may anchor for a peer,
// the author's own consent (consent.go) is a second key on top. The chain
// enforces the gate on every submission; the bridge reads the same gate to
// (1) not spend a transaction on an author the chain would refuse, and
// (2) follow exactly the admitted authors, since it discovers posts through
// its own home timeline.

// gateTTL bounds how long a peer's gate is trusted before it is re-read.
const gateTTL = 5 * time.Minute

// authorGate is one peer's gate as the chain states it.
type authorGate struct {
	// open: "*" and no curation collection; any author is admitted.
	open bool
	// admitted: the normalized authors ("user@host") the gate lets in, when
	// not open.
	admitted map[string]bool
}

func (g authorGate) admits(handle string) bool {
	if g.open {
		return true
	}
	n, ok := types.NormalizeAuthorIdentity(handle)
	return ok && g.admitted[n]
}

type cachedGate struct {
	gate authorGate
	at   time.Time
}

// FetchAuthorGate reads a peer's gate from the chain: its policy, and when
// the policy names one, the curation collection's active link items.
func FetchAuthorGate(ctx context.Context, chain *sdaptx.Client, peerID string) (authorGate, error) {
	var pol struct {
		Policy struct {
			AllowedIdentities []string `json:"allowed_identities"`
			Curation          *struct {
				CollectionID string `json:"collection_id"`
			} `json:"curation"`
		} `json:"policy"`
	}
	if err := chain.GetJSON(ctx, "/sparkdream/federation/v1/get_peer_policy/"+url.PathEscape(peerID), &pol); err != nil {
		return authorGate{}, err
	}
	var curated []string
	if pol.Policy.Curation != nil {
		id := pol.Policy.Curation.CollectionID
		if id == "" {
			id = "0" // proto3 JSON drops a zero id
		}
		var items struct {
			Items []struct {
				Status        string `json:"status"`
				ReferenceType string `json:"reference_type"`
				Link          *struct {
					URI string `json:"uri"`
				} `json:"link"`
			} `json:"items"`
		}
		if err := chain.GetJSON(ctx, "/sparkdream/collect/v1/items/"+url.PathEscape(id)+"?pagination.limit=1000", &items); err != nil {
			return authorGate{}, fmt.Errorf("curation collection %s: %w", id, err)
		}
		for _, it := range items.Items {
			if it.Status == "ITEM_STATUS_ACTIVE" && it.ReferenceType == "REFERENCE_TYPE_LINK" && it.Link != nil {
				curated = append(curated, it.Link.URI)
			}
		}
	}
	return buildGate(pol.Policy.AllowedIdentities, pol.Policy.Curation != nil, curated), nil
}

// buildGate mirrors the chain's rule: both gates must pass.
func buildGate(allowed []string, hasCollection bool, curated []string) authorGate {
	star := false
	list := map[string]bool{}
	for _, e := range allowed {
		if e == types.AllIdentities {
			star = true
			continue
		}
		if n, ok := types.NormalizeAuthorIdentity(e); ok {
			list[n] = true
		}
	}
	if star && !hasCollection {
		return authorGate{open: true}
	}
	if !hasCollection {
		return authorGate{admitted: list}
	}
	admitted := map[string]bool{}
	for _, u := range curated {
		n, ok := types.NormalizeAuthorIdentity(u)
		if ok && (star || list[n]) {
			admitted[n] = true
		}
	}
	return authorGate{admitted: admitted}
}

// gateFor returns a peer's gate, re-read every gateTTL. ok is false when it
// has never been readable: the caller then leaves the decision to the chain.
func (b *Bridge) gateFor(ctx context.Context, peerID string) (authorGate, bool) {
	if b.gateSource == nil {
		return authorGate{}, false
	}
	if c, ok := b.gates[peerID]; ok && time.Since(c.at) < gateTTL {
		return c.gate, true
	}
	g, err := b.gateSource(ctx, peerID)
	if err != nil {
		log.Printf("sdapbridge: read the author gate of peer %s: %v", peerID, err)
		if c, ok := b.gates[peerID]; ok {
			return c.gate, true // the last good read, rather than nothing
		}
		return authorGate{}, false
	}
	if b.gates == nil {
		b.gates = map[string]cachedGate{}
	}
	b.gates[peerID] = cachedGate{gate: g, at: time.Now()}
	return g, true
}

// peerOfHandle is the configured peer an author's handle belongs to.
func (b *Bridge) peerOfHandle(handle string) string {
	for _, id := range b.peers.ids {
		if types.CreatorIdentityHostAllowed(id, b.peers.hosts[id], handle) == nil {
			return id
		}
	}
	return ""
}

// curated reports whether the chain would admit acct's content, and why not.
func (b *Bridge) curated(ctx context.Context, acct *Account) (bool, string) {
	if acct == nil {
		return true, "" // attribution-less: the consent check decides
	}
	handle := handleFromAcct(acct.Acct, b.cfg.instanceHost())
	peerID := b.peerOfHandle(handle)
	if peerID == "" {
		return true, "" // not this bridge's peer: routing skips it anyway
	}
	g, ok := b.gateFor(ctx, peerID)
	if !ok || g.admits(handle) {
		return true, ""
	}
	return false, fmt.Sprintf("not admitted by peer %s's author curation", peerID)
}

// syncFollows makes the bridge follow exactly the admitted authors of every
// curated peer: follows those it misses, unfollows those dropped from the
// list. A peer whose gate is open, or unreadable, is left alone, and so are
// follows on hosts that belong to no curated peer.
func (b *Bridge) syncFollows(ctx context.Context) error {
	self, err := b.self(ctx)
	if err != nil {
		return err
	}
	managed := map[string]authorGate{}
	for _, id := range b.peers.ids {
		if g, ok := b.gateFor(ctx, id); ok && !g.open {
			managed[id] = g
		}
	}
	if len(managed) == 0 {
		return nil
	}
	following, err := b.masto.Following(ctx, self)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, a := range following {
		handle := handleFromAcct(a.Acct, b.cfg.instanceHost())
		n, _ := types.NormalizeAuthorIdentity(handle)
		have[n] = true
		g, ok := managed[b.peerOfHandle(handle)]
		if !ok || g.admits(handle) {
			continue
		}
		if err := b.masto.Unfollow(ctx, a.ID); err != nil {
			log.Printf("sdapbridge: unfollow %s (dropped from curation): %v", handle, err)
			continue
		}
		log.Printf("sdapbridge: unfollowed %s: dropped from the author curation", handle)
	}
	for _, g := range managed {
		for author := range g.admitted {
			if have[author] {
				continue
			}
			acct := author
			if host := b.cfg.instanceHost(); strings.HasSuffix(author, "@"+strings.ToLower(host)) {
				acct = strings.TrimSuffix(author, "@"+strings.ToLower(host)) // local accounts look up by username
			}
			id, err := b.masto.LookupAccountID(ctx, acct)
			if err != nil {
				log.Printf("sdapbridge: look up curated author %s: %v", author, err)
				continue
			}
			if err := b.masto.Follow(ctx, id); err != nil {
				log.Printf("sdapbridge: follow curated author %s: %v", author, err)
				continue
			}
			log.Printf("sdapbridge: following curated author %s", author)
		}
	}
	return nil
}
