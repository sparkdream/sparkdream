// Package relayrefund pays back the fee of a successful, non-redundant IBC
// relay between this chain and a sister Spark Dream chain.
//
// A relayer signs with a raw key on a host it usually does not control
// (Hermes cannot sign through an x/session key), so whatever that key holds
// is at the host's mercy. Refunding the relays this chain wants carried
// means the key only needs a float of a few fees: it pays each one up front
// and gets it back when the tx lands, and a stolen key yields that float and
// nothing more. ICS-29 fee middleware, the usual answer, is gone from
// ibc-go v10.
//
// A tx is refunded when all of these hold:
//
//   - every message is MsgUpdateClient, MsgRecvPacket, MsgAcknowledgement,
//     MsgTimeout or MsgTimeoutOnClose (IBC v1);
//   - it carries at least one packet message, and each packet is still
//     undelivered when the tx starts executing (no receipt / next-sequence
//     past it for a receive; the commitment still there for an ack or
//     timeout), and no packet appears twice;
//   - each packet travels on this chain's end of an ACTIVE Spark Dream
//     peer's federation or transfer channel (x/federation), and every client
//     update is for a client under one of those packets' connections;
//   - the tx succeeds.
//
// Freshness is judged before execution by plain reads, so it costs no proof
// verification; validity is judged by execution itself: a bad proof fails
// the tx, and a failed tx is never refunded (its post-handler writes are
// discarded along with its messages). Two relayers racing the same packet
// in one block: the second tx's ante runs after the first executed, sees
// the packet delivered, and pays its fee. Anyone can relay and be refunded;
// the peer-channel rule is what keeps it from being free block space: a
// packet on a peer channel was sent, and paid for, by a user on a chain this
// one has chosen to federate with.
//
// Mempool side, RedundantRelayDecorator (ibc-go's own) turns away a tx whose
// packets are all already delivered in CheckTx, before it costs anyone.
package relayrefund

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/v10/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	ibcante "github.com/cosmos/ibc-go/v10/modules/core/ante"
	ibckeeper "github.com/cosmos/ibc-go/v10/modules/core/keeper"
)

// EventTypeRelayFeeRefund is emitted for every refunded relay tx.
const (
	EventTypeRelayFeeRefund = "relay_fee_refund"
	AttributeKeyRelayer     = "relayer"
	AttributeKeyAmount      = "amount"
)

// IBCKeeperFn returns the IBC keeper. The app builds the IBC keeper after
// the ante handler, so the decorators resolve it per tx.
type IBCKeeperFn func() *ibckeeper.Keeper

// IBCState is the IBC state the refund reads: plain lookups, no proofs.
type IBCState interface {
	GetChannel(ctx sdk.Context, portID, channelID string) (channeltypes.Channel, bool)
	GetNextSequenceRecv(ctx sdk.Context, portID, channelID string) (uint64, bool)
	GetPacketReceipt(ctx sdk.Context, portID, channelID string, sequence uint64) (string, bool)
	HasPacketCommitment(ctx sdk.Context, portID, channelID string, sequence uint64) bool
	GetConnection(ctx sdk.Context, connectionID string) (connectiontypes.ConnectionEnd, bool)
}

// keeperState reads IBCState off the IBC keeper.
type keeperState struct{ k *ibckeeper.Keeper }

func (s keeperState) GetChannel(ctx sdk.Context, port, ch string) (channeltypes.Channel, bool) {
	return s.k.ChannelKeeper.GetChannel(ctx, port, ch)
}

func (s keeperState) GetNextSequenceRecv(ctx sdk.Context, port, ch string) (uint64, bool) {
	return s.k.ChannelKeeper.GetNextSequenceRecv(ctx, port, ch)
}

func (s keeperState) GetPacketReceipt(ctx sdk.Context, port, ch string, seq uint64) (string, bool) {
	return s.k.ChannelKeeper.GetPacketReceipt(ctx, port, ch, seq)
}

func (s keeperState) HasPacketCommitment(ctx sdk.Context, port, ch string, seq uint64) bool {
	return s.k.ChannelKeeper.HasPacketCommitment(ctx, port, ch, seq)
}

func (s keeperState) GetConnection(ctx sdk.Context, id string) (connectiontypes.ConnectionEnd, bool) {
	return s.k.ConnectionKeeper.GetConnection(ctx, id)
}

// KeeperState adapts an IBC keeper getter to IBCState (nil until built).
func KeeperState(fn IBCKeeperFn) func() IBCState {
	return func() IBCState {
		if k := fn(); k != nil {
			return keeperState{k}
		}
		return nil
	}
}

// PeerChannels answers whether a channel belongs to an ACTIVE Spark Dream
// federation peer (x/federation keeper).
type PeerChannels interface {
	IsActivePeerChannel(ctx context.Context, channelID string) bool
}

// BankKeeper moves the refund out of the fee collector.
type BankKeeper interface {
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
}

// Marks carries "this tx is eligible" from the ante decorator to the post
// handler, keyed by the tx bytes' hash within the block being executed.
//
// Not a context value: the ante chain's returned context does not reliably
// reach the post handler. A decorator that hands a zero sdk.Context to next
// (the gnovm ante does, for every non-Gno tx) makes baseapp fall back to its
// pre-ante context, dropping every value set in the chain. Block execution
// is sequential and identical on every node, so an in-memory set is
// deterministic; a node that restarts mid-block replays the block and
// rebuilds it.
type Marks struct {
	mu     sync.Mutex
	height int64
	set    map[[32]byte]bool
}

func NewMarks() *Marks { return &Marks{set: map[[32]byte]bool{}} }

func (m *Marks) mark(ctx sdk.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rollover(ctx)
	m.set[sha256.Sum256(ctx.TxBytes())] = true
}

// Take reports whether the tx being executed was marked, and forgets it.
func (m *Marks) Take(ctx sdk.Context) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rollover(ctx)
	key := sha256.Sum256(ctx.TxBytes())
	ok := m.set[key]
	delete(m.set, key)
	return ok
}

// rollover drops marks left by an earlier block (a tx whose ante failed
// after the mark never reaches the post handler).
func (m *Marks) rollover(ctx sdk.Context) {
	if ctx.BlockHeight() != m.height {
		m.height = ctx.BlockHeight()
		m.set = map[[32]byte]bool{}
	}
}

// FreshRelayDecorator marks a finalized tx eligible for a refund (see the
// package comment). It never rejects anything; RefundDecorator pays.
type FreshRelayDecorator struct {
	ibc   func() IBCState
	peers PeerChannels
	marks *Marks
}

func NewFreshRelayDecorator(ibc func() IBCState, peers PeerChannels, marks *Marks) FreshRelayDecorator {
	return FreshRelayDecorator{ibc: ibc, peers: peers, marks: marks}
}

func (d FreshRelayDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// only a block's own execution refunds: a CheckTx refund would let the
	// mempool spend money DeliverTx may never give back
	if !simulate && ctx.ExecMode() == sdk.ExecModeFinalize && d.eligible(ctx, tx) {
		d.marks.mark(ctx)
	}
	return next(ctx, tx, simulate)
}

func (d FreshRelayDecorator) eligible(ctx sdk.Context, tx sdk.Tx) bool {
	k := d.ibc()
	if k == nil || d.peers == nil {
		return false
	}
	packets := 0
	seen := map[string]bool{}
	clients := map[string]bool{}
	var updates []string

	// ourEnd checks the packet's channel on this chain and records the client
	// under it; false when it is not an active peer's
	ourEnd := func(port, channel string) bool {
		if !d.peers.IsActivePeerChannel(ctx, channel) {
			return false
		}
		ch, found := k.GetChannel(ctx, port, channel)
		if !found || len(ch.ConnectionHops) == 0 {
			return false
		}
		conn, found := k.GetConnection(ctx, ch.ConnectionHops[0])
		if !found {
			return false
		}
		clients[conn.ClientId] = true
		return true
	}
	once := func(key string) bool {
		if seen[key] {
			return false
		}
		seen[key] = true
		return true
	}

	for _, m := range tx.GetMsgs() {
		switch msg := m.(type) {
		case *clienttypes.MsgUpdateClient:
			updates = append(updates, msg.ClientId)

		case *channeltypes.MsgRecvPacket:
			p := msg.Packet
			if !ourEnd(p.DestinationPort, p.DestinationChannel) || !recvFresh(ctx, k, p) ||
				!once(fmt.Sprintf("dst/%s/%s/%d", p.DestinationPort, p.DestinationChannel, p.Sequence)) {
				return false
			}
			packets++

		case *channeltypes.MsgAcknowledgement:
			if !d.sentFresh(ctx, k, msg.Packet, ourEnd, once) {
				return false
			}
			packets++
		case *channeltypes.MsgTimeout:
			if !d.sentFresh(ctx, k, msg.Packet, ourEnd, once) {
				return false
			}
			packets++
		case *channeltypes.MsgTimeoutOnClose:
			if !d.sentFresh(ctx, k, msg.Packet, ourEnd, once) {
				return false
			}
			packets++

		default:
			// anything else rides at its own cost
			return false
		}
	}
	if packets == 0 {
		// a bare client update refreshes a client without carrying anything
		return false
	}
	for _, id := range updates {
		if !clients[id] {
			return false
		}
	}
	return true
}

// recvFresh: the packet has not been received on this end yet.
func recvFresh(ctx sdk.Context, k IBCState, p channeltypes.Packet) bool {
	ch, found := k.GetChannel(ctx, p.DestinationPort, p.DestinationChannel)
	if !found {
		return false
	}
	if ch.Ordering == channeltypes.ORDERED {
		next, found := k.GetNextSequenceRecv(ctx, p.DestinationPort, p.DestinationChannel)
		return found && p.Sequence >= next
	}
	_, received := k.GetPacketReceipt(ctx, p.DestinationPort, p.DestinationChannel, p.Sequence)
	return !received
}

// sentFresh: a packet this chain sent still awaits its ack or timeout.
func (d FreshRelayDecorator) sentFresh(
	ctx sdk.Context,
	k IBCState,
	p channeltypes.Packet,
	ourEnd func(port, channel string) bool,
	once func(key string) bool,
) bool {
	return ourEnd(p.SourcePort, p.SourceChannel) &&
		k.HasPacketCommitment(ctx, p.SourcePort, p.SourceChannel, p.Sequence) &&
		once(fmt.Sprintf("src/%s/%s/%d", p.SourcePort, p.SourceChannel, p.Sequence))
}

// RefundDecorator is the post handler that returns an eligible tx's fee
// to its payer, out of the fee collector (where the ante put it, and where
// it sits until the next block's distribution).
type RefundDecorator struct {
	bank  BankKeeper
	marks *Marks
}

func NewRefundDecorator(bank BankKeeper, marks *Marks) RefundDecorator {
	return RefundDecorator{bank: bank, marks: marks}
}

func (d RefundDecorator) PostHandle(ctx sdk.Context, tx sdk.Tx, simulate, success bool, next sdk.PostHandler) (sdk.Context, error) {
	marked := !simulate && ctx.ExecMode() == sdk.ExecModeFinalize && d.marks.Take(ctx)
	if success && marked {
		if feeTx, ok := tx.(sdk.FeeTx); ok {
			fee := feeTx.GetFee()
			if !fee.IsZero() {
				payer := sdk.AccAddress(feeTx.FeePayer())
				// a refund that cannot be paid leaves the relay standing: the
				// relayer is out a fee, nobody is out a packet
				if err := d.bank.SendCoinsFromModuleToAccount(ctx, authtypes.FeeCollectorName, payer, fee); err != nil {
					ctx.Logger().Error("relay fee refund failed", "relayer", payer.String(), "fee", fee.String(), "err", err)
				} else {
					ctx.EventManager().EmitEvent(sdk.NewEvent(
						EventTypeRelayFeeRefund,
						sdk.NewAttribute(AttributeKeyRelayer, payer.String()),
						sdk.NewAttribute(AttributeKeyAmount, fee.String()),
					))
				}
			}
		}
	}
	return next(ctx, tx, simulate, success)
}

// RedundantRelayDecorator wraps ibc-go's mempool check, resolving the IBC
// keeper per tx (it does not exist yet when the ante handler is built).
type RedundantRelayDecorator struct {
	ibc IBCKeeperFn
}

func NewRedundantRelayDecorator(ibc IBCKeeperFn) RedundantRelayDecorator {
	return RedundantRelayDecorator{ibc: ibc}
}

func (d RedundantRelayDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	k := d.ibc()
	if k == nil {
		return next(ctx, tx, simulate)
	}
	return ibcante.NewRedundantRelayDecorator(k).AnteHandle(ctx, tx, simulate, next)
}
