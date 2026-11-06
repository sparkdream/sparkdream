package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// OpenSystemReportAsServiceForTest files a report the way x/service's own
// liveness sweep does (caller module = x/service), for tests in the _test
// package.
func (k Keeper) OpenSystemReportAsServiceForTest(ctx context.Context, moduleAddr, operator sdk.AccAddress, serviceType string, dedupe []byte) (uint64, bool, error) {
	return k.openSystemReport(ctx, types.ModuleName, moduleAddr, operator, serviceType, 0, "liveness", dedupe)
}

// RunIsolatedForTest exposes the EndBlocker item isolation (SVC-105 /
// SVC-202) so tests can drive it with a panicking item.
func (k Keeper) RunIsolatedForTest(ctx context.Context, sweep string, fn func(ctx context.Context) (bool, error)) bool {
	return k.runIsolated(ctx, sweep, fn)
}
