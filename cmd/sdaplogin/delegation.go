package main

import (
	"context"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/types/bech32"
)

// Delegated sign-in: a member grants another key of theirs an x/session
// SIGN_IN grant naming this instance's host, and that key may then sign in to
// the member's account. The chain records the permission; this service
// enforces it at each sign-in, and the instance's sweep asks
// /sign-in-grant/... to end sessions whose grant is gone.

const (
	signInGrantType   = "GRANT_TYPE_SIGN_IN"
	grantStatusActive = "GRANT_STATUS_ACTIVE"
	maxAccountChoices = 20 // x/session's per-granter cap bounds one granter, not a grantee
	// The REST gateway parses an enum query parameter as its number only:
	// "?type=GRANT_TYPE_SIGN_IN" is refused with code 3.
	signInGrantTypeNum = "5"
	// Anyone can create grants naming any key, so one page of grants is not
	// all of them: follow next_key. x/session caps a grantee at 256 grants
	// and a page at 100, so three pages always suffice; the bound only stops
	// a misbehaving node from looping us.
	maxSignInGrantPages = 4
)

// signInGrant is the part of an x/session Grant this service reads.
type signInGrant struct {
	ID        string    `json:"id"`
	Granter   string    `json:"granter"`
	Grantee   string    `json:"grantee"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
	SignIn    *struct {
		Service string `json:"service"`
	} `json:"sign_in"`
}

// honours reports whether g lets grantee sign in to service as g.Granter now.
func (g signInGrant) honours(service, grantee string, now time.Time) bool {
	return g.Type == signInGrantType && g.Status == grantStatusActive &&
		g.SignIn != nil && g.SignIn.Service == service &&
		g.Grantee == grantee && now.Before(g.ExpiresAt)
}

// SignInGrantsTo lists the SIGN_IN grants made to grantee on the chain,
// every page of them. A chain running a release without the grant type
// refuses the type filter; that is an error here, which callers treat as
// "none".
func (q LCD) SignInGrantsTo(ctx context.Context, c Chain, grantee string) ([]signInGrant, error) {
	var grants []signInGrant
	nextKey := ""
	for page := 0; page < maxSignInGrantPages; page++ {
		var resp struct {
			Grants     []signInGrant `json:"grants"`
			Pagination *struct {
				NextKey string `json:"next_key"`
			} `json:"pagination"`
		}
		path := "/sparkdream/session/v1/grants/by_grantee/" + url.PathEscape(grantee) + "?type=" + signInGrantTypeNum
		if nextKey != "" {
			path += "&pagination.key=" + url.QueryEscape(nextKey)
		}
		if _, err := q.lcdGet(ctx, c, path, &resp); err != nil {
			return nil, err
		}
		grants = append(grants, resp.Grants...)
		if resp.Pagination == nil || resp.Pagination.NextKey == "" {
			return grants, nil
		}
		nextKey = resp.Pagination.NextKey
	}
	return nil, errors.New("sign-in grants: too many pages")
}

// Grant reads one grant by id; found is false once it is revoked, declined
// or expired (x/session deletes ended grants).
func (q LCD) Grant(ctx context.Context, c Chain, id string) (g signInGrant, found bool, err error) {
	var resp struct {
		Grant signInGrant `json:"grant"`
	}
	found, err = q.lcdGet(ctx, c, "/sparkdream/session/v1/grants/"+url.PathEscape(id), &resp)
	return resp.Grant, found, err
}

// service is the host grants must name: the Mastodon instance this
// provider signs in to.
func (s *Server) service() string { return hostOf(s.cfg.RedirectURI) }

// signedText is what the wallet signs: the page's challenge, plus the
// account chosen when it is not the signer's own, so the wallet shows whose
// account the key is about to enter and the choice cannot be swapped later.
func signedText(challenge, as string) string {
	if as == "" {
		return challenge
	}
	return challenge + "\n\nSigning in as: " + as
}

type accountChoice struct {
	Address string `json:"address"`
	Name    string `json:"name"`
}

// accounts lists the members whose accounts the given key may sign in to
// through a SIGN_IN grant, for the login page to offer next to the key's own.
// It reads only public chain data, but needs an open login page so it is not
// a free relay to the chain's LCD.
func (s *Server) accounts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.mu.Lock()
	req, ok := s.requests[q.Get("request")]
	ok = ok && s.now().Before(req.expires)
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusBadRequest, "this sign-in page has expired: go back to Mastodon and start again")
		return
	}
	chain, ok := s.chains.Get(q.Get("chain"))
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown chain")
		return
	}
	grantee := q.Get("address")
	if hrp, _, err := bech32.DecodeAndConvert(grantee); err != nil || hrp != chain.Bech32Prefix {
		writeError(w, http.StatusBadRequest, "not an address on "+chain.ChainName)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	out := []accountChoice{}
	grants, err := s.lcd.SignInGrantsTo(ctx, chain, grantee)
	if err != nil {
		// offering only the key's own account is the safe fallback
		log.Printf("sdaplogin: sign-in grants to %s on %s: %v", grantee, chain.ChainID, err)
		writeJSON(w, http.StatusOK, map[string]any{"accounts": out})
		return
	}
	now, seen := s.now(), map[string]bool{}
	for _, g := range grants {
		if len(out) >= maxAccountChoices {
			break
		}
		if !g.honours(s.service(), grantee, now) || seen[g.Granter] {
			continue
		}
		seen[g.Granter] = true
		_, addr, err := bech32.DecodeAndConvert(g.Granter)
		if err != nil {
			continue
		}
		name, err := s.lcd.PrimaryName(ctx, chain, addr)
		if err != nil || name == "" {
			continue // no handle to sign in as; verify would refuse it too
		}
		out = append(out, accountChoice{Address: g.Granter, Name: name})
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": out})
}

// errNoGrant: the chain answered and holds no grant that lets the signer in.
var errNoGrant = errors.New("no sign-in grant")

// delegatedGrant finds the grant letting grantee sign in to this service as
// granter, both bech32 on the chain.
func (s *Server) delegatedGrant(ctx context.Context, c Chain, granter, grantee string) (signInGrant, error) {
	grants, err := s.lcd.SignInGrantsTo(ctx, c, grantee)
	if err != nil {
		return signInGrant{}, err
	}
	now := s.now()
	for _, g := range grants {
		if g.Granter == granter && g.honours(s.service(), grantee, now) {
			return g, nil
		}
	}
	return signInGrant{}, errNoGrant
}

// signInGrantStatus answers the instance's sweep for one delegated session:
// "active" while the grant it was opened through still lets its grantee in
// as the account's owner, "inactive" on a definitive no, "unknown" when the
// chain could not say (the sweep then leaves the session alone).
//
//	GET /sign-in-grant/{chain}/{id}?granter=<hex>&grantee=<bech32>
func (s *Server) signInGrantStatus(w http.ResponseWriter, r *http.Request) {
	chain, ok := s.chains.Get(r.PathValue("chain"))
	if !ok {
		// an unlinked chain's grants no longer let anyone in here
		writeJSON(w, http.StatusOK, map[string]string{"status": "inactive"})
		return
	}
	id := r.PathValue("id")
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		writeError(w, http.StatusBadRequest, "grant id must be a number")
		return
	}
	granterHex := strings.ToLower(r.URL.Query().Get("granter"))
	granterBytes, err := hex.DecodeString(granterHex)
	if err != nil || len(granterBytes) != 20 {
		writeError(w, http.StatusBadRequest, "granter must be a 20-byte hex address")
		return
	}
	granter, err := bech32.ConvertAndEncode(chain.Bech32Prefix, granterBytes)
	if err != nil {
		writeError(w, http.StatusBadRequest, "granter must be a 20-byte hex address")
		return
	}
	grantee := r.URL.Query().Get("grantee")

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	g, found, err := s.lcd.Grant(ctx, chain, id)
	switch {
	case err != nil:
		log.Printf("sdaplogin: grant %s on %s: %v", id, chain.ChainID, err)
		writeJSON(w, http.StatusOK, map[string]string{"status": "unknown"})
	case found && g.Granter == granter && g.honours(s.service(), grantee, s.now()):
		writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
	default:
		writeJSON(w, http.StatusOK, map[string]string{"status": "inactive"})
	}
}
