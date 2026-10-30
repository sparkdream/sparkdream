package simulation

import (
	"fmt"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/futarchy/keeper"
	"sparkdream/x/futarchy/types"
	reptypes "sparkdream/x/rep/types"
)

// simQuestions are the questions simulated markets ask.
var simQuestions = []string{
	"Will the phoenix initiative ship this season?",
	"Will aurora reach its staking target?",
	"Will the zenith project pass review?",
	"Will the nebula guild double its members?",
}

// buildMarket builds a MsgCreateMarket from acc: liquidity between
// min_liquidity and a fifth of its SPARK (at most 100x min_liquidity), and
// an end block 5 to 40 blocks out, so a 200-block run trades, resolves and
// redeems it. The liquidity is the SPARK the handler takes. ok is false
// when acc cannot fund one.
func (e *simEnv) buildMarket(acc simtypes.Account) (*types.MsgCreateMarket, math.Int, bool) {
	minLiq := e.params.MinLiquidity
	if minLiq.IsNil() || !minLiq.IsPositive() {
		minLiq = math.OneInt()
	}
	hi := math.MinInt(e.spendable(acc).QuoRaw(5), minLiq.MulRaw(100))
	if hi.LT(minLiq) {
		return nil, math.ZeroInt(), false
	}
	liquidity := minLiq
	if span := hi.Sub(minLiq); span.IsPositive() {
		if extra, err := simtypes.RandPositiveInt(e.r, span); err == nil {
			liquidity = liquidity.Add(extra)
		}
	}
	duration := min(5+e.r.Int63n(36), max(e.params.MaxDuration, 1))
	question, _ := pick(e.r, simQuestions)
	return &types.MsgCreateMarket{
		Creator:          acc.Address.String(),
		Symbol:           fmt.Sprintf("SIM%d", e.ctx.BlockHeight()*100+e.r.Int63n(100)),
		InitialLiquidity: &liquidity,
		Question:         question,
		EndBlock:         e.ctx.BlockHeight() + duration,
	}, liquidity, true
}

// creators returns the live sim accounts the create handler accepts:
// active x/rep members at ESTABLISHED trust or above.
func (e *simEnv) creators() []simtypes.Account {
	return e.live(func(a simtypes.Account) bool {
		return e.activeMember(a.Address.String(), reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	})
}

// createMarket has a random eligible member create a market (dry-run
// first). ok is false when none could.
func (e *simEnv) createMarket() (simtypes.OperationMsg, bool, string, error) {
	reason := "no live ESTABLISHED+ member"
	for _, acc := range e.shuffled(e.creators(), 3) {
		msg, liquidity, ok := e.buildMarket(acc)
		if !ok {
			reason = "no ESTABLISHED+ member can fund min_liquidity"
			continue
		}
		spent := e.sparkCoins(liquidity)
		if why := e.usable(acc, spent); why != "" {
			reason = why
			continue
		}
		if err := e.dryRun(msg); err != nil {
			reason = "handler would reject: " + err.Error()
			continue
		}
		opMsg, _, err := e.deliver(acc, msg, spent)
		return opMsg, err == nil && opMsg.OK, "", err
	}
	return simtypes.OperationMsg{}, false, reason, nil
}

// shuffled returns up to n accounts of accs in random order.
func (e *simEnv) shuffled(accs []simtypes.Account, n int) []simtypes.Account {
	out := make([]simtypes.Account, 0, n)
	for _, i := range e.r.Perm(len(accs)) {
		if len(out) == n {
			break
		}
		out = append(out, accs[i])
	}
	return out
}

// SimulateMsgCreateMarket has an ESTABLISHED+ member open a market with
// SPARK liquidity (the handler's CoinsSpentInMsg).
func SimulateMsgCreateMarket(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	label := &types.MsgCreateMarket{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		opMsg, ok, reason, err := e.createMarket()
		if err != nil || ok {
			return opMsg, nil, err
		}
		return noOp(label, reason)
	})
}
