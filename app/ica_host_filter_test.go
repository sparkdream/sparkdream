package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	shieldtypes "sparkdream/x/shield/types"
)

// recordingRouter hands out a handler for every msg and remembers which types
// were asked for, so the test can tell "filtered out" from "no handler".
type recordingRouter struct{ asked []string }

func (r *recordingRouter) Handler(msg sdk.Msg) baseapp.MsgServiceHandler {
	r.asked = append(r.asked, sdk.MsgTypeURL(msg))
	return func(sdk.Context, sdk.Msg) (*sdk.Result, error) { return &sdk.Result{}, nil }
}

// TestICAHostRouterCeiling pins the ICA host ceiling (APP-01): an allowed
// type reaches the inner router, anything else gets no handler without the
// inner router being consulted, whatever the host's allow_messages says.
func TestICAHostRouterCeiling(t *testing.T) {
	inner := &recordingRouter{}
	r := newICAHostRouter(inner)

	for _, allowed := range []sdk.Msg{&banktypes.MsgSend{}, &stakingtypes.MsgDelegate{}} {
		require.NotNil(t, r.Handler(allowed), "%s is on the ceiling", sdk.MsgTypeURL(allowed))
	}
	require.ElementsMatch(t, []string{sdk.MsgTypeURL(&banktypes.MsgSend{}), sdk.MsgTypeURL(&stakingtypes.MsgDelegate{})}, inner.asked)

	inner.asked = nil
	for _, denied := range []sdk.Msg{
		&shieldtypes.MsgShieldedExec{}, // skips every ante rule the host already skips
		&banktypes.MsgMultiSend{},
		&stakingtypes.MsgCreateValidator{},
	} {
		require.Nil(t, r.Handler(denied), "%s is outside the ceiling", sdk.MsgTypeURL(denied))
	}
	require.Empty(t, inner.asked, "a denied type never reaches the inner router")

	require.Len(t, ICAHostAllowedMsgs, 7, "widening the ICA ceiling needs a review of every ante-only rule the new type touches")
}

// TestNetworkICAHostAllowMessagesInsideCeiling checks that no shipped genesis
// lists an ICA host allow_messages entry the compiled ceiling would refuse
// anyway (a "*" or a stray type is a config lie, not a widening).
func TestNetworkICAHostAllowMessagesInsideCeiling(t *testing.T) {
	ceiling := map[string]bool{}
	for _, m := range ICAHostAllowedMsgs {
		ceiling[m] = true
	}
	for _, network := range []string{"devnet", "testnet", "mainnet"} {
		t.Run(network, func(t *testing.T) {
			genFile := filepath.Join("..", "deploy", "config", "network", network, "genesis.json")
			var doc struct {
				AppState struct {
					ICA struct {
						Host struct {
							Params struct {
								AllowMessages []string `json:"allow_messages"`
							} `json:"params"`
						} `json:"host_genesis_state"`
					} `json:"interchainaccounts"`
				} `json:"app_state"`
			}
			raw, err := os.ReadFile(genFile)
			if err != nil {
				t.Skipf("%s not yet present: %v", genFile, err)
			}
			require.NoError(t, json.Unmarshal(raw, &doc), genFile)
			allow := doc.AppState.ICA.Host.Params.AllowMessages
			require.NotEmpty(t, allow, "%s: ICA host allow_messages missing", genFile)
			for _, m := range allow {
				require.True(t, ceiling[m], "%s: ICA host allow_messages lists %q, outside app.ICAHostAllowedMsgs", genFile, m)
			}
		})
	}
}
