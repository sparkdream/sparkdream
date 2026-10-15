package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/session/types"
)

const (
	unbondOperatorURL = "/sparkdream.service.v1.MsgUnbondOperator"
	topUpBondURL      = "/sparkdream.service.v1.MsgTopUpBond"
)

// TestDefaultParams_ExcludeForbiddenServiceMsgs pins the genesis defaults
// and the Validate guard (which genesis and MsgUpdateParams both run).
func TestDefaultParams_ExcludeForbiddenServiceMsgs(t *testing.T) {
	p := types.DefaultParams()
	require.NotContains(t, p.MaxAllowedMsgTypes, unbondOperatorURL)
	require.NotContains(t, p.MaxAllowedMsgTypes, topUpBondURL)
	require.NotContains(t, p.AllowedMsgTypes, unbondOperatorURL)
	require.NotContains(t, p.AllowedMsgTypes, topUpBondURL)
	require.NoError(t, p.Validate())

	for _, url := range []string{unbondOperatorURL, topUpBondURL, "/sparkdream.service.v1.MsgRegisterOperator"} {
		bad := types.DefaultParams()
		bad.MaxAllowedMsgTypes = append(bad.MaxAllowedMsgTypes, url)
		require.ErrorContains(t, bad.Validate(), "max_allowed_msg_types contains forbidden message", url)

		bad = types.DefaultParams()
		bad.MaxAllowedMsgTypes = append(bad.MaxAllowedMsgTypes, url)
		bad.AllowedMsgTypes = append(bad.AllowedMsgTypes, url)
		require.ErrorContains(t, bad.Validate(), "forbidden message", url)
	}
}
