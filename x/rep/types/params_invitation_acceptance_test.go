package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestInvitationAcceptanceEpochs_Default(t *testing.T) {
	p := types.DefaultParams()
	require.Equal(t, int64(30), p.InvitationAcceptanceEpochs)
	require.NoError(t, p.Validate())
}

func TestInvitationAcceptanceEpochs_ValidateBounds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		epochs  int64
		wantErr bool
	}{
		{"zero disables expiry", 0, false},
		{"positive window", 7, false},
		{"negative is rejected", -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			p.InvitationAcceptanceEpochs = tc.epochs
			err := p.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
