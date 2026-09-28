package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/types/bech32"
)

const (
	testRedirect = "https://social.phoenix.example/auth/auth/openid_connect/callback"
	testIssuer   = "https://login.social.phoenix.example"
	testSecret   = "s3cret"
)

// fakeLCD answers x/rep member and x/name reverse_resolve queries.
type fakeLCD struct {
	mu      sync.Mutex
	members map[string]string // bech32 -> member JSON
	names   map[string]string // bech32 -> primary name
	down    bool
}

func (f *fakeLCD) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		http.Error(w, `{"code":13,"message":"internal"}`, http.StatusInternalServerError)
		return
	}
	switch {
	case strings.HasPrefix(r.URL.Path, "/sparkdream/rep/v1/member/"):
		m, ok := f.members[strings.TrimPrefix(r.URL.Path, "/sparkdream/rep/v1/member/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"code":5,"message":"not found"}`)
			return
		}
		io.WriteString(w, `{"member":`+m+`}`)
	case strings.HasPrefix(r.URL.Path, "/sparkdream/name/v1/reverse_resolve/"):
		n, ok := f.names[strings.TrimPrefix(r.URL.Path, "/sparkdream/name/v1/reverse_resolve/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"code":5,"message":"account has no primary name set"}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"name": n})
	default:
		http.NotFound(w, r)
	}
}

type harness struct {
	srv  *Server
	http *httptest.Server
	lcd  *fakeLCD
	key  *rsa.PrivateKey
	priv *secp256k1.PrivKey
	addr string // the test member's bech32 address
}

func newHarness(t *testing.T, minTrust string) *harness {
	t.Helper()
	lcd := &fakeLCD{members: map[string]string{}, names: map[string]string{}}
	lcdSrv := httptest.NewServer(lcd)
	t.Cleanup(lcdSrv.Close)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	chains := NewChainList("unused", http.DefaultClient)
	chains.chains = map[string]Chain{"fleet-phoenix": {
		ChainID: "phoenix-1", ChainName: "Phoenix", Rest: lcdSrv.URL, RPC: "https://rpc.phoenix.example",
		Bech32Prefix: "sprkdrm", Denom: "uspark", DisplayDenom: "SPARK", Decimals: 6, MinTrustLevel: minTrust,
	}}
	cfg := Config{
		Issuer: testIssuer, ClientID: "mastodon", ClientSecret: testSecret, RedirectURI: testRedirect,
		EmailDomain: "wallet.invalid",
	}
	srv := NewServer(cfg, key, chains, http.DefaultClient)
	hs := httptest.NewServer(srv.Routes())
	t.Cleanup(hs.Close)

	sum := sha256.Sum256([]byte("sdaplogin harness member"))
	priv := &secp256k1.PrivKey{Key: sum[:]}
	addr, _ := bech32.ConvertAndEncode("sprkdrm", priv.PubKey().Address())
	return &harness{srv: srv, http: hs, lcd: lcd, key: key, priv: priv, addr: addr}
}

func (h *harness) member(json, name string) {
	h.lcd.mu.Lock()
	defer h.lcd.mu.Unlock()
	h.lcd.members[h.addr] = json
	if name != "" {
		h.lcd.names[h.addr] = name
	}
}

const verifier = "a-code-verifier-that-is-long-enough-for-pkce-0123456789"

func challengeFor(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// openPage runs /authorize and returns the page's embedded JSON.
func (h *harness) openPage(t *testing.T) (request, challenge string) {
	t.Helper()
	q := url.Values{
		"client_id": {"mastodon"}, "redirect_uri": {testRedirect}, "response_type": {"code"},
		"scope": {"openid profile email"}, "state": {"st4te"}, "nonce": {"n0nce"},
		"code_challenge": {challengeFor(verifier)}, "code_challenge_method": {"S256"},
	}
	resp, err := http.Get(h.http.URL + "/authorize?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorize: %d %s", resp.StatusCode, body)
	}
	const open = `<script id="page" type="application/json">`
	s := string(body)
	i := strings.Index(s, open)
	if i < 0 {
		t.Fatalf("no page JSON in %s", s)
	}
	s = s[i+len(open):]
	s = s[:strings.Index(s, "</script>")]
	var page struct {
		Request   string `json:"request"`
		Challenge string `json:"challenge"`
		Chains    []struct {
			ID      string `json:"id"`
			ChainID string `json:"chainId"`
		} `json:"chains"`
	}
	if err := json.Unmarshal([]byte(s), &page); err != nil {
		t.Fatalf("page JSON %q: %v", s, err)
	}
	if len(page.Chains) != 1 || page.Chains[0].ID != "fleet-phoenix" || page.Chains[0].ChainID != "phoenix-1" {
		t.Fatalf("chains on the page: %+v", page.Chains)
	}
	return page.Request, page.Challenge
}

// sign produces what Keplr's signArbitrary returns for the harness key.
func (h *harness) sign(t *testing.T, data string) WalletSignature {
	t.Helper()
	msg, err := adr036SignBytes(h.addr, data)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := h.priv.Sign(msg)
	if err != nil {
		t.Fatal(err)
	}
	var out WalletSignature
	out.PubKey.Type = secp256k1PubKeyType
	out.PubKey.Value = base64.StdEncoding.EncodeToString(h.priv.PubKey().Bytes())
	out.Signature = base64.StdEncoding.EncodeToString(sig)
	return out
}

func (h *harness) verify(t *testing.T, request string, sig WalletSignature) (int, map[string]string) {
	t.Helper()
	body, _ := json.Marshal(verifyRequest{Request: request, Chain: "fleet-phoenix", Address: h.addr, Signature: sig})
	resp, err := http.Post(h.http.URL+"/authorize/verify", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (h *harness) exchange(t *testing.T, form url.Values, basic bool) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.http.URL+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if basic {
		req.SetBasicAuth("mastodon", testSecret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// signIn runs the page and signature steps and returns the code.
func (h *harness) signIn(t *testing.T) string {
	t.Helper()
	request, challenge := h.openPage(t)
	status, out := h.verify(t, request, h.sign(t, challenge))
	if status != http.StatusOK {
		t.Fatalf("verify: %d %v", status, out)
	}
	u, err := url.Parse(out["redirect"])
	if err != nil || !strings.HasPrefix(out["redirect"], testRedirect+"?") {
		t.Fatalf("redirect %q", out["redirect"])
	}
	if u.Query().Get("state") != "st4te" {
		t.Fatalf("state not carried back: %s", u)
	}
	return u.Query().Get("code")
}

func TestLoginFlow(t *testing.T) {
	h := newHarness(t, "")
	h.member(`{"status":"MEMBER_STATUS_ACTIVE","trust_level":"TRUST_LEVEL_PROVISIONAL"}`, "phoenix-one")
	code := h.signIn(t)

	status, tok := h.exchange(t, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {testRedirect}, "code_verifier": {verifier},
	}, true)
	if status != http.StatusOK {
		t.Fatalf("token: %d %v", status, tok)
	}
	claims := checkIDToken(t, h, tok["id_token"].(string))
	sub := hex.EncodeToString(h.priv.PubKey().Address())
	for k, want := range map[string]any{
		"iss": testIssuer, "aud": "mastodon", "nonce": "n0nce", "sub": sub,
		"preferred_username": "phoenix-one", "email": sub + "@wallet.invalid", "email_verified": true,
	} {
		if claims[k] != want {
			t.Errorf("id_token %s = %v, want %v", k, claims[k], want)
		}
	}

	// userinfo carries the same identity
	req, _ := http.NewRequest(http.MethodGet, h.http.URL+"/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tok["access_token"].(string))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var info map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&info)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || info["sub"] != sub || info["preferred_username"] != "phoenix-one" {
		t.Fatalf("userinfo: %d %v", resp.StatusCode, info)
	}

	// a code is single use
	status, _ = h.exchange(t, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {testRedirect}, "code_verifier": {verifier},
	}, true)
	if status != http.StatusBadRequest {
		t.Fatalf("reused code: status %d", status)
	}
}

func checkIDToken(t *testing.T, h *harness, tok string) map[string]any {
	t.Helper()
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("id_token %q", tok)
	}
	// verify against the published JWKS, as Mastodon does
	resp, err := http.Get(h.http.URL + "/jwks")
	if err != nil {
		t.Fatal(err)
	}
	var set struct {
		Keys []struct{ Kid, N, E string } `json:"keys"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&set)
	resp.Body.Close()
	var header struct{ Alg, Kid string }
	hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
	_ = json.Unmarshal(hb, &header)
	if header.Alg != "RS256" || len(set.Keys) != 1 || set.Keys[0].Kid != header.Kid {
		t.Fatalf("header %+v, jwks %+v", header, set)
	}
	n, _ := base64.RawURLEncoding.DecodeString(set.Keys[0].N)
	e, _ := base64.RawURLEncoding.DecodeString(set.Keys[0].E)
	pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("id_token signature: %v", err)
	}
	var claims map[string]any
	body, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if err := json.Unmarshal(body, &claims); err != nil {
		t.Fatal(err)
	}
	return claims
}

func TestTokenRejects(t *testing.T) {
	h := newHarness(t, "")
	h.member(`{"trust_level":"TRUST_LEVEL_NEW"}`, "aurora") // status left out: proto3 zero is ACTIVE
	good := func(code string) url.Values {
		return url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {testRedirect}, "code_verifier": {verifier}}
	}
	cases := []struct {
		name   string
		form   func(code string) url.Values
		basic  bool
		status int
	}{
		{"wrong secret", func(c string) url.Values {
			f := good(c)
			f.Set("client_id", "mastodon")
			f.Set("client_secret", "nope")
			return f
		}, false, http.StatusUnauthorized},
		{"wrong redirect", func(c string) url.Values { f := good(c); f.Set("redirect_uri", "https://evil.example/cb"); return f }, true, http.StatusBadRequest},
		{"wrong verifier", func(c string) url.Values { f := good(c); f.Set("code_verifier", "not-the-verifier"); return f }, true, http.StatusBadRequest},
		{"missing verifier", func(c string) url.Values { f := good(c); f.Del("code_verifier"); return f }, true, http.StatusBadRequest},
		{"client_secret_post", func(c string) url.Values {
			f := good(c)
			f.Set("client_id", "mastodon")
			f.Set("client_secret", testSecret)
			return f
		}, false, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, out := h.exchange(t, tc.form(h.signIn(t)), tc.basic)
			if status != tc.status {
				t.Fatalf("status %d (%v), want %d", status, out, tc.status)
			}
		})
	}
}

func TestVerifyRejects(t *testing.T) {
	cases := []struct {
		name    string
		minimum string
		member  string // "" = not a member
		nameOf  string
		down    bool
		status  int
		wantErr string
	}{
		{name: "not a member", status: http.StatusForbidden, wantErr: "not an active member"},
		{name: "zeroed", member: `{"status":"MEMBER_STATUS_ZEROED"}`, nameOf: "phoenix", status: http.StatusForbidden, wantErr: "not an active member"},
		{name: "inactive", member: `{"status":"MEMBER_STATUS_INACTIVE","trust_level":"TRUST_LEVEL_CORE"}`, nameOf: "phoenix", status: http.StatusForbidden, wantErr: "not an active member"},
		{name: "below the floor", minimum: "TRUST_LEVEL_ESTABLISHED", member: `{"trust_level":"TRUST_LEVEL_PROVISIONAL"}`, nameOf: "phoenix",
			status: http.StatusForbidden, wantErr: "trust level established or above"},
		{name: "at the floor", minimum: "TRUST_LEVEL_ESTABLISHED", member: `{"trust_level":"TRUST_LEVEL_ESTABLISHED"}`, nameOf: "phoenix", status: http.StatusOK},
		{name: "numeric enums", member: `{"status":0,"trust_level":2}`, nameOf: "phoenix", status: http.StatusOK},
		{name: "no primary name", member: `{}`, status: http.StatusForbidden, wantErr: "no primary name"},
		{name: "chain unreachable", member: `{}`, nameOf: "phoenix", down: true, status: http.StatusBadGateway, wantErr: "could not reach"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.minimum)
			if tc.member != "" {
				h.member(tc.member, tc.nameOf)
			}
			h.lcd.down = tc.down
			request, challenge := h.openPage(t)
			status, out := h.verify(t, request, h.sign(t, challenge))
			if status != tc.status || !strings.Contains(out["error"], tc.wantErr) {
				t.Fatalf("status %d %v, want %d %q", status, out, tc.status, tc.wantErr)
			}
		})
	}
}

func TestVerifySignatureAndPageLimits(t *testing.T) {
	h := newHarness(t, "")
	h.member(`{}`, "phoenix")
	request, challenge := h.openPage(t)

	// a signature over anything but this page's challenge is refused
	status, out := h.verify(t, request, h.sign(t, challenge+"x"))
	if status != http.StatusForbidden || !strings.Contains(out["error"], "signature rejected") {
		t.Fatalf("status %d %v", status, out)
	}
	// the page allows maxAttempts tries in all, then expires
	for i := 1; i < maxAttempts; i++ {
		h.verify(t, request, h.sign(t, "wrong"))
	}
	status, out = h.verify(t, request, h.sign(t, challenge))
	if status != http.StatusBadRequest || !strings.Contains(out["error"], "expired") {
		t.Fatalf("after %d attempts: %d %v", maxAttempts, status, out)
	}

	// a page's challenge is spent once it signs in
	request, challenge = h.openPage(t)
	sig := h.sign(t, challenge)
	if status, out := h.verify(t, request, sig); status != http.StatusOK {
		t.Fatalf("first: %d %v", status, out)
	}
	if status, _ := h.verify(t, request, sig); status != http.StatusBadRequest {
		t.Fatalf("replay: %d", status)
	}
}

func TestAuthorizeRejects(t *testing.T) {
	h := newHarness(t, "")
	base := url.Values{
		"client_id": {"mastodon"}, "redirect_uri": {testRedirect}, "response_type": {"code"}, "scope": {"openid"},
	}
	cases := []struct {
		name string
		edit func(url.Values)
	}{
		{"unknown client", func(v url.Values) { v.Set("client_id", "other") }},
		{"foreign redirect", func(v url.Values) { v.Set("redirect_uri", "https://evil.example/cb") }},
		{"implicit flow", func(v url.Values) { v.Set("response_type", "token") }},
		{"no openid scope", func(v url.Values) { v.Set("scope", "profile") }},
		{"plain pkce", func(v url.Values) { v.Set("code_challenge", "abc"); v.Set("code_challenge_method", "plain") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := url.Values{}
			for k, vals := range base {
				v[k] = append([]string(nil), vals...)
			}
			tc.edit(v)
			resp, err := http.Get(h.http.URL + "/authorize?" + v.Encode())
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status %d", resp.StatusCode)
			}
		})
	}
}

func TestMembershipEndpoint(t *testing.T) {
	cases := []struct {
		name   string
		member string
		down   bool
		want   Membership
	}{
		{"active", `{"status":"MEMBER_STATUS_ACTIVE"}`, false, MembershipActive},
		{"zeroed", `{"status":"MEMBER_STATUS_ZEROED"}`, false, MembershipInactive},
		{"never a member", "", false, MembershipInactive},
		{"chain unreachable", `{}`, true, MembershipUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, "")
			if tc.member != "" {
				h.member(tc.member, "")
			}
			h.lcd.down = tc.down
			sub := hex.EncodeToString(h.priv.PubKey().Address())
			resp, err := http.Get(h.http.URL + "/membership/" + sub)
			if err != nil {
				t.Fatal(err)
			}
			var out map[string]string
			_ = json.NewDecoder(resp.Body).Decode(&out)
			resp.Body.Close()
			if Membership(out["status"]) != tc.want {
				t.Fatalf("status %v, want %s", out, tc.want)
			}
		})
	}

	t.Run("no chains", func(t *testing.T) {
		h := newHarness(t, "")
		h.srv.chains.chains = map[string]Chain{}
		if got := h.srv.lcd.Membership(context.Background(), h.srv.chains.All(), make([]byte, 20)); got != MembershipUnknown {
			t.Fatalf("got %s", got)
		}
	})
	t.Run("bad sub", func(t *testing.T) {
		h := newHarness(t, "")
		resp, _ := http.Get(h.http.URL + "/membership/xyz")
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status %d", resp.StatusCode)
		}
	})
}

func TestMembershipAcrossChains(t *testing.T) {
	// active on any linked chain is enough; one unreachable chain makes a
	// "no" elsewhere inconclusive
	h := newHarness(t, "")
	other := &fakeLCD{members: map[string]string{}, names: map[string]string{}}
	otherSrv := httptest.NewServer(other)
	defer otherSrv.Close()
	chains := map[string]Chain{
		"a": {ChainID: "phoenix-1", Rest: h.srv.chains.chains["fleet-phoenix"].Rest, Bech32Prefix: "sprkdrm"},
		"b": {ChainID: "aurora-1", Rest: otherSrv.URL, Bech32Prefix: "aurora"},
	}
	addr := h.priv.PubKey().Address().Bytes()
	auroraAddr, _ := bech32.ConvertAndEncode("aurora", addr)

	other.members[auroraAddr] = `{"status":"MEMBER_STATUS_ACTIVE"}`
	if got := h.srv.lcd.Membership(context.Background(), chains, addr); got != MembershipActive {
		t.Fatalf("active on the second chain: %s", got)
	}
	delete(other.members, auroraAddr)
	if got := h.srv.lcd.Membership(context.Background(), chains, addr); got != MembershipInactive {
		t.Fatalf("member nowhere: %s", got)
	}
	other.down = true
	if got := h.srv.lcd.Membership(context.Background(), chains, addr); got != MembershipUnknown {
		t.Fatalf("one chain down: %s", got)
	}
}

func TestParseSigningKey(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	der := mustPKCS8(t, key)
	if _, err := parseSigningKey(base64.StdEncoding.EncodeToString(der)); err != nil {
		t.Fatalf("base64 DER: %v", err)
	}
	pemKey := "-----BEGIN PRIVATE KEY-----\n" + base64.StdEncoding.EncodeToString(der) + "\n-----END PRIVATE KEY-----\n"
	if _, err := parseSigningKey(pemKey); err != nil {
		t.Fatalf("PEM: %v", err)
	}
	if _, err := parseSigningKey("not a key"); err == nil {
		t.Fatal("garbage accepted")
	}
}

func mustPKCS8(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func TestChainListRefresh(t *testing.T) {
	var fail bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, `{
			"fleet-a": {"chainId":"phoenix-1","chainName":"Phoenix","rest":"https://api.phoenix.example/","bech32Prefix":"sprkdrm"},
			"fleet-b": {"chainId":"","rest":"https://api.aurora.example","bech32Prefix":"sprkdrm"}
		}`)
	}))
	defer srv.Close()

	l := NewChainList(srv.URL+"/sparkdream/login-chains.json", http.DefaultClient)
	if err := l.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	all := l.All()
	if len(all) != 1 || all["fleet-a"].Rest != "https://api.phoenix.example" {
		t.Fatalf("an entry missing its chain id must be dropped, rest trimmed: %+v", all)
	}

	fail = true
	if err := l.Refresh(context.Background()); err == nil {
		t.Fatal("a failed fetch reported no error")
	}
	if len(l.All()) != 1 {
		t.Fatal("a failed fetch dropped the cached list")
	}
}
