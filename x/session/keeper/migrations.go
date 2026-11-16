package keeper

import (
	"context"

	"sparkdream/x/session/types"
)

// MigrateV1ToV2 admits the x/rep work and staking messages
// (types.RepWorkMsgTypes) on a chain that started before they were delegable.
// The ceiling is set at genesis and no message can raise it, so the upgrade
// is the only way in. They are appended to the active list as well: a network
// that narrowed its active list never decided on these entries, and the
// Commons Operations Committee can remove them like any other.
func (k Keeper) MigrateV1ToV2(ctx context.Context) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	params.MaxAllowedMsgTypes = appendMissing(params.MaxAllowedMsgTypes, types.RepWorkMsgTypes)
	params.AllowedMsgTypes = appendMissing(params.AllowedMsgTypes, types.RepWorkMsgTypes)
	if err := params.Validate(); err != nil {
		return err
	}
	return k.Params.Set(ctx, params)
}

// appendMissing appends the entries of add that list does not hold yet,
// keeping the order of both.
func appendMissing(list, add []string) []string {
	have := make(map[string]bool, len(list))
	for _, m := range list {
		have[m] = true
	}
	for _, m := range add {
		if !have[m] {
			list = append(list, m)
			have[m] = true
		}
	}
	return list
}
