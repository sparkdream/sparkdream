package main

import (
	"strings"
	"testing"
)

// The chain accepts only public-domain content, so the bridge anchors a post
// only when its author dedicated it with #cc0 or #publicdomain, and claims
// the matching license on the submission.
func TestIngestRequiresPublicDomainHashtag(t *testing.T) {
	const uri = "https://md.test/users/alice/statuses/10"
	acct := &Account{Acct: "alice", URI: "https://md.test/users/alice"}
	for _, tc := range []struct {
		name, body, want string
	}{
		{"cc0", "<p>my drawing #cc0</p>", "CC0-1.0"},
		{"publicdomain", `<p>an 1860 map #<span>PublicDomain</span></p>`, "PDM-1.0"},
		{"untagged", "<p>all rights reserved</p>", ""},
		{"lookalike", "<p>#cc0art</p>", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			note := strings.Replace(publicNote, "<p>hello federation #cc0</p>", tc.body, 1)
			b, sub := testBridge(t, map[string]string{"/users/alice/statuses/10": note})
			if err := b.IngestURI(t.Context(), uri, acct); err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(sub.msgs) != 0 {
					t.Fatalf("an undedicated post must not be anchored, got %d submissions", len(sub.msgs))
				}
				return
			}
			if len(sub.msgs) != 1 {
				t.Fatalf("want one submission, got %d", len(sub.msgs))
			}
			if got := sub.msgs[0].License; got != tc.want {
				t.Fatalf("license = %q, want %q", got, tc.want)
			}
		})
	}
}

// An undedicated timeline status stays on the revisit list, so an author who
// adds #cc0 in an edit is anchored then.
func TestUntaggedTimelineStatusIsTracked(t *testing.T) {
	const uri = "https://md.test/users/alice/statuses/10"
	note := strings.Replace(publicNote, "<p>hello federation #cc0</p>", "<p>no tag yet</p>", 1)
	b, sub := testBridge(t, map[string]string{"/users/alice/statuses/10": note})
	st := &Status{ID: "10", URI: uri, Account: Account{Acct: "alice", URI: "https://md.test/users/alice"}}
	if err := b.IngestStatus(t.Context(), st); err != nil {
		t.Fatal(err)
	}
	if len(sub.msgs) != 0 {
		t.Fatalf("got %d submissions", len(sub.msgs))
	}
	if tr, ok := b.state.Tracking(uri); !ok || tr.StatusID != "10" {
		t.Fatalf("untagged status must be tracked for revisits: %+v ok=%v", tr, ok)
	}
}
