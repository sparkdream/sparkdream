package types

import "testing"

func TestPostKey(t *testing.T) {
	if len(PostKey) == 0 {
		t.Fatal("PostKey is empty")
	}
	if string(PostKey) != "post/value/" {
		t.Errorf("unexpected prefix: %s", string(PostKey))
	}
}
