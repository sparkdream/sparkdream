package types_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// gogoproto encodes a map field by ranging over the Go map unless the file
// sets (gogoproto.stable_marshaler_all): the same state then serializes to
// different bytes on different nodes, so the app hash splits. Every proto
// file with a map field must set the option.
func TestProtoMapFieldsHaveStableMarshaler(t *testing.T) {
	root := filepath.Join("..", "..", "..", "proto", "sparkdream")
	checked := 0
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".proto") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		src := string(b)
		if !strings.Contains(src, "map<") {
			return nil
		}
		checked++
		require.Contains(t, src, "option (gogoproto.stable_marshaler_all) = true;",
			"%s has a map field but no (gogoproto.stable_marshaler_all) option", p)
		return nil
	})
	require.NoError(t, err)
	require.Positive(t, checked, "no proto files with map fields found under %s", root)
}
