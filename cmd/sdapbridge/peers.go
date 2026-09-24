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

// contentHostsTTL bounds how stale the cached content_hosts can be after
// an OpsComm policy change.
const contentHostsTTL = time.Hour

// peerSet is the peers one bridge process anchors for. The bridge account
// follows people wherever they are; each source instance is its own chain
// peer, and one operator may hold bindings on many of them under a single
// bond. Every post is routed to the peer that owns its host by the same
// provenance rule the chain enforces, and skipped when no configured peer
// does.
type peerSet struct {
	ids []string
	// source reads one peer's content_hosts from the chain; nil in tests,
	// where each peer's own host is all it owns.
	source func(ctx context.Context, peerID string) ([]string, error)
	hosts  map[string][]string
	at     time.Time
}

func newPeerSet(ids []string, source func(context.Context, string) ([]string, error)) *peerSet {
	return &peerSet{ids: ids, source: source, hosts: map[string][]string{}}
}

// refresh re-reads every peer's content_hosts once per contentHostsTTL. A
// failed read keeps that peer's previous list.
func (p *peerSet) refresh(ctx context.Context) {
	if p.source == nil || time.Since(p.at) < contentHostsTTL {
		return
	}
	for _, id := range p.ids {
		hosts, err := p.source(ctx, id)
		if err != nil {
			log.Printf("sdapbridge: read content_hosts for peer %s (keeping %v): %v", id, p.hosts[id], err)
			continue
		}
		p.hosts[id] = hosts
	}
	p.at = time.Now()
}

// route returns the configured peer that owns uri's host.
func (p *peerSet) route(uri string) (string, error) {
	for _, id := range p.ids {
		if types.ContentURIHostAllowed(id, p.hosts[id], uri) == nil {
			return id, nil
		}
	}
	return "", fmt.Errorf("%s belongs to none of the configured peers %v", uri, p.ids)
}

// FetchContentHosts reads PeerPolicy.content_hosts over the LCD.
func FetchContentHosts(ctx context.Context, chain *sdaptx.Client, peerID string) ([]string, error) {
	var out struct {
		Policy struct {
			ContentHosts []string `json:"content_hosts"`
		} `json:"policy"`
	}
	if err := chain.GetJSON(ctx, "/sparkdream/federation/v1/get_peer_policy/"+url.PathEscape(peerID), &out); err != nil {
		return nil, err
	}
	return out.Policy.ContentHosts, nil
}

// BoundPeers keeps the configured peers this operator holds a bridge
// binding on. Submitting for any other peer fails with ErrBridgeNotFound
// on every post, so a peer listed by mistake is dropped at startup rather
// than discovered one rejected transaction at a time.
func BoundPeers(ctx context.Context, chain *sdaptx.Client, operator string, ids []string) []string {
	var bound []string
	for _, id := range ids {
		var out struct {
			Binding struct {
				PeerID    string `json:"peer_id"`
				Suspended bool   `json:"suspended"`
			} `json:"bridge_binding"`
		}
		path := "/sparkdream/federation/v1/get_bridge_binding/" + url.PathEscape(operator) + "/" + url.PathEscape(id)
		if err := chain.GetJSON(ctx, path, &out); err != nil || out.Binding.PeerID != id {
			log.Printf("sdapbridge: dropping peer %s: operator %s holds no bridge binding on it (%v)", id, operator, err)
			continue
		}
		if out.Binding.Suspended {
			log.Printf("sdapbridge: peer %s: binding is suspended (underfunded bond); submissions will fail until it is topped up", id)
		}
		bound = append(bound, id)
	}
	return bound
}

// splitList parses a comma-separated env value, dropping blanks.
func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
