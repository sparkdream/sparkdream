package types

import "testing"

func TestPostFlagKey(t *testing.T) {
	if len(PostFlagKey) == 0 {
		t.Fatal("PostFlagKey is empty")
	}
	if string(PostFlagKey) != "postFlag/value/" {
		t.Errorf("unexpected prefix: %s", string(PostFlagKey))
	}
}
