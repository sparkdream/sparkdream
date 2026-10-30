package simulation

import (
	"fmt"
	"time"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	blogtypes "sparkdream/x/blog/types"
	commontypes "sparkdream/x/common/types"
	seasontypes "sparkdream/x/season/types"
	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// grantKind is one MsgCreateGrant payload variant the simulation builds.
type grantKind int

const (
	kindSessionKey grantKind = iota
	kindRecurringPull
	kindAllowance
	kindOneshotTransfer
	kindOneshotExec
	kindSignIn
	numGrantKinds
)

func (g grantKind) String() string {
	return [...]string{"session_key", "recurring_pull", "spending_allowance", "oneshot_transfer", "oneshot_exec", "sign_in"}[g]
}

// grantType is the stored grant type of a kind.
func (g grantKind) grantType() types.GrantType {
	switch g {
	case kindSessionKey:
		return types.GrantType_GRANT_TYPE_SESSION_KEY
	case kindRecurringPull:
		return types.GrantType_GRANT_TYPE_RECURRING_PULL
	case kindAllowance:
		return types.GrantType_GRANT_TYPE_SPENDING_ALLOWANCE
	case kindOneshotTransfer, kindOneshotExec:
		return types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT
	default:
		return types.GrantType_GRANT_TYPE_SIGN_IN
	}
}

// simSignInServices are the hostnames simulated SIGN_IN grants name.
var simSignInServices = []string{"phoenix.example.org", "aurora.example.net", "zenith.example.com"}

// simDisplayNames are the display names simulated inner messages set.
var simDisplayNames = []string{"Phoenix", "Aurora", "Zenith", "Nebula", "Cascade"}

// innerBuilders build a delegable message for signer (MsgExecSession and a
// oneshot Exec rewrite the signer field to the granter at dispatch, so it
// is set to the granter here too). Only types on params.allowed_msg_types
// that the sim can fill without other module state are listed.
var innerBuilders = map[string]func(e *simEnv, signer string) sdk.Msg{
	sdk.MsgTypeURL(&seasontypes.MsgSetDisplayName{}): func(e *simEnv, signer string) sdk.Msg {
		name, _ := pick(e.r, simDisplayNames)
		return &seasontypes.MsgSetDisplayName{Creator: signer, Name: fmt.Sprintf("%s %d", name, e.r.Intn(1000))}
	},
	sdk.MsgTypeURL(&blogtypes.MsgCreatePost{}): func(e *simEnv, signer string) sdk.Msg {
		name, _ := pick(e.r, simDisplayNames)
		return &blogtypes.MsgCreatePost{
			Creator: signer,
			Title:   "Notes from " + name,
			Body:    simtypes.RandStringOfLength(e.r, 20+e.r.Intn(80)),
		}
	},
}

// globallyAllowed reports whether typeURL is on params.allowed_msg_types and
// may be delegated (never NonDelegableSessionMsgs or ForbiddenAllowlistMsgs).
func (e *simEnv) globallyAllowed(typeURL string) bool {
	if types.NonDelegableSessionMsgs[typeURL] || types.ForbiddenAllowlistMsgs[typeURL] {
		return false
	}
	for _, t := range e.params.AllowedMsgTypes {
		if t == typeURL {
			return true
		}
	}
	return false
}

// buildableInnerTypes returns the inner message types the sim can build that
// are allowed right now, in a stable order.
func (e *simEnv) buildableInnerTypes() []string {
	var out []string
	for _, t := range []string{sdk.MsgTypeURL(&seasontypes.MsgSetDisplayName{}), sdk.MsgTypeURL(&blogtypes.MsgCreatePost{})} {
		if e.globallyAllowed(t) {
			out = append(out, t)
		}
	}
	return out
}

// sessionMsgTypes picks a session key's allowed_msg_types: every buildable
// inner type plus random other allowlisted types, capped at
// max_msg_types_per_session.
func (e *simEnv) sessionMsgTypes(selfRevoke bool) []string {
	limit := commontypes.Saturate[int](e.params.MaxMsgTypesPerSession)
	if limit <= 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(t string) {
		if len(out) < limit && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	if selfRevoke {
		add(types.MsgRevokeGrantTypeURL)
	}
	for _, t := range e.buildableInnerTypes() {
		add(t)
	}
	extra := e.r.Intn(4)
	for _, i := range e.r.Perm(len(e.params.AllowedMsgTypes)) {
		if extra == 0 {
			break
		}
		t := e.params.AllowedMsgTypes[i]
		if e.globallyAllowed(t) && !seen[t] {
			add(t)
			extra--
		}
	}
	return out
}

// randSeconds returns a duration in [lo, hi] seconds (lo when hi <= lo).
func (e *simEnv) randSeconds(lo, hi int64) int64 {
	if hi <= lo {
		return lo
	}
	return lo + e.r.Int63n(hi-lo+1)
}

// lifetimeCap bounds an expiry offset (seconds from now) by the universal
// max_grant_lifetime_seconds.
func (e *simEnv) lifetimeCap(offset int64) int64 {
	if e.params.MaxGrantLifetimeSeconds > 0 && offset > e.params.MaxGrantLifetimeSeconds {
		return e.params.MaxGrantLifetimeSeconds
	}
	return offset
}

// sessionExpiry returns a session key expiry within max_expiration (and the
// grant lifetime cap): at least an hour so the key outlives a few blocks.
func (e *simEnv) sessionExpiry() time.Time {
	maxSecs := int64(e.params.MaxExpiration / time.Second)
	offset := e.lifetimeCap(e.randSeconds(min(3600, maxSecs), maxSecs))
	return e.ctx.BlockTime().Add(time.Duration(offset) * time.Second)
}

// spendLimit returns a positive session fee budget within
// max_spend_limit_amount (at most 10 SPARK).
func (e *simEnv) spendLimit() sdk.Coin {
	hi := math.MinInt(e.params.MaxSpendLimitAmount, math.NewInt(10_000_000))
	if !hi.IsPositive() {
		return sdk.NewCoin(e.denom(), math.ZeroInt())
	}
	amt, err := simtypes.RandPositiveInt(e.r, hi)
	if err != nil {
		amt = hi
	}
	return sdk.NewCoin(e.denom(), amt)
}

// maxExecCount returns a session's exec cap within params.max_exec_count.
func (e *simEnv) maxExecCount() uint64 {
	hi := min(e.params.MaxExecCount, 50)
	if hi == 0 {
		return 0
	}
	return 1 + commontypes.Saturate[uint64](e.r.Int63n(commontypes.Saturate[int64](hi)))
}

// oneshotDeposit mirrors the handler's deposit formula
// (computeOneshotDeposit): Transfer pays max(creation_fee, min_deposit),
// Exec max(ceil(gas_limit * gas_price) + creation_fee, min_deposit).
func (e *simEnv) oneshotDeposit(gasLimit uint64, exec bool) (math.Int, error) {
	deposit := math.NewIntFromUint64(e.params.OneshotCreationFee)
	if exec {
		price, err := math.LegacyNewDecFromStr(e.params.OneshotGasPrice)
		if err != nil {
			return math.ZeroInt(), err
		}
		deposit = deposit.Add(price.MulInt(math.NewIntFromUint64(gasLimit)).Ceil().TruncateInt())
	}
	return math.MaxInt(deposit, math.NewIntFromUint64(e.params.MinOneshotDeposit)), nil
}

// buildGrant builds a MsgCreateGrant of kind from granter to grantee. spent
// is the SPARK the handler escrows from the granter (the oneshot deposit);
// a non-empty reason means no valid message exists.
func (e *simEnv) buildGrant(kind grantKind, granter, grantee simtypes.Account) (*types.MsgCreateGrant, math.Int, string, error) {
	now := e.ctx.BlockTime()
	msg := &types.MsgCreateGrant{
		Granter: granter.Address.String(),
		Grantee: grantee.Address.String(),
		Note:    "simulation " + kind.String(),
	}
	spent := math.ZeroInt()
	switch kind {
	case kindSessionKey:
		selfRevoke := e.r.Intn(2) == 0
		msg.ExpiresAt = e.sessionExpiry()
		msg.Payload = &types.MsgCreateGrant_SessionKey{SessionKey: &types.SessionKeyPayload{
			AllowedMsgTypes: e.sessionMsgTypes(selfRevoke),
			SpendLimit:      e.spendLimit(),
			MaxExecCount:    e.maxExecCount(),
			AllowSelfRevoke: selfRevoke,
		}}

	case kindRecurringPull:
		minPeriod := e.params.MinRecurringPeriodSeconds
		if minPeriod <= 0 {
			return nil, spent, "min_recurring_period_seconds is not positive", nil
		}
		period := e.randSeconds(minPeriod, 3*minPeriod)
		// Half the pulls start up to an hour back (the handler's clock-skew
		// allowance), so a claim is due at once when the period is short.
		start := int64(0)
		startAt := now.Unix()
		if e.r.Intn(2) == 0 {
			startAt = now.Unix() - e.r.Int63n(3500)
			start = startAt
		}
		offset := e.lifetimeCap(e.randSeconds(max(period*2, 3600), max(period*20, 3*86400)))
		expires := now.Unix() + offset
		if e.params.MaxRecurringDurationSeconds > 0 && expires-startAt > e.params.MaxRecurringDurationSeconds {
			expires = startAt + e.params.MaxRecurringDurationSeconds
		}
		if expires <= now.Unix() {
			return nil, spent, "recurring pull window does not reach past now", nil
		}
		msg.ExpiresAt = time.Unix(expires, 0).UTC()
		amount := math.NewInt(1_000 + e.r.Int63n(99_000))
		maxPerEpoch := ""
		if e.r.Intn(2) == 0 {
			maxPerEpoch = amount.MulRaw(1 + e.r.Int63n(20)).String()
		}
		msg.Payload = &types.MsgCreateGrant_RecurringPull{RecurringPull: &types.RecurringPullPayload{
			AmountPerPeriod: sdk.NewCoin(e.denom(), amount),
			PeriodSeconds:   period,
			StartTime:       start,
			MaxPerEpoch:     maxPerEpoch,
		}}

	case kindAllowance:
		minPeriod := max(e.params.MinAllowancePeriodSeconds, 1)
		msg.ExpiresAt = now.Add(time.Duration(e.lifetimeCap(e.randSeconds(3600, 5*86400))) * time.Second)
		var recipients []string
		if e.r.Intn(3) == 0 {
			for _, acc := range e.live(func(a simtypes.Account) bool { return !a.Address.Equals(granter.Address) }) {
				if len(recipients) >= min(3, commontypes.Saturate[int](e.params.MaxAllowanceRecipientList)) {
					break
				}
				if e.r.Intn(4) == 0 {
					recipients = append(recipients, acc.Address.String())
				}
			}
		}
		msg.Payload = &types.MsgCreateGrant_SpendingAllowance{SpendingAllowance: &types.SpendingAllowancePayload{
			MaxPerPeriod:      sdk.NewCoin(e.denom(), math.NewInt(100_000+e.r.Int63n(900_000))),
			PeriodSeconds:     e.randSeconds(minPeriod, 3*minPeriod),
			AllowedRecipients: recipients,
			Denom:             e.denom(),
		}}

	case kindOneshotTransfer, kindOneshotExec:
		fireAt := now.Unix() + e.params.MinScheduleDelaySeconds + e.r.Int63n(3*3600)
		if e.params.MaxScheduleHorizonSeconds > 0 && fireAt > now.Unix()+e.params.MaxScheduleHorizonSeconds {
			fireAt = now.Unix() + e.params.MaxScheduleHorizonSeconds
		}
		expires := fireAt + e.params.FireToExpiryBufferSeconds + e.r.Int63n(86400)
		if e.params.MaxGrantLifetimeSeconds > 0 && expires-now.Unix() > e.params.MaxGrantLifetimeSeconds {
			return nil, spent, "oneshot fire window exceeds the grant lifetime cap", nil
		}
		msg.ExpiresAt = time.Unix(expires, 0).UTC()
		so := &types.ScheduledOneshotPayload{FireAt: fireAt}
		var err error
		if kind == kindOneshotTransfer {
			amount := math.NewInt(1_000 + e.r.Int63n(99_000))
			// One transfer in four asks for more than the granter holds:
			// it pauses at fire time (PAUSED_INSUFFICIENT_FUNDS), the state
			// MsgRetryScheduledOneshot acts on.
			if e.r.Intn(4) == 0 {
				amount = e.spendable(granter).MulRaw(2).AddRaw(1)
			}
			so.Action = &types.ScheduledOneshotPayload_Transfer{Transfer: &types.OneshotTransfer{
				Recipient: grantee.Address.String(),
				Amount:    sdk.NewCoin(e.denom(), amount),
			}}
			spent, err = e.oneshotDeposit(0, false)
		} else {
			inner, ok := pick(e.r, e.buildableInnerTypes())
			if !ok {
				return nil, spent, "no buildable inner message type is allowlisted", nil
			}
			anyMsg, aerr := codectypes.NewAnyWithValue(innerBuilders[inner](e, msg.Granter))
			if aerr != nil {
				return nil, spent, "", aerr
			}
			gas := e.params.MinOneshotExecGas
			if e.params.MaxOneshotExecGas > gas {
				gas += commontypes.Saturate[uint64](e.r.Int63n(commontypes.Saturate[int64](e.params.MaxOneshotExecGas - gas + 1)))
			}
			so.Action = &types.ScheduledOneshotPayload_Exec{Exec: &types.OneshotExec{Msg: anyMsg, GasLimit: gas}}
			spent, err = e.oneshotDeposit(gas, true)
		}
		if err != nil {
			return nil, spent, "", err
		}
		msg.Payload = &types.MsgCreateGrant_ScheduledOneshot{ScheduledOneshot: so}

	default:
		service, _ := pick(e.r, simSignInServices)
		msg.ExpiresAt = now.Add(time.Duration(e.lifetimeCap(e.randSeconds(3600, 10*86400))) * time.Second)
		msg.Payload = &types.MsgCreateGrant_SignIn{SignIn: &types.SignInPayload{Service: service}}
	}
	return msg, spent, "", nil
}

// createGrant builds and delivers a grant of kind between two live sim
// accounts (dry-run first) and returns the stored grant. ok is false when
// no pair could create one; opMsg is the delivered create.
func (e *simEnv) createGrant(kind grantKind) (simtypes.OperationMsg, types.Grant, bool, error) {
	for attempt := 0; attempt < 3; attempt++ {
		granter, grantee, found := e.pair(math.NewInt(100_000))
		if !found {
			return simtypes.OperationMsg{}, types.Grant{}, false, nil
		}
		msg, spent, reason, err := e.buildGrant(kind, granter, grantee)
		if err != nil {
			return simtypes.OperationMsg{}, types.Grant{}, false, err
		}
		if reason != "" {
			continue
		}
		opMsg, ok, err := e.ensure(granter, msg, spent)
		if err != nil {
			return opMsg, types.Grant{}, false, err
		}
		if !ok {
			continue
		}
		g, found, err := e.newestGrant(msg.Granter, msg.Grantee, kind.grantType())
		if err != nil || !found {
			return opMsg, types.Grant{}, false, err
		}
		return opMsg, g, true, nil
	}
	return simtypes.OperationMsg{}, types.Grant{}, false, nil
}

// SimulateMsgCreateGrant creates a grant of a random payload variant
// (SessionKey, RecurringPull, SpendingAllowance, ScheduledOneshot Transfer
// or Exec, SignIn) between two live sim accounts. The oneshot gas deposit
// the handler escrows is the tx's CoinsSpentInMsg.
func SimulateMsgCreateGrant(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	label := &types.MsgCreateGrant{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		kind := grantKind(e.r.Intn(int(numGrantKinds)))
		reason := "no pair of live sim accounts"
		for attempt := 0; attempt < 3; attempt++ {
			granter, grantee, ok := e.pair(math.NewInt(100_000))
			if !ok {
				break
			}
			msg, spent, why, err := e.buildGrant(kind, granter, grantee)
			if err != nil {
				return noOpErr(label, err)
			}
			if why != "" {
				reason = why
				continue
			}
			if why := e.usable(granter, spent); why != "" {
				reason = why
				continue
			}
			if err := e.dryRun(msg); err != nil {
				reason = kind.String() + ": handler would reject: " + err.Error()
				continue
			}
			return e.deliver(granter, msg, spent)
		}
		return noOp(label, reason)
	})
}
