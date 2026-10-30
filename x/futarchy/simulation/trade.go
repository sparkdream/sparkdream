package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/futarchy/keeper"
	"sparkdream/x/futarchy/types"
	reptypes "sparkdream/x/rep/types"
)

// tradeable reports whether a market takes trades now: ACTIVE, before its
// end block, with its LMSR fields set.
func (e *simEnv) tradeable(m types.Market) bool {
	return m.Status == "ACTIVE" && e.ctx.BlockHeight() < m.EndBlock &&
		m.BValue != nil && m.PoolYes != nil && m.PoolNo != nil && m.MinTick != nil
}

// traders returns the live sim accounts that hold SPARK: active members
// first (the community the markets are for), anyone holding SPARK when no
// member can trade.
func (e *simEnv) traders(minSpark math.Int) []simtypes.Account {
	funded := func(a simtypes.Account) bool { return e.spendable(a).GT(minSpark) }
	members := e.live(func(a simtypes.Account) bool {
		return funded(a) && e.activeMember(a.Address.String(), reptypes.TrustLevel_TRUST_LEVEL_NEW)
	})
	if len(members) > 0 {
		return members
	}
	return e.live(funded)
}

// trade builds, dry-runs and delivers a trade on market m by a random
// trader. The amount sits between the market's min tick and the smaller of
// a quarter of the trader's SPARK and half the LMSR b; a trade the handler
// refuses (price pushed past max_lmsr_exponent, too small after the fee)
// is retried at a smaller size. done is false when no trade fits.
func (e *simEnv) trade(m types.Market) (simtypes.OperationMsg, bool, error) {
	minTick := *m.MinTick
	if !minTick.IsPositive() {
		minTick = math.OneInt()
	}
	for _, acc := range e.shuffled(e.traders(minTick.MulRaw(4)), 3) {
		hi := math.MinInt(e.spendable(acc).QuoRaw(4), m.BValue.QuoInt64(2).TruncateInt())
		if hi.LT(minTick) {
			continue
		}
		amount := minTick
		if span := hi.Sub(minTick); span.IsPositive() {
			if extra, err := simtypes.RandPositiveInt(e.r, span); err == nil {
				amount = amount.Add(extra)
			}
		}
		isYes := e.r.Intn(2) == 0
		for attempt := 0; attempt < 3 && amount.GTE(minTick); attempt++ {
			amt := amount
			msg := &types.MsgTrade{Creator: acc.Address.String(), MarketId: m.Index, IsYes: isYes, AmountIn: &amt}
			spent := e.sparkCoins(amt)
			if e.usable(acc, spent) == "" && e.dryRun(msg) == nil {
				opMsg, _, err := e.deliver(acc, msg, spent)
				return opMsg, true, err
			}
			amount = amount.QuoRaw(4)
		}
	}
	return simtypes.OperationMsg{}, false, nil
}

// SimulateMsgTrade buys YES or NO shares on an active market (the amount in
// is the tx's CoinsSpentInMsg). Markets include the x/commons
// confidence-vote markets. When no market takes trades, the op first has an
// ESTABLISHED+ member create one with a real MsgCreateMarket, then trades
// on it.
func SimulateMsgTrade(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	label := &types.MsgTrade{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		open, err := e.markets(e.tradeable)
		if err != nil {
			return noOpErr(label, err)
		}
		for _, i := range e.r.Perm(len(open)) {
			opMsg, done, err := e.trade(open[i])
			if err != nil || done {
				return opMsg, nil, err
			}
		}

		// Prerequisite: a market to trade on.
		created, ok, reason, err := e.createMarket()
		if err != nil {
			return created, nil, err
		}
		if !ok {
			return noOp(label, "no active market and none could be created: "+reason)
		}
		open, err = e.markets(e.tradeable)
		if err != nil {
			return noOpErr(label, err)
		}
		if len(open) > 0 {
			// The newest market is the one just created.
			opMsg, done, err := e.trade(open[len(open)-1])
			if err != nil || done {
				return opMsg, nil, err
			}
		}
		created.Comment = "created a market; no trader fits it yet"
		return created, nil, nil
	})
}
