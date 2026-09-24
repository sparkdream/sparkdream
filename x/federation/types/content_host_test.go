package types

import "testing"

func TestContentURIHostAllowed(t *testing.T) {
	cases := []struct {
		name  string
		hosts []string
		uri   string
		ok    bool
	}{
		{"peer host", nil, "https://phoenix.example/users/a/statuses/1", true},
		{"port ignored", nil, "http://phoenix.example:3000/ap/users/1/statuses/2", true},
		{"case-insensitive host", nil, "https://PHOENIX.example/users/a/statuses/1", true},
		{"listed content host", []string{"social.phoenix.example"}, "https://social.phoenix.example/users/a/statuses/1", true},
		{"another instance", nil, "https://aurora.example/users/a/statuses/1", false},
		{"subdomain is not the peer", nil, "https://evil.phoenix.example/users/a/statuses/1", false},
		{"suffix is not the peer", nil, "https://phoenix.example.evil/users/a/statuses/1", false},
		{"userinfo cannot smuggle the peer", nil, "https://phoenix.example@aurora.example/x", false},
		{"not http", nil, "at://did:plc:abc/app.bsky.feed.post/1", false},
		{"no host", nil, "https:///users/a", false},
		{"relative", nil, "/users/a/statuses/1", false},
	}
	for _, c := range cases {
		err := ContentURIHostAllowed("phoenix.example", c.hosts, c.uri)
		if (err == nil) != c.ok {
			t.Errorf("%s: ContentURIHostAllowed(%q) err=%v, want ok=%v", c.name, c.uri, err, c.ok)
		}
	}
}

func TestValidateContentHosts(t *testing.T) {
	if err := ValidateContentHosts([]string{"social.phoenix.example", "cdn.phoenix.example"}); err != nil {
		t.Fatalf("valid list rejected: %v", err)
	}
	for _, bad := range [][]string{
		{"Social.Phoenix.example"},
		{"phoenix.example:3000"},
		{"https://phoenix.example"},
		{"a.example", "a.example"},
		{"a1.example", "a2.example", "a3.example", "a4.example", "a5.example", "a6.example", "a7.example", "a8.example", "a9.example"},
	} {
		if err := ValidateContentHosts(bad); err == nil {
			t.Errorf("ValidateContentHosts(%v) accepted", bad)
		}
	}
}

func TestCreatorIdentityHostAllowed(t *testing.T) {
	cases := []struct {
		name     string
		hosts    []string
		identity string
		ok       bool
	}{
		{"peer host", nil, "@alice@phoenix.example", true},
		{"no leading @", nil, "alice@phoenix.example", true},
		{"case-insensitive host", nil, "@alice@Phoenix.Example", true},
		{"port ignored", nil, "@alice@phoenix.example:3000", true},
		{"listed content host", []string{"social.phoenix.example"}, "@alice@social.phoenix.example", true},
		{"another instance", nil, "@alice@aurora.example", false},
		{"subdomain is not the peer", nil, "@alice@evil.phoenix.example", false},
		{"the last @ decides", nil, "@phoenix.example@aurora.example", false},
		{"no host", nil, "@alice", false},
		{"empty host", nil, "@alice@", false},
		{"no user", nil, "@@phoenix.example", false},
	}
	for _, c := range cases {
		err := CreatorIdentityHostAllowed("phoenix.example", c.hosts, c.identity)
		if (err == nil) != c.ok {
			t.Errorf("%s: CreatorIdentityHostAllowed(%q) err=%v, want ok=%v", c.name, c.identity, err, c.ok)
		}
	}
}
