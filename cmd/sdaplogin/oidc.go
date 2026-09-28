package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	requestTTL     = 10 * time.Minute // the login page is open this long
	codeTTL        = 60 * time.Second // authorization code, redeemed by Mastodon at once
	tokenTTL       = 10 * time.Minute // access and id tokens: only read during the callback
	membershipTTL  = 5 * time.Minute  // the sweep's answers, cached per account
	maxAttempts    = 5                // signature tries per login page
	maxPending     = 10_000           // open login pages, bounded against floods
	maxVerifyBytes = 16 << 10
)

//go:embed page.html
var pageHTML string

var pageTmpl = template.Must(template.New("page").Parse(pageHTML))

// Server is a single-client OpenID Connect provider whose users are wallet
// keys that are members of a linked chain.
type Server struct {
	cfg    Config
	key    *rsa.PrivateKey
	kid    string
	chains *ChainList
	lcd    LCD
	now    func() time.Time

	mu         sync.Mutex
	requests   map[string]*loginRequest
	codes      map[string]*grant
	tokens     map[string]*grant
	membership map[string]membershipEntry
}

// loginRequest is one open login page: the client's authorize parameters
// and the challenge the wallet must sign.
type loginRequest struct {
	state, nonce, codeChallenge string
	challenge                   string
	expires                     time.Time
	attempts                    int
}

// grant is who signed in: behind an authorization code, then an access token.
type grant struct {
	sub, name, nonce string
	codeChallenge    string
	authTime         time.Time
	expires          time.Time
}

type membershipEntry struct {
	status  Membership
	expires time.Time
}

func NewServer(cfg Config, key *rsa.PrivateKey, chains *ChainList, client *http.Client) *Server {
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	sum := sha256.Sum256(der)
	return &Server{
		cfg:        cfg,
		key:        key,
		kid:        base64.RawURLEncoding.EncodeToString(sum[:12]),
		chains:     chains,
		lcd:        LCD{client: client},
		now:        time.Now,
		requests:   map[string]*loginRequest{},
		codes:      map[string]*grant{},
		tokens:     map[string]*grant{},
		membership: map[string]membershipEntry{},
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", s.discovery)
	mux.HandleFunc("GET /jwks", s.jwks)
	mux.HandleFunc("GET /authorize", s.authorize)
	mux.HandleFunc("POST /authorize/verify", s.verify)
	mux.HandleFunc("POST /token", s.token)
	mux.HandleFunc("GET /userinfo", s.userinfo)
	mux.HandleFunc("POST /userinfo", s.userinfo)
	mux.HandleFunc("GET /membership/{sub}", s.membershipStatus)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok") })
	return mux
}

// Sweep drops expired requests, codes, tokens and cached answers.
func (s *Server) Sweep() {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.requests {
		if now.After(v.expires) {
			delete(s.requests, k)
		}
	}
	for _, m := range []map[string]*grant{s.codes, s.tokens} {
		for k, v := range m {
			if now.After(v.expires) {
				delete(m, k)
			}
		}
	}
	for k, v := range s.membership {
		if now.After(v.expires) {
			delete(s.membership, k)
		}
	}
}

// ---------------------------------------------------------------------------
// discovery

func (s *Server) discovery(w http.ResponseWriter, _ *http.Request) {
	iss := s.cfg.Issuer
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                iss,
		"authorization_endpoint":                iss + "/authorize",
		"token_endpoint":                        iss + "/token",
		"userinfo_endpoint":                     iss + "/userinfo",
		"jwks_uri":                              iss + "/jwks",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"claims_supported":                      []string{"sub", "preferred_username", "nickname", "name", "email", "email_verified"},
		"code_challenge_methods_supported":      []string{"S256"},
	})
}

func (s *Server) jwks(w http.ResponseWriter, _ *http.Request) {
	pub := s.key.PublicKey
	writeJSON(w, http.StatusOK, map[string]any{"keys": []map[string]string{{
		"kty": "RSA",
		"use": "sig",
		"alg": "RS256",
		"kid": s.kid,
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}}})
}

// ---------------------------------------------------------------------------
// authorize: the login page, then the signed challenge

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	// a bad client or redirect is shown here, never redirected to
	if q.Get("client_id") != s.cfg.ClientID || q.Get("redirect_uri") != s.cfg.RedirectURI {
		pageError(w, http.StatusBadRequest, "This sign-in link is not for this server.")
		return
	}
	if q.Get("response_type") != "code" {
		pageError(w, http.StatusBadRequest, "Unsupported response type.")
		return
	}
	if !hasScope(q.Get("scope"), "openid") {
		pageError(w, http.StatusBadRequest, "The openid scope is required.")
		return
	}
	challenge := q.Get("code_challenge")
	if challenge != "" && q.Get("code_challenge_method") != "S256" {
		pageError(w, http.StatusBadRequest, "Only S256 code challenges are supported.")
		return
	}
	chains := s.chains.Sorted()
	if len(chains) == 0 {
		pageError(w, http.StatusServiceUnavailable, "No chain is linked to this server yet.")
		return
	}

	now := s.now()
	id, nonce := randomToken(), randomToken()
	req := &loginRequest{
		state:         q.Get("state"),
		nonce:         q.Get("nonce"),
		codeChallenge: challenge,
		challenge:     challengeText(s.cfg.RedirectURI, nonce, now),
		expires:       now.Add(requestTTL),
	}
	s.mu.Lock()
	if len(s.requests) >= maxPending {
		s.mu.Unlock()
		pageError(w, http.StatusServiceUnavailable, "Too many sign-ins in progress. Try again in a few minutes.")
		return
	}
	s.requests[id] = req
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	err := pageTmpl.Execute(w, map[string]any{
		"Host": hostOf(s.cfg.RedirectURI),
		"Page": map[string]any{
			"request":   id,
			"challenge": req.challenge,
			"chains":    chains,
			"verifyUrl": s.cfg.Issuer + "/authorize/verify",
		},
	})
	if err != nil {
		log.Printf("sdaplogin: render page: %v", err)
	}
}

// challengeText is what the member's wallet shows and signs.
func challengeText(redirectURI, nonce string, now time.Time) string {
	return fmt.Sprintf(
		"Sign in to %s as a Spark Dream member.\n\n"+
			"This proves you hold this account. It is not a transaction and costs nothing.\n\n"+
			"Nonce: %s\nIssued: %s",
		hostOf(redirectURI), nonce, now.UTC().Format(time.RFC3339),
	)
}

type verifyRequest struct {
	Request   string          `json:"request"`
	Chain     string          `json:"chain"`
	Address   string          `json:"address"`
	Signature WalletSignature `json:"signature"`
}

func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	var in verifyRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxVerifyBytes)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request")
		return
	}
	now := s.now()
	s.mu.Lock()
	req, ok := s.requests[in.Request]
	if ok && now.After(req.expires) {
		delete(s.requests, in.Request)
		ok = false
	}
	if ok {
		req.attempts++
		if req.attempts > maxAttempts {
			delete(s.requests, in.Request)
			ok = false
		}
	}
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusBadRequest, "this sign-in page has expired: go back to Mastodon and start again")
		return
	}

	chain, ok := s.chains.Get(in.Chain)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown chain")
		return
	}
	addr, err := verifyADR036(chain.Bech32Prefix, in.Address, req.challenge, in.Signature)
	if err != nil {
		writeError(w, http.StatusForbidden, "signature rejected: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	switch err := s.lcd.CheckMember(ctx, chain, addr); {
	case errors.Is(err, errNotMember):
		writeError(w, http.StatusForbidden, notMemberMessage(chain))
		return
	case err != nil:
		log.Printf("sdaplogin: member check on %s: %v", chain.ChainID, err)
		writeError(w, http.StatusBadGateway, "could not reach "+chain.ChainName+": try again shortly")
		return
	}
	name, err := s.lcd.PrimaryName(ctx, chain, addr)
	if err != nil {
		log.Printf("sdaplogin: name lookup on %s: %v", chain.ChainID, err)
		writeError(w, http.StatusBadGateway, "could not reach "+chain.ChainName+": try again shortly")
		return
	}
	if name == "" {
		writeError(w, http.StatusForbidden,
			"your account has no primary name on "+chain.ChainName+": register one (x/name) and set it as primary, then sign in again. It becomes your handle here.")
		return
	}

	code := randomToken()
	s.mu.Lock()
	delete(s.requests, in.Request)
	s.codes[code] = &grant{
		sub:           hex.EncodeToString(addr),
		name:          name,
		nonce:         req.nonce,
		codeChallenge: req.codeChallenge,
		authTime:      now,
		expires:       now.Add(codeTTL),
	}
	s.mu.Unlock()

	back, _ := url.Parse(s.cfg.RedirectURI)
	v := back.Query()
	v.Set("code", code)
	if req.state != "" {
		v.Set("state", req.state)
	}
	back.RawQuery = v.Encode()
	log.Printf("sdaplogin: %s signed in through %s as %s", in.Address, chain.ChainID, name)
	writeJSON(w, http.StatusOK, map[string]string{"redirect": back.String()})
}

func notMemberMessage(c Chain) string {
	if c.floor() == "TRUST_LEVEL_NEW" {
		return "this account is not an active member of " + c.ChainName
	}
	level := strings.ToLower(strings.TrimPrefix(c.floor(), "TRUST_LEVEL_"))
	return "this account is not an active member of " + c.ChainName + " at trust level " + level + " or above"
}

// ---------------------------------------------------------------------------
// token and userinfo

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		tokenError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	id, secret, basic := r.BasicAuth()
	if basic {
		// RFC 6749 2.3.1: basic credentials are form-urlencoded
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != s.cfg.ClientID || subtle.ConstantTimeCompare([]byte(secret), []byte(s.cfg.ClientSecret)) != 1 {
		tokenError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	if r.PostForm.Get("grant_type") != "authorization_code" {
		tokenError(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}
	code := r.PostForm.Get("code")
	now := s.now()
	s.mu.Lock()
	g, ok := s.codes[code]
	delete(s.codes, code) // single use, even when the exchange fails
	s.mu.Unlock()
	if !ok || now.After(g.expires) || r.PostForm.Get("redirect_uri") != s.cfg.RedirectURI {
		tokenError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	if g.codeChallenge != "" && !pkceMatches(g.codeChallenge, r.PostForm.Get("code_verifier")) {
		tokenError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	idToken, err := s.signIDToken(g, now)
	if err != nil {
		log.Printf("sdaplogin: sign id token: %v", err)
		tokenError(w, http.StatusInternalServerError, "server_error")
		return
	}
	access := randomToken()
	s.mu.Lock()
	s.tokens[access] = &grant{sub: g.sub, name: g.name, authTime: g.authTime, expires: now.Add(tokenTTL)}
	s.mu.Unlock()
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": access,
		"token_type":   "Bearer",
		"expires_in":   int(tokenTTL.Seconds()),
		"id_token":     idToken,
	})
}

func pkceMatches(challenge, verifier string) bool {
	if verifier == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

func (s *Server) userinfo(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	tok, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		writeError(w, http.StatusUnauthorized, "invalid_token")
		return
	}
	s.mu.Lock()
	g, found := s.tokens[tok]
	s.mu.Unlock()
	if !found || s.now().After(g.expires) {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		writeError(w, http.StatusUnauthorized, "invalid_token")
		return
	}
	writeJSON(w, http.StatusOK, s.claims(g))
}

// claims describe the member to Mastodon. preferred_username (omniauth's
// info.nickname) is the x/name the image's initializer makes the handle.
// The email is a placeholder at a reserved domain: without one Mastodon
// sends the member to a page that asks for an address and mails a
// confirmation to it. External accounts skip the MX check, and nothing is
// ever delivered there.
func (s *Server) claims(g *grant) map[string]any {
	return map[string]any{
		"sub":                g.sub,
		"preferred_username": g.name,
		"nickname":           g.name,
		"name":               g.name,
		"email":              g.sub + "@" + s.cfg.EmailDomain,
		"email_verified":     true,
	}
}

func (s *Server) signIDToken(g *grant, now time.Time) (string, error) {
	claims := s.claims(g)
	claims["iss"] = s.cfg.Issuer
	claims["aud"] = s.cfg.ClientID
	claims["iat"] = now.Unix()
	claims["exp"] = now.Add(tokenTTL).Unix()
	claims["auth_time"] = g.authTime.Unix()
	if g.nonce != "" {
		claims["nonce"] = g.nonce
	}
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": s.kid})
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// ---------------------------------------------------------------------------
// membership, for the instance's sweep

func (s *Server) membershipStatus(w http.ResponseWriter, r *http.Request) {
	sub := strings.ToLower(r.PathValue("sub"))
	addr, err := hex.DecodeString(sub)
	if err != nil || len(addr) != 20 {
		writeError(w, http.StatusBadRequest, "expected a 20-byte hex address")
		return
	}
	now := s.now()
	s.mu.Lock()
	cached, ok := s.membership[sub]
	s.mu.Unlock()
	if ok && now.Before(cached.expires) {
		writeJSON(w, http.StatusOK, map[string]string{"status": string(cached.status)})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	status := s.lcd.Membership(ctx, s.chains.All(), addr)
	if status != MembershipUnknown {
		s.mu.Lock()
		s.membership[sub] = membershipEntry{status: status, expires: now.Add(membershipTTL)}
		s.mu.Unlock()
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": string(status)})
}

// ---------------------------------------------------------------------------
// helpers

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hasScope(scope, want string) bool {
	for _, s := range strings.Fields(scope) {
		if s == want {
			return true
		}
	}
	return false
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Host
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func tokenError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func pageError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = pageTmpl.Execute(w, map[string]any{"Error": msg})
}
