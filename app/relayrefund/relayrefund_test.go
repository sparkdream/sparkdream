package relayrefund_test

import (
	"context"
	"fmt"
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/v10/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	"sparkdream/app/relayrefund"
)

// fakeIBC: channel-1 (federation) and channel-0 (transfer) lead to the
// sister chain over connection-0 / 07-tendermint-0; channel-7 is someone
// else's, over connection-1 / 07-tendermint-1.
type fakeIBC struct {
	received   map[string]bool
	committed  map[string]bool
	ordered    map[string]uint64 // channel -> next sequence to receive
	connection map[string]string
}

func newFakeIBC() *fakeIBC {
	return &fakeIBC{
		received:   map[string]bool{},
		committed:  map[string]bool{},
		ordered:    map[string]uint64{},
		connection: map[string]string{"channel-0": "connection-0", "channel-1": "connection-0", "channel-7": "connection-1"},
	}
}

func key(port, ch string, seq uint64) string { return fmt.Sprintf("%s/%s/%d", port, ch, seq) }

func (f *fakeIBC) GetChannel(_ sdk.Context, _, ch string) (channeltypes.Channel, bool) {
	conn, ok := f.connection[ch]
	if !ok {
		return channeltypes.Channel{}, false
	}
	order := channeltypes.UNORDERED
	if _, o := f.ordered[ch]; o {
		order = channeltypes.ORDERED
	}
	return channeltypes.Channel{Ordering: order, ConnectionHops: []string{conn}}, true
}

func (f *fakeIBC) GetNextSequenceRecv(_ sdk.Context, _, ch string) (uint64, bool) {
	n, ok := f.ordered[ch]
	return n, ok
}

func (f *fakeIBC) GetPacketReceipt(_ sdk.Context, port, ch string, seq uint64) (string, bool) {
	return "", f.received[key(port, ch, seq)]
}

func (f *fakeIBC) HasPacketCommitment(_ sdk.Context, port, ch string, seq uint64) bool {
	return f.committed[key(port, ch, seq)]
}

func (f *fakeIBC) GetConnection(_ sdk.Context, id string) (connectiontypes.ConnectionEnd, bool) {
	switch id {
	case "connection-0":
		return connectiontypes.ConnectionEnd{ClientId: "07-tendermint-0"}, true
	case "connection-1":
		return connectiontypes.ConnectionEnd{ClientId: "07-tendermint-1"}, true
	}
	return connectiontypes.ConnectionEnd{}, false
}

type fakePeers map[string]bool

func (p fakePeers) IsActivePeerChannel(_ context.Context, ch string) bool { return p[ch] }

type fakeBank struct {
	sent  []sdk.Coins
	to    []sdk.AccAddress
	from  []string
	fails bool
}

func (b *fakeBank) SendCoinsFromModuleToAccount(_ context.Context, from string, to sdk.AccAddress, amt sdk.Coins) error {
	if b.fails {
		return fmt.Errorf("fee collector is short")
	}
	b.from, b.to, b.sent = append(b.from, from), append(b.to, to), append(b.sent, amt)
	return nil
}

type fakeTx struct {
	msgs  []sdk.Msg
	fee   sdk.Coins
	payer sdk.AccAddress
}

func (t fakeTx) GetMsgs() []sdk.Msg                    { return t.msgs }
func (t fakeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }
func (t fakeTx) GetGas() uint64                        { return 400_000 }
func (t fakeTx) GetFee() sdk.Coins                     { return t.fee }
func (t fakeTx) FeePayer() []byte                      { return t.payer }
func (t fakeTx) FeeGranter() []byte                    { return nil }

var relayer = sdk.AccAddress("relayer_____________")

func recv(ch string, seq uint64) *channeltypes.MsgRecvPacket {
	return &channeltypes.MsgRecvPacket{Packet: channeltypes.Packet{
		Sequence: seq, SourcePort: "transfer", SourceChannel: "channel-9", DestinationPort: "transfer", DestinationChannel: ch,
	}}
}

func sent(seq uint64) channeltypes.Packet {
	return channeltypes.Packet{
		Sequence: seq, SourcePort: "federation", SourceChannel: "channel-1", DestinationPort: "federation", DestinationChannel: "channel-4",
	}
}

func update(client string) *clienttypes.MsgUpdateClient {
	return &clienttypes.MsgUpdateClient{ClientId: client}
}

// run passes tx through the ante decorator and the post handler, and
// returns what the bank was asked to refund.
func run(t *testing.T, ibc *fakeIBC, tx fakeTx, mode sdk.ExecMode, simulate, success bool) *fakeBank {
	t.Helper()
	ctx := newCtx(t, mode)
	bank := &fakeBank{}
	marks := relayrefund.NewMarks()
	ante := sdk.ChainAnteDecorators(relayrefund.NewFreshRelayDecorator(
		func() relayrefund.IBCState { return ibc },
		fakePeers{"channel-0": true, "channel-1": true},
		marks,
	))
	post := sdk.ChainPostDecorators(relayrefund.NewRefundDecorator(bank, marks))
	_, err := ante(ctx, tx, simulate)
	require.NoError(t, err)
	// baseapp runs the post handler on its own context, not necessarily the
	// one the ante chain returned
	_, err = post(ctx, tx, simulate, success)
	require.NoError(t, err)
	return bank
}

func newCtx(t *testing.T, mode sdk.ExecMode) sdk.Context {
	t.Helper()
	key := storetypes.NewKVStoreKey("relayrefund_" + t.Name())
	return testutil.DefaultContext(key, storetypes.NewTransientStoreKey("t_"+t.Name())).
		WithExecMode(mode).WithBlockHeight(10).WithTxBytes([]byte("tx-" + t.Name()))
}

func tx(msgs ...sdk.Msg) fakeTx {
	return fakeTx{msgs: msgs, fee: sdk.NewCoins(sdk.NewInt64Coin("uspark", 10_000)), payer: relayer}
}

func TestRefundsFreshRelaysOnPeerChannels(t *testing.T) {
	ibc := newFakeIBC()
	ibc.committed[key("federation", "channel-1", 3)] = true
	ibc.committed[key("federation", "channel-1", 4)] = true

	for name, tc := range map[string]fakeTx{
		"receive with its client update": tx(update("07-tendermint-0"), recv("channel-0", 5)),
		"batch of receives":              tx(update("07-tendermint-0"), recv("channel-0", 5), recv("channel-1", 6)),
		"ack":                            tx(update("07-tendermint-0"), &channeltypes.MsgAcknowledgement{Packet: sent(3)}),
		"timeout":                        tx(&channeltypes.MsgTimeout{Packet: sent(4)}),
		"timeout on close":               tx(&channeltypes.MsgTimeoutOnClose{Packet: sent(4)}),
	} {
		t.Run(name, func(t *testing.T) {
			bank := run(t, ibc, tc, sdk.ExecModeFinalize, false, true)
			require.Len(t, bank.sent, 1)
			require.Equal(t, authtypes.FeeCollectorName, bank.from[0])
			require.Equal(t, relayer, bank.to[0])
			require.Equal(t, tc.fee, bank.sent[0])
		})
	}
}

func TestNoRefund(t *testing.T) {
	ibc := newFakeIBC()
	ibc.received[key("transfer", "channel-0", 5)] = true // already delivered
	ibc.committed[key("federation", "channel-1", 3)] = true
	ibc.ordered["channel-1"] = 10

	for name, tc := range map[string]fakeTx{
		"already received":               tx(recv("channel-0", 5)),
		"one of two already received":    tx(recv("channel-0", 5), recv("channel-0", 6)),
		"the same packet twice":          tx(recv("channel-0", 6), recv("channel-0", 6)),
		"ack and timeout of one packet":  tx(&channeltypes.MsgAcknowledgement{Packet: sent(3)}, &channeltypes.MsgTimeout{Packet: sent(3)}),
		"ordered channel, sequence past": tx(recv("channel-1", 9)),
		"ack already processed":          tx(&channeltypes.MsgAcknowledgement{Packet: sent(8)}),
		"not a peer's channel":           tx(recv("channel-7", 1)),
		"unknown channel":                tx(recv("channel-8", 1)),
		"bare client update":             tx(update("07-tendermint-0")),
		"someone else's client":          tx(update("07-tendermint-1"), recv("channel-0", 6)),
		"a non-relay message along":      tx(recv("channel-0", 6), &authtypes.MsgUpdateParams{}),
		"zero fee":                       {msgs: []sdk.Msg{recv("channel-0", 6)}, payer: relayer},
	} {
		t.Run(name, func(t *testing.T) {
			require.Empty(t, run(t, ibc, tc, sdk.ExecModeFinalize, false, true).sent)
		})
	}

	// an ordered channel's next sequence is fresh
	require.Len(t, run(t, ibc, tx(recv("channel-1", 10)), sdk.ExecModeFinalize, false, true).sent, 1)

	fresh := tx(recv("channel-0", 6))
	// a failed tx (bad proof, say) pays its fee
	require.Empty(t, run(t, ibc, fresh, sdk.ExecModeFinalize, false, false).sent)
	// only a block's own execution refunds: never the mempool, never a simulation
	require.Empty(t, run(t, ibc, fresh, sdk.ExecModeCheck, false, true).sent)
	require.Empty(t, run(t, ibc, fresh, sdk.ExecModeReCheck, false, true).sent)
	require.Empty(t, run(t, ibc, fresh, sdk.ExecModeFinalize, true, true).sent)
}

// A refund the fee collector cannot pay leaves the relay standing.
func TestRefundFailureDoesNotFailTheTx(t *testing.T) {
	ctx := newCtx(t, sdk.ExecModeFinalize)
	marks := relayrefund.NewMarks()
	ante := sdk.ChainAnteDecorators(relayrefund.NewFreshRelayDecorator(
		func() relayrefund.IBCState { return newFakeIBC() }, fakePeers{"channel-0": true}, marks,
	))
	post := sdk.ChainPostDecorators(relayrefund.NewRefundDecorator(&fakeBank{fails: true}, marks))
	_, err := ante(ctx, tx(recv("channel-0", 1)), false)
	require.NoError(t, err)
	_, err = post(ctx, tx(recv("channel-0", 1)), false, true)
	require.NoError(t, err)
}

// Before the app has built its IBC keeper, nothing is eligible.
func TestNoIBCKeeperYet(t *testing.T) {
	ctx := newCtx(t, sdk.ExecModeFinalize)
	marks := relayrefund.NewMarks()
	ante := sdk.ChainAnteDecorators(relayrefund.NewFreshRelayDecorator(
		func() relayrefund.IBCState { return nil }, fakePeers{"channel-0": true}, marks,
	))
	_, err := ante(ctx, tx(recv("channel-0", 1)), false)
	require.NoError(t, err)
	require.False(t, marks.Take(ctx))
}

// zeroCtxDecorator hands next a zero context, as the gnovm ante does for
// every non-Gno tx; baseapp then drops back to its pre-ante context.
type zeroCtxDecorator struct{}

func (zeroCtxDecorator) AnteHandle(_ sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	return next(sdk.Context{}, tx, simulate)
}

// The mark survives a decorator after it that discards the context: the
// regression that kept every refund from paying on a live chain.
func TestMarkSurvivesAZeroedContext(t *testing.T) {
	ctx := newCtx(t, sdk.ExecModeFinalize)
	marks := relayrefund.NewMarks()
	bank := &fakeBank{}
	ante := sdk.ChainAnteDecorators(
		relayrefund.NewFreshRelayDecorator(func() relayrefund.IBCState { return newFakeIBC() }, fakePeers{"channel-0": true}, marks),
		zeroCtxDecorator{},
	)
	returned, err := ante(ctx, tx(recv("channel-0", 1)), false)
	require.NoError(t, err)
	require.True(t, returned.IsZero())
	_, err = sdk.ChainPostDecorators(relayrefund.NewRefundDecorator(bank, marks))(ctx, tx(recv("channel-0", 1)), false, true)
	require.NoError(t, err)
	require.Len(t, bank.sent, 1)
}

// A mark is used once, belongs to its own tx, and does not outlive its block.
func TestMarksAreOneShotPerTxAndBlock(t *testing.T) {
	ctx := newCtx(t, sdk.ExecModeFinalize)
	marks := relayrefund.NewMarks()
	ante := sdk.ChainAnteDecorators(relayrefund.NewFreshRelayDecorator(
		func() relayrefund.IBCState { return newFakeIBC() }, fakePeers{"channel-0": true}, marks,
	))
	_, err := ante(ctx, tx(recv("channel-0", 1)), false)
	require.NoError(t, err)
	require.False(t, marks.Take(ctx.WithTxBytes([]byte("another tx"))))
	require.True(t, marks.Take(ctx))
	require.False(t, marks.Take(ctx))

	_, err = ante(ctx, tx(recv("channel-0", 1)), false)
	require.NoError(t, err)
	require.False(t, marks.Take(ctx.WithBlockHeight(11)))
}
