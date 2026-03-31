package cmd

import (
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	cmttypes "github.com/cometbft/cometbft/types"
)

func TestDiscoverArchives(t *testing.T) {
	dir := t.TempDir()

	// Create archive files in non-sorted order
	files := []string{
		"blocks_10001_to_20000.jsonl.gz",
		"blocks_1_to_10000.jsonl.gz",
		"blocks_20001_to_30000.jsonl.gz",
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte{}, 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Add non-matching files that should be ignored
	for _, f := range []string{"README.md", "manifest.csv", ".last_archived_height", "blocks_bad.jsonl.gz"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte{}, 0644); err != nil {
			t.Fatal(err)
		}
	}

	archives, err := discoverArchives(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(archives) != 3 {
		t.Fatalf("expected 3 archives, got %d", len(archives))
	}

	// Verify sorted ascending by fromBlock
	expected := []struct {
		from, to int64
	}{
		{1, 10000},
		{10001, 20000},
		{20001, 30000},
	}
	for i, e := range expected {
		if archives[i].fromBlock != e.from || archives[i].toBlock != e.to {
			t.Errorf("archive[%d]: expected %d-%d, got %d-%d",
				i, e.from, e.to, archives[i].fromBlock, archives[i].toBlock)
		}
	}
}

func TestDiscoverArchives_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	archives, err := discoverArchives(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 0 {
		t.Fatalf("expected 0 archives, got %d", len(archives))
	}
}

func TestDiscoverArchives_NonexistentDir(t *testing.T) {
	_, err := discoverArchives("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestDiscoverArchives_IgnoresSubdirectories(t *testing.T) {
	dir := t.TempDir()

	// Create a subdirectory that matches the pattern name
	if err := os.Mkdir(filepath.Join(dir, "blocks_1_to_100.jsonl.gz"), 0755); err != nil {
		t.Fatal(err)
	}
	// Create a real file
	if err := os.WriteFile(filepath.Join(dir, "blocks_1_to_100.jsonl.gz", "dummy"), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	archives, err := discoverArchives(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 0 {
		t.Fatalf("expected 0 archives (directory should be skipped), got %d", len(archives))
	}
}

func TestDetectArchiveGaps(t *testing.T) {
	tests := []struct {
		name      string
		archives  []archiveFile
		startFrom int64
		endHeight int64
		wantErr   bool
	}{
		{
			name: "continuous coverage",
			archives: []archiveFile{
				{path: "blocks_1_to_1000.jsonl.gz", fromBlock: 1, toBlock: 1000},
				{path: "blocks_1001_to_2000.jsonl.gz", fromBlock: 1001, toBlock: 2000},
				{path: "blocks_2001_to_3000.jsonl.gz", fromBlock: 2001, toBlock: 3000},
			},
			startFrom: 1,
			endHeight: 0,
			wantErr:   false,
		},
		{
			name: "gap between files",
			archives: []archiveFile{
				{path: "blocks_1_to_1000.jsonl.gz", fromBlock: 1, toBlock: 1000},
				{path: "blocks_1500_to_2000.jsonl.gz", fromBlock: 1500, toBlock: 2000},
			},
			startFrom: 1,
			endHeight: 0,
			wantErr:   true,
		},
		{
			name: "gap at start",
			archives: []archiveFile{
				{path: "blocks_500_to_1000.jsonl.gz", fromBlock: 500, toBlock: 1000},
			},
			startFrom: 1,
			endHeight: 0,
			wantErr:   true,
		},
		{
			name: "skips files below startFrom",
			archives: []archiveFile{
				{path: "blocks_1_to_1000.jsonl.gz", fromBlock: 1, toBlock: 1000},
				{path: "blocks_1001_to_2000.jsonl.gz", fromBlock: 1001, toBlock: 2000},
				{path: "blocks_2001_to_3000.jsonl.gz", fromBlock: 2001, toBlock: 3000},
			},
			startFrom: 2001,
			endHeight: 0,
			wantErr:   false,
		},
		{
			name: "gap after skipped files",
			archives: []archiveFile{
				{path: "blocks_1_to_1000.jsonl.gz", fromBlock: 1, toBlock: 1000},
				{path: "blocks_2001_to_3000.jsonl.gz", fromBlock: 2001, toBlock: 3000},
			},
			startFrom: 1001,
			endHeight: 0,
			wantErr:   true,
		},
		{
			name: "gap hidden by endHeight cutoff",
			archives: []archiveFile{
				{path: "blocks_1_to_1000.jsonl.gz", fromBlock: 1, toBlock: 1000},
				{path: "blocks_3001_to_4000.jsonl.gz", fromBlock: 3001, toBlock: 4000},
			},
			startFrom: 1,
			endHeight: 1000,
			wantErr:   false,
		},
		{
			name: "overlapping files are ok",
			archives: []archiveFile{
				{path: "blocks_1_to_1000.jsonl.gz", fromBlock: 1, toBlock: 1000},
				{path: "blocks_500_to_2000.jsonl.gz", fromBlock: 500, toBlock: 2000},
			},
			startFrom: 1,
			endHeight: 0,
			wantErr:   false,
		},
		{
			name:      "empty archives",
			archives:  []archiveFile{},
			startFrom: 1,
			endHeight: 0,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := detectArchiveGaps(tt.archives, tt.startFrom, tt.endHeight)
			if (err != nil) != tt.wantErr {
				t.Errorf("detectArchiveGaps() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseBlockLine(t *testing.T) {
	t.Run("valid block entry", func(t *testing.T) {
		line := []byte(`{"block_id":{"hash":"ABCD","parts":{"total":1,"hash":"EF01"}},"block":{"header":{"height":"42","chain_id":"test-1","time":"2025-01-01T00:00:00Z","version":{"block":"11"}},"data":{"txs":null},"evidence":{"evidence":null},"last_commit":null}}`)

		block, blockID, err := parseBlockLine(line)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if block == nil {
			t.Fatal("block is nil")
		}
		if block.Height != 42 {
			t.Errorf("expected height 42, got %d", block.Height)
		}
		if block.ChainID != "test-1" {
			t.Errorf("expected chain_id test-1, got %s", block.ChainID)
		}
		if len(blockID.Hash) == 0 {
			t.Error("expected non-empty block ID hash")
		}
	})

	t.Run("missing block field", func(t *testing.T) {
		line := []byte(`{"block_id":{"hash":"ABCD"},"block_results":{}}`)

		_, _, err := parseBlockLine(line)
		if err == nil {
			t.Fatal("expected error for missing block")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, _, err := parseBlockLine([]byte(`not json`))
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("nil block_id is tolerated", func(t *testing.T) {
		line := []byte(`{"block":{"header":{"height":"1","time":"2025-01-01T00:00:00Z","version":{"block":"11"}},"data":{"txs":null},"evidence":{"evidence":null},"last_commit":null}}`)

		block, blockID, err := parseBlockLine(line)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if block == nil {
			t.Fatal("block is nil")
		}
		if block.Height != 1 {
			t.Errorf("expected height 1, got %d", block.Height)
		}
		// blockID should be zero value when not provided
		if len(blockID.Hash) != 0 {
			t.Error("expected empty block ID hash when block_id is absent")
		}
	})
}

func TestProcessArchiveFile(t *testing.T) {
	// Create a gzipped JSONL file with 3 blocks
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "blocks_1_to_3.jsonl.gz")

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	for h := 1; h <= 3; h++ {
		line := fmt.Sprintf(`{"block":{"header":{"height":"%d","time":"2025-01-01T00:00:00Z","version":{"block":"11"}},"data":{"txs":null},"evidence":{"evidence":null},"last_commit":null}}`, h)
		if _, err := gz.Write([]byte(line + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	gz.Close()
	f.Close()

	t.Run("reads all blocks", func(t *testing.T) {
		var heights []int64
		err := processArchiveFile(archivePath, 1, 0, func(block *cmttypes.Block, _ cmttypes.BlockID) error {
			heights = append(heights, block.Height)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(heights) != 3 {
			t.Fatalf("expected 3 blocks, got %d", len(heights))
		}
		for i, h := range heights {
			if h != int64(i+1) {
				t.Errorf("block %d: expected height %d, got %d", i, i+1, h)
			}
		}
	})

	t.Run("skips blocks below startFrom", func(t *testing.T) {
		var heights []int64
		err := processArchiveFile(archivePath, 2, 0, func(block *cmttypes.Block, _ cmttypes.BlockID) error {
			heights = append(heights, block.Height)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(heights) != 2 {
			t.Fatalf("expected 2 blocks, got %d", len(heights))
		}
		if heights[0] != 2 || heights[1] != 3 {
			t.Errorf("expected heights [2 3], got %v", heights)
		}
	})

	t.Run("stops at endHeight", func(t *testing.T) {
		var heights []int64
		err := processArchiveFile(archivePath, 1, 2, func(block *cmttypes.Block, _ cmttypes.BlockID) error {
			heights = append(heights, block.Height)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(heights) != 2 {
			t.Fatalf("expected 2 blocks, got %d", len(heights))
		}
		if heights[0] != 1 || heights[1] != 2 {
			t.Errorf("expected heights [1 2], got %v", heights)
		}
	})

	t.Run("handler error propagates", func(t *testing.T) {
		err := processArchiveFile(archivePath, 1, 0, func(block *cmttypes.Block, _ cmttypes.BlockID) error {
			return fmt.Errorf("test error at height %d", block.Height)
		})
		if err == nil {
			t.Fatal("expected error from handler")
		}
		if err.Error() != "test error at height 1" {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestArchiveFilePattern(t *testing.T) {
	tests := []struct {
		name    string
		match   bool
		from    string
		to      string
	}{
		{"blocks_1_to_10000.jsonl.gz", true, "1", "10000"},
		{"blocks_10001_to_20000.jsonl.gz", true, "10001", "20000"},
		{"blocks_1_to_750.jsonl.gz", true, "1", "750"},
		{"blocks_bad_to_100.jsonl.gz", false, "", ""},
		{"blocks_1_to_100.jsonl", false, "", ""},
		{"blocks_1_to_100.gz", false, "", ""},
		{"random_file.txt", false, "", ""},
		{"blocks_1_to_.jsonl.gz", false, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := archiveFilePattern.FindStringSubmatch(tt.name)
			if tt.match {
				if matches == nil {
					t.Fatalf("expected match for %s", tt.name)
				}
				if matches[1] != tt.from || matches[2] != tt.to {
					t.Errorf("expected from=%s to=%s, got from=%s to=%s", tt.from, tt.to, matches[1], matches[2])
				}
			} else {
				if matches != nil {
					t.Errorf("expected no match for %s, got %v", tt.name, matches)
				}
			}
		})
	}
}
