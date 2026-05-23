package types

import (
	"context"

	"github.com/cosmos/cosmos-sdk/baseapp"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypesv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	identitytypes "sparkdream/x/identity/types"
)

// IdentityKeeper is the subset of x/identity guardian reads from to validate
// native-denom constraints. The bank.MsgSetSendEnabled filter rejects the
// sealed BondDenom / DreamDenom to keep gov from freezing the native economy
// under cover of an IBC-voucher emergency. x/identity is a leaf module and
// does not depend on x/guardian, so this import direction is acyclic.
type IdentityKeeper interface {
	IsIdentityKeeper() // marker — disambiguates from rep/session.Keeper for depinject
	GetSealedIdentity(ctx context.Context) (identitytypes.ChainIdentity, error)
}

// MintKeeper is the read-side of x/mint that the field filter for
// mint.MsgUpdateParams uses to compare proposed vs current params.
type MintKeeper interface {
	GetParams(ctx context.Context) (minttypes.Params, error)
}

// StakingKeeper is the read-side of x/staking that the field filter for
// staking.MsgUpdateParams uses to compare proposed vs current params.
type StakingKeeper interface {
	GetParams(ctx context.Context) (stakingtypes.Params, error)
}

// DistrKeeper is the read-side of x/distribution that the field filter for
// distribution.MsgUpdateParams uses. Wrapped at the app layer because the
// upstream keeper exposes Params via a collections.Item, not a method.
type DistrKeeper interface {
	GetParams(ctx context.Context) (distrtypes.Params, error)
}

// GovKeeper is the read-side of x/gov that the field filter for
// gov.MsgUpdateParams uses. Wrapped at the app layer because the upstream
// keeper exposes Params via a collections.Item, not a method.
type GovKeeper interface {
	GetParams(ctx context.Context) (govtypesv1.Params, error)
}

// SlashingKeeper is the read-side of x/slashing that the field filter for
// slashing.MsgUpdateParams uses.
type SlashingKeeper interface {
	GetParams(ctx context.Context) (slashingtypes.Params, error)
}

// AuthKeeper is the read-side of x/auth that the field filter for
// auth.MsgUpdateParams uses. The upstream signature returns no error;
// the app-layer adapter wraps it to match this interface.
type AuthKeeper interface {
	GetParams(ctx context.Context) (authtypes.Params, error)
}

// MessageRouter is the msg-service-router interface guardian uses to route
// inner msgs to their target module's handler. baseapp.MessageRouter is
// what runtime supplies via depinject.
type MessageRouter = baseapp.MessageRouter
