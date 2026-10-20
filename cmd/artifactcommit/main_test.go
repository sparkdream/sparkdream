package main

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
)

func TestBuildTreeMatchesChainVerification(t *testing.T) {
	entries := []entryIn{{TokenID: 1, Salt: "0a0b"}, {TokenID: 2}, {TokenID: 3, Salt: "ff"}}
	entries[0].Metadata.Name = "aurora #1"
	entries[1].Metadata.Name = "aurora #2"
	entries[1].Metadata.Attributes = append(entries[1].Metadata.Attributes, struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}{"rarity", "rare"})
	res, err := buildTree(7, entries)
	require.NoError(t, err)
	require.Len(t, res.Leaves, 3)
	require.Len(t, res.Leaves[1].Salt, 64, "missing salt is generated")

	root, err := hex.DecodeString(res.Root)
	require.NoError(t, err)
	for i, leaf := range res.Leaves {
		salt, err := hex.DecodeString(leaf.Salt)
		require.NoError(t, err)
		meta := types.TokenMetadata{Name: entries[i].Metadata.Name}
		for _, a := range entries[i].Metadata.Attributes {
			meta.Attributes = append(meta.Attributes, types.Attribute{Key: a.Key, Value: a.Value})
		}
		proof := make([][]byte, len(leaf.Proof))
		for j, p := range leaf.Proof {
			proof[j], _ = hex.DecodeString(p)
		}
		require.True(t, types.VerifyRevealProof(types.RevealLeaf(7, leaf.TokenID, salt, meta), proof, root), "leaf %d", i)
		require.Equal(t, types.MetadataHash(meta), leaf.MetadataHash)
	}

	_, err = buildTree(7, []entryIn{{TokenID: 1}, {TokenID: 1}})
	require.Error(t, err, "duplicate token id")
	_, err = buildTree(7, nil)
	require.Error(t, err)
}

func TestSourceHashIgnoresModuleFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "aurora.gno"), []byte("package aurora\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gnomod.toml"), []byte("module = \"gno.land/p/demo/aurora\"\n"), 0o600))
	files, err := readPackage(dir)
	require.NoError(t, err)
	h := types.RendererSourceHash(files)
	require.Equal(t, types.RendererSourceHash([]types.GnoSourceFile{{Name: "aurora.gno", Body: "package aurora\n"}}), h)
}
