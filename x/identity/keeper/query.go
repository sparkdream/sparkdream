package keeper

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/identity/types"
)

var _ types.QueryServer = queryServer{}

// NewQueryServerImpl returns an implementation of the QueryServer interface
// for the provided Keeper. Takes *Keeper so the query server sees live
// keeper fields (notably any late-bound bank keeper).
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return queryServer{k}
}

type queryServer struct {
	k *Keeper
}

// ChainIdentity returns the chain's immutable identity record. Returns
// codes.NotFound before InitGenesis runs (CLI/REST clients querying a
// partially constructed genesis don't crash).
func (q queryServer) ChainIdentity(ctx context.Context, _ *types.QueryChainIdentityRequest) (*types.QueryChainIdentityResponse, error) {
	id, err := q.k.GetChainIdentity(ctx)
	if err != nil {
		if errors.Is(err, types.ErrIdentityNotInitialized) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryChainIdentityResponse{Identity: id}, nil
}

// BondDenom returns the chain's bond denom. Same NotFound semantics as
// ChainIdentity.
func (q queryServer) BondDenom(ctx context.Context, _ *types.QueryBondDenomRequest) (*types.QueryBondDenomResponse, error) {
	id, err := q.k.GetChainIdentity(ctx)
	if err != nil {
		if errors.Is(err, types.ErrIdentityNotInitialized) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryBondDenomResponse{Denom: id.BondDenom}, nil
}

// DreamDenom returns the chain's dream denom. Same NotFound semantics as
// ChainIdentity.
func (q queryServer) DreamDenom(ctx context.Context, _ *types.QueryDreamDenomRequest) (*types.QueryDreamDenomResponse, error) {
	id, err := q.k.GetChainIdentity(ctx)
	if err != nil {
		if errors.Is(err, types.ErrIdentityNotInitialized) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryDreamDenomResponse{Denom: id.DreamDenom}, nil
}
