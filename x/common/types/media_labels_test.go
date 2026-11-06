package types_test

import (
	"crypto/sha256"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/common/types"
)

func TestContainsDataURI(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"empty data uri", "data:,hello", true},
		{"base64 image", "data:image/png;base64,iVBORw0KGgo=", true},
		{"inside html attribute", `<img src="data:image/png;base64,AAAA">`, true},
		{"markdown image", "![x](data:image/gif;base64,R0lGOD)", true},
		{"upper case", "DATA:text/plain,hi", true},
		{"non-image media type", "data:application/zip;base64,UEsDBA==", true},
		{"after newline", "intro\ndata:,x", true},
		{"metadata prose", "metadata: see below, thanks", false},
		{"Data label prose", "Data: 5 records, all valid", false},
		{"whitespace before comma", "data: something, else", false},
		{"no comma (type and params still read as a data URI)", "data:image/png;base64", true},
		{"scheme char before", "mydata:x,y", false},
		{"plus before", "x+data:x,y", false},
		{"dot before", "x.data:x,y", false},
		{"dash before", "x-data:x,y", false},
		{"plain text", "hello world", false},
		{"mediatype longer than 256 runes", "data:" + repeat("a", 257) + ",x", true},
		{"mediatype of exactly 256 runes", "data:" + repeat("a", 256) + ",x", true},
		// CSC-01: encodings a browser decodes before reading the URL.
		{"entity-encoded comma", `<img src="data:image/png;base64&#44;iVBORw0KGgo=">`, true},
		{"entity-encoded colon", `<img src="data&#58;image/png;base64,iVBOR">`, true},
		{"hex entity colon", `<img src="data&#x3A;image/png;base64,iVBOR">`, true},
		{"entity without semicolon", `<img src="data&#58image/png;base64,iVBOR">`, true},
		{"named colon entity", `<img src="data&colon;image/png;base64,iVBOR">`, true},
		{"entity-encoded letter", `<img src="&#100;ata:image/png;base64,iVBOR">`, true},
		{"newline before comma", "<img src=\"data:image/png;base64\n,iVBOR\">", true},
		{"tab inside scheme", "<img src=\"da\tta:image/png;base64,iVBOR\">", true},
		{"named tab inside scheme", `<img src="da&Tab;ta:image/png;base64,iVBOR">`, true},
		{"long mediatype parameter", "data:image/png;x=" + repeat("y", 300) + ";base64,iVBOR", true},
		{"css escape in url()", `<div style="background:url(da\74 a:image/png;base64,iVBOR)">`, true},
		{"space after colon then type", "data: image/png;base64,iVBOR", true},
		{"text data uri without type slash", "data:text,hello", true},
		{"prose with entities", "Tom &amp; Jerry: data &#58; 5 rows", false},
		{"metadata entity prose", "meta&#100;ata: x/y", false},
		// CSC-101 (rules v3): references and escapes past the v2 decoder.
		{"zero-padded decimal entity colon", `<img src="data&#0000000058;image/png;base64,iVBOR">`, true},
		{"zero-padded hex entity colon", `<img src="data&#x000000003a;image/png;base64,iVBOR">`, true},
		{"css line continuation LF", "<div style='background:url(\"da\\\nta:image/png;base64,iVBOR\")'>", true},
		{"css line continuation CRLF", "<div style='background:url(\"data\\\r\n:image/png;base64,iVBOR\")'>", true},
		{"css line continuation FF", "<div style='background:url(\"da\\\fta:image/png;base64,iVBOR\")'>", true},
		{"oversized entity is not a colon", "data&#99999999999999999999;image/png", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, types.ContainsDataURI(tt.in))
		})
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

func TestLabelBody(t *testing.T) {
	const (
		inline     = uint32(types.MediaFlag_MEDIA_FLAG_INLINE_DATA)
		compressed = uint32(types.MediaFlag_MEDIA_FLAG_COMPRESSED)
		offchain   = uint32(types.MediaFlag_MEDIA_FLAG_OFFCHAIN_REF)
	)
	tests := []struct {
		name string
		ct   types.ContentType
		body string
		want uint32
	}{
		{"plain text", types.ContentType_CONTENT_TYPE_TEXT, "hello", 0},
		{"unspecified scanned as text", types.ContentType_CONTENT_TYPE_UNSPECIFIED, "data:,x", inline},
		{"markdown with data uri", types.ContentType_CONTENT_TYPE_MARKDOWN, "![a](data:image/png;base64,AA)", inline},
		{"html with data uri", types.ContentType_CONTENT_TYPE_HTML, `<img src="data:image/png;base64,AA">`, inline},
		{"html with external url is not labelled", types.ContentType_CONTENT_TYPE_HTML, `<img src="https://aurora.example/a.png">`, 0},
		{"gzip", types.ContentType_CONTENT_TYPE_GZIP, "H4sIAAAA", compressed},
		{"zstd", types.ContentType_CONTENT_TYPE_ZSTD, "KLUv/QAA", compressed},
		{"ipfs", types.ContentType_CONTENT_TYPE_IPFS, "bafyzenith", offchain},
		{"arweave", types.ContentType_CONTENT_TYPE_ARWEAVE, "tx", offchain},
		{"filecoin", types.ContentType_CONTENT_TYPE_FILECOIN, "cid", offchain},
		{"jackal", types.ContentType_CONTENT_TYPE_JACKAL, "fid", offchain},
		{"empty body is never media", types.ContentType_CONTENT_TYPE_GZIP, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := types.LabelBody(tt.ct, tt.body)
			require.Equal(t, tt.want, l.Flags)
			require.Equal(t, types.MediaRulesVersion, l.RulesVersion)
			if tt.body == "" {
				require.Nil(t, l.BodyHash)
			} else {
				h := sha256.Sum256([]byte(tt.body))
				require.Equal(t, h[:], l.BodyHash)
			}
		})
	}
}

func TestLabelTombstone(t *testing.T) {
	l := types.LabelTombstone("[deleted]")
	require.Zero(t, l.Flags)
	require.Equal(t, types.MediaRulesVersion, l.RulesVersion)
	require.Equal(t, types.BodyHash("[deleted]"), l.BodyHash)
}

func TestLabelFederatedContent(t *testing.T) {
	require.Zero(t, types.LabelFederatedContent("<p>hi</p>", "").Flags)
	require.Equal(t, uint32(types.MediaFlag_MEDIA_FLAG_EXTERNAL_URI),
		types.LabelFederatedContent("<p>hi</p>", "https://phoenix.example/s/1").Flags)
	require.Equal(t, uint32(types.MediaFlag_MEDIA_FLAG_INLINE_DATA|types.MediaFlag_MEDIA_FLAG_EXTERNAL_URI),
		types.LabelFederatedContent(`<img src="data:image/png;base64,AA">`, "https://phoenix.example/s/1").Flags)
	require.Nil(t, types.LabelFederatedContent("x", "").BodyHash)
}

func TestCheckMediaPermitted(t *testing.T) {
	bondMin := math.NewInt(100)
	base := types.MediaGate{Flags: 1, Member: true, TrustLevel: 1, MinTrust: 1, BondMin: bondMin}
	tests := []struct {
		name string
		mut  func(g *types.MediaGate)
		ok   bool
	}{
		{"trusted member", func(*types.MediaGate) {}, true},
		{"plain text always passes", func(g *types.MediaGate) { g.Flags, g.Member, g.Anonymous = 0, false, true }, true},
		{"anonymous refused even when trusted", func(g *types.MediaGate) { g.Anonymous = true }, false},
		{"non-member refused even with bond", func(g *types.MediaGate) { g.Member, g.AuthorBond = false, bondMin }, false},
		{"low trust without bond", func(g *types.MediaGate) { g.TrustLevel = 0 }, false},
		{"low trust with small bond", func(g *types.MediaGate) { g.TrustLevel, g.AuthorBond = 0, math.NewInt(99) }, false},
		{"low trust with bond", func(g *types.MediaGate) { g.TrustLevel, g.AuthorBond = 0, bondMin }, true},
		{"bond path disabled", func(g *types.MediaGate) { g.TrustLevel, g.AuthorBond, g.BondMin = 0, bondMin, math.ZeroInt() }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := base
			tt.mut(&g)
			require.Equal(t, tt.ok, types.CheckMediaPermitted(g) == "")
		})
	}
}

func TestValidateMediaParams(t *testing.T) {
	require.NoError(t, types.ValidateMediaParams(1, math.NewInt(5), math.ZeroInt()))
	require.NoError(t, types.ValidateMediaParams(0, math.Int{}, math.Int{}), "unset Ints (zeroed by a partial params update) are valid")
	require.Error(t, types.ValidateMediaParams(5, math.ZeroInt(), math.ZeroInt()))
	require.Error(t, types.ValidateMediaParams(1, math.NewInt(-1), math.ZeroInt()))
	require.Error(t, types.ValidateMediaParams(1, math.ZeroInt(), math.NewInt(-1)))
}
