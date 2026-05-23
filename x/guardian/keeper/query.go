package keeper

import (
	"context"

	"sparkdream/x/guardian/types"
)

type queryServer struct {
	k *Keeper
}

// NewQueryServerImpl returns a guardian query server. Currently exposes
// only the AllowedMsgs introspection.
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return queryServer{k: k}
}

var _ types.QueryServer = queryServer{}

// AllowedMsgs returns the static allowlist. Built into the binary; only
// changeable via chain upgrade.
func (q queryServer) AllowedMsgs(_ context.Context, _ *types.QueryAllowedMsgsRequest) (*types.QueryAllowedMsgsResponse, error) {
	return &types.QueryAllowedMsgsResponse{TypeUrls: AllowedMsgTypes()}, nil
}
