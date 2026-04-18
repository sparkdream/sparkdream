package types

import "testing"

func TestThreadMetadataKey(t *testing.T) {
	if len(ThreadMetadataKey) == 0 {
		t.Fatal("ThreadMetadataKey is empty")
	}
	if string(ThreadMetadataKey) != "threadMetadata/value/" {
		t.Errorf("unexpected prefix: %s", string(ThreadMetadataKey))
	}
}
