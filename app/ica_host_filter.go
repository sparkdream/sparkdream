package app

import (
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	icatypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/types"
)

// ICAHostAllowedMsgs is the compiled ceiling of message types an interchain
// account hosted on this chain may execute. The ICA host dispatches packet
// msgs straight through the router as the interchain-account address, so no
// ante decorator runs on them (tombstone backstop, commons proposal-fee
// floor, gnovm no-mix rule, session and shield fee accounting). Only plain
// token and staking operations, whose rules all live in their msg servers,
// are admitted. The host's `allow_messages` param (gov-controlled, outside
// x/guardian) can only narrow this list: a type outside it is refused even
// if the param says "*". Widening it needs a chain upgrade and a review of
// every ante-only rule the new type touches.
var ICAHostAllowedMsgs = []string{
	"/cosmos.bank.v1beta1.MsgSend",
	"/cosmos.staking.v1beta1.MsgDelegate",
	"/cosmos.staking.v1beta1.MsgUndelegate",
	"/cosmos.staking.v1beta1.MsgBeginRedelegate",
	"/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward",
	"/cosmos.distribution.v1beta1.MsgSetWithdrawAddress",
	"/ibc.applications.transfer.v1.MsgTransfer",
}

// icaHostRouter wraps the app's msg router for the ICA host and returns no
// handler for any type outside ICAHostAllowedMsgs, which the host turns into
// a failed packet ("no message handler found").
type icaHostRouter struct {
	inner   icatypes.MessageRouter
	allowed map[string]bool
}

var _ icatypes.MessageRouter = icaHostRouter{}

func newICAHostRouter(inner icatypes.MessageRouter) icaHostRouter {
	allowed := make(map[string]bool, len(ICAHostAllowedMsgs))
	for _, t := range ICAHostAllowedMsgs {
		allowed[t] = true
	}
	return icaHostRouter{inner: inner, allowed: allowed}
}

func (r icaHostRouter) Handler(msg sdk.Msg) baseapp.MsgServiceHandler {
	if !r.allowed[sdk.MsgTypeURL(msg)] {
		return nil
	}
	return r.inner.Handler(msg)
}
