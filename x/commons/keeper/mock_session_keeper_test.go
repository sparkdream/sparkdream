package keeper_test

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	sessiontypes "sparkdream/x/session/types"
)

// mockSessionKeeper is a recording test double for the SessionKeeper
// interface that x/commons consumes. It captures every call from the
// wrapper handlers and stores grants in a simple map so wrapper unit
// tests can verify:
//
//   - the wrapper translates msg inputs into the correct session
//     payload (granter / grantee / amounts / max_per_epoch / expires),
//   - the wrapper passes the correct callerModuleAddr (the commons
//     module bech32 address),
//   - the wrapper handles session-returned errors correctly (notably
//     ErrGrantNotFound / ErrGrantTerminal → ErrRecurringSpendInactive).
//
// We deliberately do NOT re-implement session's claim mechanics
// (period clock, status transitions, hook PreCheck/PostCommit). Those
// are exercised by the session-side tests (M2) and the SessionClaimHook
// tests (M4); the wrapper layer's job is translation, not business
// logic.
//
// For tests that want to exercise specific session error paths, the
// mock exposes per-method override functions
// (e.g. `RevokeGrantInternalFn`) that take precedence over the
// default in-memory behavior.
type mockSessionKeeper struct {
	// In-memory grant store + ID allocator.
	grants  map[uint64]sessiontypes.Grant
	nextID  uint64
	deleted map[uint64]bool

	// Recorded calls — tests assert against these.
	CreateCalls  []mockCreateCall
	RevokeCalls  []mockRevokeCall
	DeclineCalls []mockDeclineCall
	ClaimCalls   []mockClaimCall

	// Per-method overrides for error-injection tests. When non-nil,
	// the override is invoked instead of the default behavior.
	CreateGrantOnBehalfOfFn       func(ctx context.Context, callerModuleAddr string, msg *sessiontypes.MsgCreateGrant) (uint64, error)
	RevokeGrantInternalFn         func(ctx context.Context, callerModuleAddr string, grantID uint64) (sdk.Coin, error)
	DeclineGrantInternalFn        func(ctx context.Context, callerModuleAddr string, grantID uint64, grantee string) (sdk.Coin, error)
	ClaimRecurringPullForGranteeF func(ctx context.Context, callerModuleAddr string, grantID uint64, grantee string) (*sessiontypes.MsgClaimRecurringPullResponse, error)
}

type mockCreateCall struct {
	CallerModule string
	Msg          *sessiontypes.MsgCreateGrant
}

type mockRevokeCall struct {
	CallerModule string
	GrantID      uint64
}

type mockDeclineCall struct {
	CallerModule string
	GrantID      uint64
	Grantee      string
}

type mockClaimCall struct {
	CallerModule string
	GrantID      uint64
	Grantee      string
}

func newMockSessionKeeper() *mockSessionKeeper {
	return &mockSessionKeeper{
		grants:  make(map[uint64]sessiontypes.Grant),
		deleted: make(map[uint64]bool),
	}
}

// --- SessionKeeper interface implementation ----------------------------------

func (m *mockSessionKeeper) CreateGrantOnBehalfOf(
	ctx context.Context, callerModuleAddr string, msg *sessiontypes.MsgCreateGrant,
) (uint64, error) {
	m.CreateCalls = append(m.CreateCalls, mockCreateCall{CallerModule: callerModuleAddr, Msg: msg})
	if m.CreateGrantOnBehalfOfFn != nil {
		return m.CreateGrantOnBehalfOfFn(ctx, callerModuleAddr, msg)
	}
	m.nextID++
	id := m.nextID
	grant := sessiontypes.Grant{
		Id:        id,
		Granter:   msg.Granter,
		Grantee:   msg.Grantee,
		ExpiresAt: msg.ExpiresAt,
		Note:      msg.Note,
		Status:    sessiontypes.GrantStatus_GRANT_STATUS_ACTIVE,
	}
	switch p := msg.Payload.(type) {
	case *sessiontypes.MsgCreateGrant_RecurringPull:
		grant.Type = sessiontypes.GrantType_GRANT_TYPE_RECURRING_PULL
		grant.Payload = &sessiontypes.Grant_RecurringPull{RecurringPull: p.RecurringPull}
	case *sessiontypes.MsgCreateGrant_SessionKey:
		grant.Type = sessiontypes.GrantType_GRANT_TYPE_SESSION_KEY
	case *sessiontypes.MsgCreateGrant_SpendingAllowance:
		grant.Type = sessiontypes.GrantType_GRANT_TYPE_SPENDING_ALLOWANCE
	case *sessiontypes.MsgCreateGrant_ScheduledOneshot:
		grant.Type = sessiontypes.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT
	}
	m.grants[id] = grant
	return id, nil
}

func (m *mockSessionKeeper) RevokeGrantInternal(
	ctx context.Context, callerModuleAddr string, grantID uint64,
) (sdk.Coin, error) {
	m.RevokeCalls = append(m.RevokeCalls, mockRevokeCall{CallerModule: callerModuleAddr, GrantID: grantID})
	if m.RevokeGrantInternalFn != nil {
		return m.RevokeGrantInternalFn(ctx, callerModuleAddr, grantID)
	}
	if _, ok := m.grants[grantID]; !ok {
		return sdk.Coin{}, errorsmod.Wrapf(sessiontypes.ErrGrantNotFound, "id=%d", grantID)
	}
	delete(m.grants, grantID)
	m.deleted[grantID] = true
	return sdk.NewCoin("uspark", math.ZeroInt()), nil
}

func (m *mockSessionKeeper) DeclineGrantInternal(
	ctx context.Context, callerModuleAddr string, grantID uint64, grantee string,
) (sdk.Coin, error) {
	m.DeclineCalls = append(m.DeclineCalls, mockDeclineCall{CallerModule: callerModuleAddr, GrantID: grantID, Grantee: grantee})
	if m.DeclineGrantInternalFn != nil {
		return m.DeclineGrantInternalFn(ctx, callerModuleAddr, grantID, grantee)
	}
	g, ok := m.grants[grantID]
	if !ok {
		return sdk.Coin{}, errorsmod.Wrapf(sessiontypes.ErrGrantNotFound, "id=%d", grantID)
	}
	if g.Grantee != grantee {
		return sdk.Coin{}, errorsmod.Wrapf(sessiontypes.ErrDeclineUnauthorized,
			"caller %s is not the grantee %s", grantee, g.Grantee)
	}
	delete(m.grants, grantID)
	m.deleted[grantID] = true
	return sdk.NewCoin("uspark", math.ZeroInt()), nil
}

func (m *mockSessionKeeper) ClaimRecurringPullForGrantee(
	ctx context.Context, callerModuleAddr string, grantID uint64, grantee string,
) (*sessiontypes.MsgClaimRecurringPullResponse, error) {
	m.ClaimCalls = append(m.ClaimCalls, mockClaimCall{CallerModule: callerModuleAddr, GrantID: grantID, Grantee: grantee})
	if m.ClaimRecurringPullForGranteeF != nil {
		return m.ClaimRecurringPullForGranteeF(ctx, callerModuleAddr, grantID, grantee)
	}
	// Default: stubbed canonical response so the wrapper test can
	// verify the response shape translation.
	return &sessiontypes.MsgClaimRecurringPullResponse{
		ClaimNumber:   1,
		NextClaimTime: 0,
	}, nil
}

func (m *mockSessionKeeper) GetGrant(_ context.Context, id uint64) (sessiontypes.Grant, error) {
	g, ok := m.grants[id]
	if !ok {
		return sessiontypes.Grant{}, errorsmod.Wrapf(sessiontypes.ErrGrantNotFound, "id=%d", id)
	}
	return g, nil
}

func (m *mockSessionKeeper) ListGrantsByGranter(_ context.Context, granter string, filterType sessiontypes.GrantType) ([]sessiontypes.Grant, error) {
	var out []sessiontypes.Grant
	for _, g := range m.grants {
		if g.Granter != granter {
			continue
		}
		if filterType != sessiontypes.GrantType_GRANT_TYPE_UNSPECIFIED && g.Type != filterType {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

func (m *mockSessionKeeper) ListGrantsByGrantee(_ context.Context, grantee string, filterType sessiontypes.GrantType) ([]sessiontypes.Grant, error) {
	var out []sessiontypes.Grant
	for _, g := range m.grants {
		if g.Grantee != grantee {
			continue
		}
		if filterType != sessiontypes.GrantType_GRANT_TYPE_UNSPECIFIED && g.Type != filterType {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

// SetClaimHooks is a no-op for the mock: the wrapper tests don't
// exercise the SessionClaimHook path (that's covered by M4's
// session_claim_hook_test.go).
func (m *mockSessionKeeper) SetClaimHooks(_ ...sessiontypes.GrantClaimHook) {}
