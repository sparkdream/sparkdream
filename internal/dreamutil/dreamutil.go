package dreamutil

import (
	"context"
	"fmt"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DREAMKeeper is the shared interface for DREAM token operations.
// Both x/name and x/season RepKeeper interfaces include these methods.
type DREAMKeeper interface {
	LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	// BurnLockedDREAM releases and burns one escrowed amount in a single
	// call, so a failure leaves the lock in place.
	BurnLockedDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
}

// Ops provides DREAM operations with string address convenience methods.
// Modules embed this in their keeper to avoid duplicating address conversion boilerplate.
type Ops struct {
	keeper       DREAMKeeper
	addressCodec address.Codec
}

// NewOps creates a new Ops instance. If keeper is nil, all operations are no-ops
// (development mode where x/rep is not wired up).
func NewOps(keeper DREAMKeeper, addressCodec address.Codec) Ops {
	return Ops{keeper: keeper, addressCodec: addressCodec}
}

// Lock escrows DREAM tokens for the given address.
func (o Ops) Lock(ctx context.Context, addr string, amount uint64) error {
	if o.keeper == nil {
		panic(fmt.Errorf("dream keeper not wired"))
	}
	addrBytes, err := o.addressCodec.StringToBytes(addr)
	if err != nil {
		return err
	}
	return o.keeper.LockDREAM(ctx, addrBytes, math.NewIntFromUint64(amount))
}

// Unlock releases escrowed DREAM tokens for the given address.
func (o Ops) Unlock(ctx context.Context, addr string, amount uint64) error {
	if o.keeper == nil {
		panic(fmt.Errorf("dream keeper not wired"))
	}
	addrBytes, err := o.addressCodec.StringToBytes(addr)
	if err != nil {
		return err
	}
	return o.keeper.UnlockDREAM(ctx, addrBytes, math.NewIntFromUint64(amount))
}

// Burn destroys DREAM tokens from the given address's UNLOCKED balance (a
// fee). x/rep refuses to burn locked DREAM through this path; slashing an
// escrowed stake goes through BurnLocked.
func (o Ops) Burn(ctx context.Context, addr string, amount uint64) error {
	if o.keeper == nil {
		panic(fmt.Errorf("dream keeper not wired"))
	}
	addrBytes, err := o.addressCodec.StringToBytes(addr)
	if err != nil {
		return err
	}
	return o.keeper.BurnDREAM(ctx, addrBytes, math.NewIntFromUint64(amount))
}

// BurnLocked slashes an escrowed stake: it releases the stake's lock and
// burns the same amount through x/rep's BurnLockedDREAM, in one call, so a
// failed burn never leaves the stake unlocked. Burning a locked stake with
// Burn alone left it locked forever (and paid the burn out of the holder's
// free balance, or failed when there was none).
func (o Ops) BurnLocked(ctx context.Context, addr string, amount uint64) error {
	if o.keeper == nil {
		panic(fmt.Errorf("dream keeper not wired"))
	}
	addrBytes, err := o.addressCodec.StringToBytes(addr)
	if err != nil {
		return err
	}
	return o.keeper.BurnLockedDREAM(ctx, addrBytes, math.NewIntFromUint64(amount))
}

// SettleStakes handles the common dispute resolution pattern:
// the winner's stake is unlocked (returned) and the loser's stake is burned.
func (o Ops) SettleStakes(ctx context.Context, winnerAddr string, winnerAmount uint64, loserAddr string, loserAmount uint64) error {
	if o.keeper == nil {
		panic(fmt.Errorf("dream keeper not wired"))
	}
	if err := o.Unlock(ctx, winnerAddr, winnerAmount); err != nil {
		return err
	}
	return o.BurnLocked(ctx, loserAddr, loserAmount)
}
