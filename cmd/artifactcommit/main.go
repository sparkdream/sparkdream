// artifactcommit builds the off-chain side of x/artifact commit-reveal and
// renderer classes (docs/x-artifact-spec.md §7.15, §7.16):
//
//	artifactcommit tree <class-id> <entries.json>   Merkle root and per-token proofs for a blind mint
//	artifactcommit seed <hex-seed>                   commitment of a generative class seed
//	artifactcommit source-hash <package-dir>         renderer source hash of a Gno package directory
//
// entries.json is a list of {"token_id": N, "salt": "<hex>", "metadata": {...}}
// where metadata uses the TokenMetadata JSON field names (name, description,
// uri, uri_hash, attributes[{key,value}]). An empty salt is filled with 32
// random bytes and echoed back, so keep the output: it is what MsgReveal
// needs later.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"sparkdream/x/artifact/types"
)

type entryIn struct {
	TokenID  uint64 `json:"token_id"`
	Salt     string `json:"salt"`
	Metadata struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		URI         string `json:"uri"`
		URIHash     string `json:"uri_hash"`
		Attributes  []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"attributes"`
	} `json:"metadata"`
}

type leafOut struct {
	TokenID      uint64   `json:"token_id"`
	Salt         string   `json:"salt"`
	MetadataHash string   `json:"metadata_hash"`
	Proof        []string `json:"proof"`
}

type treeOut struct {
	ClassID uint64    `json:"class_id"`
	Root    string    `json:"root"`
	Leaves  []leafOut `json:"leaves"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "artifactcommit:", err)
		os.Exit(1)
	}
}

func run(args []string, out *os.File) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: artifactcommit tree <class-id> <entries.json> | seed <hex-seed> | source-hash <dir>")
	}
	switch args[0] {
	case "tree":
		if len(args) != 3 {
			return fmt.Errorf("usage: artifactcommit tree <class-id> <entries.json>")
		}
		classID, err := strconv.ParseUint(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("class id: %w", err)
		}
		raw, err := os.ReadFile(args[2])
		if err != nil {
			return err
		}
		var entries []entryIn
		if err := json.Unmarshal(raw, &entries); err != nil {
			return fmt.Errorf("entries: %w", err)
		}
		res, err := buildTree(classID, entries)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(res)
	case "seed":
		seed, err := types.DecodeHexSecret("seed", args[1])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, types.SeedCommitment(seed))
		return err
	case "source-hash":
		files, err := readPackage(args[1])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, types.RendererSourceHash(files))
		return err
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func buildTree(classID uint64, entries []entryIn) (treeOut, error) {
	if len(entries) == 0 {
		return treeOut{}, fmt.Errorf("no entries")
	}
	res := treeOut{ClassID: classID}
	leaves := make([][]byte, len(entries))
	seen := map[uint64]bool{}
	for i, e := range entries {
		if e.TokenID == 0 || seen[e.TokenID] {
			return treeOut{}, fmt.Errorf("entry %d: token_id must be unique and >= 1", i)
		}
		seen[e.TokenID] = true
		if e.Salt == "" {
			bz := make([]byte, 32)
			if _, err := rand.Read(bz); err != nil {
				return treeOut{}, err
			}
			e.Salt = hex.EncodeToString(bz)
		}
		salt, err := types.DecodeHexSecret("salt", e.Salt)
		if err != nil {
			return treeOut{}, fmt.Errorf("entry %d: %w", i, err)
		}
		meta := types.TokenMetadata{Name: e.Metadata.Name, Description: e.Metadata.Description, Uri: e.Metadata.URI, UriHash: e.Metadata.URIHash}
		for _, a := range e.Metadata.Attributes {
			meta.Attributes = append(meta.Attributes, types.Attribute{Key: a.Key, Value: a.Value})
		}
		leaves[i] = types.RevealLeaf(classID, e.TokenID, salt, meta)
		res.Leaves = append(res.Leaves, leafOut{TokenID: e.TokenID, Salt: e.Salt, MetadataHash: types.MetadataHash(meta)})
	}
	root, proofs := types.BuildRevealTree(leaves)
	res.Root = hex.EncodeToString(root)
	for i := range res.Leaves {
		res.Leaves[i].Proof = types.HexHashes(proofs[i])
		if res.Leaves[i].Proof == nil {
			res.Leaves[i].Proof = []string{}
		}
	}
	return res, nil
}

// readPackage lists a package directory the way gnovm stores it: every
// regular file in the directory, by name.
func readPackage(dir string) ([]types.GnoSourceFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []types.GnoSourceFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		files = append(files, types.GnoSourceFile{Name: e.Name(), Body: string(body)})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s has no files", dir)
	}
	return files, nil
}
