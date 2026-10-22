package app

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseGnoString(t *testing.T) {
	doc := `{"name":"Zenith #1","description":"a \"quoted\" (paren) string)"}`
	got, err := parseGnoString("(" + strconv.Quote(doc) + " string)")
	require.NoError(t, err)
	require.Equal(t, doc, got)

	got, err = parseGnoString(`("" string)`)
	require.NoError(t, err)
	require.Equal(t, "", got)

	for _, bad := range []string{
		``,
		`(42 int)`,
		`("a" string)` + "\n" + `("b" string)`,
		`("unterminated string)`,
		`"bare"`,
	} {
		_, err := parseGnoString(bad)
		require.Error(t, err, bad)
	}
}
