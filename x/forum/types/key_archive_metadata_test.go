package types

import "testing"

func TestArchiveMetadataKey(t *testing.T) {
	if len(ArchiveMetadataKey) == 0 {
		t.Fatal("ArchiveMetadataKey is empty")
	}
	if string(ArchiveMetadataKey) != "archiveMetadata/value/" {
		t.Errorf("unexpected prefix: %s", string(ArchiveMetadataKey))
	}
}
