package keeper

import sdk "github.com/cosmos/cosmos-sdk/types"

// RewriteSignerField exposes rewriteSignerField to keeper_test.
func RewriteSignerField(msg sdk.Msg, granter string) error {
	return rewriteSignerField(msg, granter)
}
