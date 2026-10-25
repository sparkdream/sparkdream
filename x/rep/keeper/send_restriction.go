package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SendRestriction is the x/bank SendRestrictionFn that makes SPARK follow a
// migrated identity (plan §2.1). Every transfer whose recipient is a
// tombstone is redirected to the canonical address: escrow refunds, bounty
// refunds, role pay and plain MsgSends to the old address all land on the
// new one, so a liability recorded against the old address can still be
// settled and a thief who kept the old key cannot collect them.
//
// Appended to the bank keeper in app.go. It costs one store read per
// transfer and never fails a send. UndelegateCoins bypasses bank send
// restrictions, which is why MsgSweepTombstone exists.
func (k Keeper) SendRestriction(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) (sdk.AccAddress, error) {
	toStr, err := k.addressCodec.BytesToString(toAddr)
	if err != nil {
		return toAddr, nil
	}
	t, err := k.Tombstones.Get(ctx, toStr)
	if err != nil {
		return toAddr, nil
	}
	canonical, err := k.addressCodec.StringToBytes(t.Canonical)
	if err != nil {
		return toAddr, nil
	}
	return canonical, nil
}
