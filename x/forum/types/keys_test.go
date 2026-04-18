package types

import "testing"

func TestModuleConstants(t *testing.T) {
	if ModuleName != "forum" {
		t.Errorf("ModuleName = %q, want %q", ModuleName, "forum")
	}
	if StoreKey != ModuleName {
		t.Errorf("StoreKey = %q, want %q", StoreKey, ModuleName)
	}
	if GovModuleName != "gov" {
		t.Errorf("GovModuleName = %q, want %q", GovModuleName, "gov")
	}
}

func TestPrefixKeys(t *testing.T) {
	cases := []struct {
		name   string
		prefix []byte
		want   string
	}{
		{"ParamsKey", ParamsKey, "p_forum"},
		{"BountyKey", BountyKey, "bounty/value/"},
		{"BountyCountKey", BountyCountKey, "bounty/count/"},
		{"PostSeqKey", PostSeqKey, "post/seq/"},
		{"ExpirationQueueKey", ExpirationQueueKey, "expiration_queue/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.prefix) != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, string(tc.prefix), tc.want)
			}
		})
	}
}
