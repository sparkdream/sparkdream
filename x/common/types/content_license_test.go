package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/common/types"
)

func TestIsUnencumberedLicense(t *testing.T) {
	for _, tc := range []struct {
		license string
		want    bool
	}{
		{"CC0-1.0", true},
		{"PDM-1.0", true},
		{"", false},
		{"cc0-1.0", false}, // ids are exact
		{"CC-BY-4.0", false},
		{"CC-BY-SA-4.0", false},
		{"Apache-2.0", false},
	} {
		require.Equal(t, tc.want, types.IsUnencumberedLicense(tc.license), tc.license)
	}
}

func TestChainContentLicenseIsUnencumbered(t *testing.T) {
	require.True(t, types.IsUnencumberedLicense(types.ChainContentLicense))
}

func TestUnencumberedLicensesIsACopy(t *testing.T) {
	l := types.UnencumberedLicenses()
	l[0] = "CC-BY-4.0"
	require.False(t, types.IsUnencumberedLicense("CC-BY-4.0"))
}

func TestIsOpenCodeLicense(t *testing.T) {
	for _, l := range []string{"CC0-1.0", "MIT", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "ISC", "0BSD", "Unlicense"} {
		require.True(t, types.IsOpenCodeLicense(l), l)
	}
	for _, l := range []string{"", "GPL-3.0", "AGPL-3.0", "MPL-2.0", "BSL-1.1", "Proprietary", "mit", "PDM-1.0"} {
		require.False(t, types.IsOpenCodeLicense(l), l)
	}
}
