// Command sdaplogin lets members of a Spark Dream chain sign in to a
// launcher-run Mastodon instance with their wallet. It is a single-client
// OpenID Connect provider: Mastodon is the client (OIDC_* env), and a user
// is a wallet key that is an active x/rep member of one of the chains linked
// to the instance.
//
// Flow: Mastodon sends the browser to /authorize. The page has the member
// pick a chain and sign a challenge with Keplr (ADR-036 signArbitrary). The
// verify step checks the signature, the member's status and trust level on
// that chain's LCD, and reads their primary x/name. It then hands Mastodon a
// code, which Mastodon exchanges at /token and /userinfo.
//
// Identity: sub is the hex of the 20-byte address, the same on every chain
// the key is used on, so a member keeps one account whichever linked chain
// they sign in through. The handle is the member's primary x/name, which the
// image's initializer (zz_sparkdream_wallet_login.rb) turns into the
// username when the account is first created. Keying accounts on the
// address rather than the name means a name that later changes hands never
// opens its previous holder's account.
//
// The chain list is not configured here: the launcher stores it in the
// instance (mastodon-bootstrap login-chain sync) and the instance serves it
// at LOGIN_CHAINS_URL, re-read every LOGIN_REFRESH. /membership/{sub} answers
// the instance's hourly sweep, which disables the login of accounts that are
// no longer members.
//
// State (open login pages, codes, tokens) is in memory: a restart only
// makes a member in the middle of signing in start again.
package main

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Config comes from the environment.
type Config struct {
	Issuer       string        // LOGIN_ISSUER: this service's public https URL
	ClientID     string        // LOGIN_CLIENT_ID (default "mastodon")
	ClientSecret string        // LOGIN_CLIENT_SECRET
	RedirectURI  string        // LOGIN_REDIRECT_URI: Mastodon's callback, matched exactly
	SigningKey   string        // LOGIN_SIGNING_KEY: RSA key, PEM or base64 DER (PKCS#8 or PKCS#1)
	ChainsURL    string        // LOGIN_CHAINS_URL: the instance's /sparkdream/login-chains.json
	Listen       string        // LOGIN_LISTEN (default ":8080")
	EmailDomain  string        // LOGIN_EMAIL_DOMAIN: placeholder email domain (default "wallet.invalid")
	Refresh      time.Duration // LOGIN_REFRESH: chain list refresh (default 60s)
}

func loadConfig() Config {
	return Config{
		Issuer:       strings.TrimRight(os.Getenv("LOGIN_ISSUER"), "/"),
		ClientID:     env("LOGIN_CLIENT_ID", "mastodon"),
		ClientSecret: os.Getenv("LOGIN_CLIENT_SECRET"),
		RedirectURI:  os.Getenv("LOGIN_REDIRECT_URI"),
		SigningKey:   os.Getenv("LOGIN_SIGNING_KEY"),
		ChainsURL:    os.Getenv("LOGIN_CHAINS_URL"),
		Listen:       env("LOGIN_LISTEN", ":8080"),
		EmailDomain:  env("LOGIN_EMAIL_DOMAIN", "wallet.invalid"),
		Refresh:      envDuration("LOGIN_REFRESH", 60*time.Second),
	}
}

func (c Config) Validate() error {
	for name, v := range map[string]string{
		"LOGIN_ISSUER":        c.Issuer,
		"LOGIN_CLIENT_SECRET": c.ClientSecret,
		"LOGIN_REDIRECT_URI":  c.RedirectURI,
		"LOGIN_SIGNING_KEY":   c.SigningKey,
		"LOGIN_CHAINS_URL":    c.ChainsURL,
	} {
		if v == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	for name, v := range map[string]string{"LOGIN_ISSUER": c.Issuer, "LOGIN_REDIRECT_URI": c.RedirectURI} {
		u, err := url.Parse(v)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("%s must be an https URL", name)
		}
	}
	return nil
}

func main() {
	cfg := loadConfig()
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "sdaplogin:", err)
		os.Exit(2)
	}
	key, err := parseSigningKey(cfg.SigningKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdaplogin: LOGIN_SIGNING_KEY:", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client := &http.Client{Timeout: 15 * time.Second}
	chains := NewChainList(cfg.ChainsURL, client)
	// the instance may still be starting: serve anyway and pick the list up
	// on the next refresh
	if err := chains.Refresh(ctx); err != nil {
		log.Printf("sdaplogin: chain list: %v (retrying every %s)", err, cfg.Refresh)
	} else {
		log.Printf("sdaplogin: %d chain(s) linked", len(chains.All()))
	}
	go chains.Run(ctx, cfg.Refresh)

	srv := NewServer(cfg, key, chains, client)
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				srv.Sweep()
			}
		}
	}()

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdown)
	}()
	log.Printf("sdaplogin: issuer %s, listening on %s", cfg.Issuer, cfg.Listen)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("sdaplogin: %v", err)
	}
}

// parseSigningKey reads an RSA private key given as PEM or as base64 DER
// (the launcher passes base64 so the env value stays on one line).
func parseSigningKey(v string) (*rsa.PrivateKey, error) {
	var der []byte
	if block, _ := pem.Decode([]byte(v)); block != nil {
		der = block.Bytes
	} else {
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v))
		if err != nil {
			return nil, errors.New("neither PEM nor base64 DER")
		}
		der = b
	}
	if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		rsaKey, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("not an RSA key")
		}
		return rsaKey, nil
	}
	k, err := x509.ParsePKCS1PrivateKey(der)
	if err != nil {
		return nil, errors.New("not a PKCS#8 or PKCS#1 RSA key")
	}
	return k, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
