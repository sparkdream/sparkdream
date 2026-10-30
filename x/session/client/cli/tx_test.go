package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseExpiry(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	got, err := parseExpiry("+720h", now)
	require.NoError(t, err)
	require.Equal(t, now.Add(720*time.Hour), got)
	got, err = parseExpiry("2027-01-31T00:00:00Z", now)
	require.NoError(t, err)
	require.Equal(t, time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC), got)
	_, err = parseExpiry("tomorrow", now)
	require.Error(t, err)
	_, err = parseExpiry("+soon", now)
	require.Error(t, err)
}

func TestCreateRecurringPullCmdShape(t *testing.T) {
	cmd := GetTxCmd()
	sub, _, err := cmd.Find([]string{"create-recurring-pull"})
	require.NoError(t, err)
	require.NotNil(t, sub.Flags().Lookup(flagMaxPerEpoch))
	require.NotNil(t, sub.Flags().Lookup(flagStartTime))
	require.Error(t, sub.Args(sub, []string{"a", "b"}))
}

func TestGenericGrantAndExecCmdShape(t *testing.T) {
	cmd := GetTxCmd()
	sub, _, err := cmd.Find([]string{"create-grant"})
	require.NoError(t, err)
	require.Error(t, sub.Args(sub, []string{"a", "b"}))
	require.NoError(t, sub.Args(sub, []string{"a", "b", "c"}))
	sub, _, err = cmd.Find([]string{"exec-session"})
	require.NoError(t, err)
	require.NoError(t, sub.Args(sub, []string{"a", "b"}))
}
