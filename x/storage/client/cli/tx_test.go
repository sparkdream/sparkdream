package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

func writePNG(t *testing.T, alpha uint8) (string, []byte) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := 0; i < 16; i++ {
		img.SetNRGBA(i%4, i/4, color.NRGBA{uint8(i * 10), 50, 90, alpha})
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	path := filepath.Join(t.TempDir(), "image.png")
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
	return path, buf.Bytes()
}

// The command computes the hash and the size from the file.
func TestFileRequest(t *testing.T) {
	path, data := writePNG(t, 255)
	msg, err := fileRequest("requester", "arweave", path, "image/png", "https://files.example/image.png", math.NewInt(5))
	require.NoError(t, err)
	sum := sha256.Sum256(data)
	require.Equal(t, hex.EncodeToString(sum[:]), msg.Sha256)
	require.Equal(t, uint64(len(data)), msg.SizeBytes)
	require.Equal(t, "image/png", msg.ContentType)
	require.Equal(t, "arweave", msg.Backend)
	require.Equal(t, int64(5), msg.MaxFee.Int64())
}

// A file an operator would decline is refused before anything is paid.
func TestFileRequestRefusesAFileOutsideTheProfile(t *testing.T) {
	transparent, _ := writePNG(t, 0)
	_, err := fileRequest("requester", "arweave", transparent, "image/png", "https://files.example/a.png", math.NewInt(5))
	require.ErrorContains(t, err, "would be declined")

	opaque, _ := writePNG(t, 255)
	_, err = fileRequest("requester", "arweave", opaque, "image/jpeg", "https://files.example/a.jpg", math.NewInt(5))
	require.ErrorContains(t, err, "would be declined")

	_, err = fileRequest("requester", "arweave", filepath.Join(t.TempDir(), "missing.png"), "image/png", "https://files.example/a.png", math.NewInt(5))
	require.Error(t, err)

	empty := filepath.Join(t.TempDir(), "empty.png")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))
	_, err = fileRequest("requester", "arweave", empty, "image/png", "https://files.example/a.png", math.NewInt(5))
	require.ErrorContains(t, err, "is empty")
}

func TestTxCmd(t *testing.T) {
	cmd := GetTxCmd()
	sub, _, err := cmd.Find([]string{"request-file-copy"})
	require.NoError(t, err)
	require.Equal(t, "request-file-copy", sub.Name())
}
