package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/types/bech32"
)

// Chain is one chain whose members may sign in: an entry of the map the
// launcher stores in the Mastodon instance (Setting sparkdream_login_chains,
// served at /sparkdream/login-chains.json) and keys by the chain fleet's
// launch id. The wallet fields are what the login page needs to suggest the
// chain to Keplr; Rest is also where membership is read.
type Chain struct {
	ChainID       string  `json:"chainId"`
	ChainName     string  `json:"chainName"`
	RPC           string  `json:"rpc"`
	Rest          string  `json:"rest"`
	Bech32Prefix  string  `json:"bech32Prefix"`
	Denom         string  `json:"denom"`
	DisplayDenom  string  `json:"displayDenom"`
	Decimals      int     `json:"decimals"`
	GasPrice      float64 `json:"gasPrice"`
	MinTrustLevel string  `json:"minTrustLevel,omitempty"`
}

func (c Chain) usable() bool {
	return c.ChainID != "" && c.Rest != "" && c.Bech32Prefix != ""
}

// trustLevels orders x/rep's TrustLevel enum (sparkdream.rep.v1.TrustLevel).
var trustLevels = map[string]int{
	"TRUST_LEVEL_NEW":         0,
	"TRUST_LEVEL_PROVISIONAL": 1,
	"TRUST_LEVEL_ESTABLISHED": 2,
	"TRUST_LEVEL_TRUSTED":     3,
	"TRUST_LEVEL_CORE":        4,
}

// ChainList is the cached chain map, refreshed from the Mastodon instance.
// A failed refresh keeps the last good copy: the list changes only when a
// chain links or unlinks, and a login outage is worse than a stale entry.
type ChainList struct {
	url    string
	client *http.Client

	mu      sync.RWMutex
	chains  map[string]Chain
	fetched time.Time
}

func NewChainList(url string, client *http.Client) *ChainList {
	return &ChainList{url: url, client: client, chains: map[string]Chain{}}
}

// Refresh fetches the list once.
func (l *ChainList) Refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("chain list %s: status %d", l.url, resp.StatusCode)
	}
	var raw map[string]Chain
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&raw); err != nil {
		return fmt.Errorf("chain list %s: %w", l.url, err)
	}
	chains := map[string]Chain{}
	for id, c := range raw {
		if c.usable() {
			c.Rest = strings.TrimRight(c.Rest, "/")
			chains[id] = c
		}
	}
	l.mu.Lock()
	l.chains, l.fetched = chains, time.Now()
	l.mu.Unlock()
	return nil
}

// Run refreshes the list every interval until ctx ends.
func (l *ChainList) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := l.Refresh(ctx); err != nil {
				log.Printf("sdaplogin: chain list refresh: %v (keeping %d cached)", err, len(l.All()))
			}
		}
	}
}

// All returns the chains keyed by id.
func (l *ChainList) All() map[string]Chain {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make(map[string]Chain, len(l.chains))
	for k, v := range l.chains {
		out[k] = v
	}
	return out
}

// Get returns one chain by its key.
func (l *ChainList) Get(id string) (Chain, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	c, ok := l.chains[id]
	return c, ok
}

// Sorted returns the chains ordered by name, for the login page.
func (l *ChainList) Sorted() []keyedChain {
	all := l.All()
	out := make([]keyedChain, 0, len(all))
	for id, c := range all {
		out = append(out, keyedChain{ID: id, Chain: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ChainName != out[j].ChainName {
			return out[i].ChainName < out[j].ChainName
		}
		return out[i].ID < out[j].ID
	})
	return out
}

type keyedChain struct {
	ID string `json:"id"`
	Chain
}

// errNotMember is a definitive answer: the chain has no active member at
// the address with enough trust.
var errNotMember = errors.New("not a member")

// LCD reads membership and names from a chain's REST endpoint.
type LCD struct {
	client *http.Client
}

// lcdGet GETs a path on the chain's LCD. found is false on a definitive
// NotFound (HTTP 404, or gRPC code 5 in the body).
func (q LCD) lcdGet(ctx context.Context, c Chain, path string, out any) (found bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Rest+path, nil)
	if err != nil {
		return false, err
	}
	resp, err := q.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		var st struct {
			Code int `json:"code"`
		}
		if json.Unmarshal(body, &st) == nil && st.Code == 5 {
			return false, nil
		}
		return false, fmt.Errorf("%s%s: status %d", c.Rest, path, resp.StatusCode)
	}
	return true, json.Unmarshal(body, out)
}

// enumName reads a proto enum from LCD JSON, which renders enums as their
// names but may carry a number; a field proto3 left out is the zero value.
func enumName(raw json.RawMessage, names map[string]int) (int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		v, ok := names[s]
		if !ok {
			return 0, fmt.Errorf("unknown enum value %q", s)
		}
		return v, nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, fmt.Errorf("enum value %s", raw)
	}
	return n, nil
}

var memberStatuses = map[string]int{
	"MEMBER_STATUS_ACTIVE":   0,
	"MEMBER_STATUS_INACTIVE": 1,
	"MEMBER_STATUS_ZEROED":   2,
}

// CheckMember returns nil when addr is an ACTIVE x/rep member of the chain
// at or above its trust floor, errNotMember on a definitive no, and any
// other error when the chain could not be asked.
func (q LCD) CheckMember(ctx context.Context, c Chain, addr []byte) error {
	bech, err := bech32.ConvertAndEncode(c.Bech32Prefix, addr)
	if err != nil {
		return err
	}
	var resp struct {
		Member struct {
			Status     json.RawMessage `json:"status"`
			TrustLevel json.RawMessage `json:"trust_level"`
		} `json:"member"`
	}
	found, err := q.lcdGet(ctx, c, "/sparkdream/rep/v1/member/"+url.PathEscape(bech), &resp)
	if err != nil {
		return err
	}
	if !found {
		return errNotMember
	}
	status, err := enumName(resp.Member.Status, memberStatuses)
	if err != nil {
		return err
	}
	if status != memberStatuses["MEMBER_STATUS_ACTIVE"] {
		return errNotMember
	}
	trust, err := enumName(resp.Member.TrustLevel, trustLevels)
	if err != nil {
		return err
	}
	if trust < trustLevels[c.floor()] {
		return errNotMember
	}
	return nil
}

func (c Chain) floor() string {
	if _, ok := trustLevels[c.MinTrustLevel]; ok {
		return c.MinTrustLevel
	}
	return "TRUST_LEVEL_NEW"
}

// PrimaryName returns addr's primary x/name on the chain, "" when it has none.
func (q LCD) PrimaryName(ctx context.Context, c Chain, addr []byte) (string, error) {
	bech, err := bech32.ConvertAndEncode(c.Bech32Prefix, addr)
	if err != nil {
		return "", err
	}
	var resp struct {
		Name string `json:"name"`
	}
	found, err := q.lcdGet(ctx, c, "/sparkdream/name/v1/reverse_resolve/"+url.PathEscape(bech), &resp)
	if err != nil || !found {
		return "", err
	}
	return resp.Name, nil
}

// Membership is the sweep's answer for one account.
type Membership string

const (
	MembershipActive   Membership = "active"
	MembershipInactive Membership = "inactive"
	// MembershipUnknown: some chain could not be asked, or there are no
	// chains. The sweep leaves the account alone.
	MembershipUnknown Membership = "unknown"
)

// Membership asks every chain: active on any one is enough, and inactive
// needs a definitive no from all of them.
func (q LCD) Membership(ctx context.Context, chains map[string]Chain, addr []byte) Membership {
	if len(chains) == 0 {
		return MembershipUnknown
	}
	unknown := false
	for id, c := range chains {
		err := q.CheckMember(ctx, c, addr)
		switch {
		case err == nil:
			return MembershipActive
		case errors.Is(err, errNotMember):
		default:
			log.Printf("sdaplogin: membership on %s (%s): %v", id, c.ChainID, err)
			unknown = true
		}
	}
	if unknown {
		return MembershipUnknown
	}
	return MembershipInactive
}
