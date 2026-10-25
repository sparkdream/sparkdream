package keeper

import (
	"context"
	"errors"

	"sparkdream/x/name/types"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ReverseResolve(ctx context.Context, req *types.QueryReverseResolveRequest) (*types.QueryReverseResolveResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address cannot be empty")
	}

	// Look up the OwnerInfo for the address
	ownerInfo, err := q.k.Owners.Get(ctx, req.Address)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "account has no name information")
		}
		return nil, status.Error(codes.Internal, "internal error")
	}

	// Check if a primary name is actually set
	if ownerInfo.PrimaryName == "" {
		return nil, status.Error(codes.NotFound, "account has no primary name set")
	}

	// Verify the forward mapping before answering: the primary must still be
	// owned by the address, or name it as its accepted target. A primary
	// left behind by a path that changed the record without clearing it must
	// not let the address display a name it no longer resolves from.
	record, found := q.k.GetName(ctx, ownerInfo.PrimaryName)
	if !found {
		return nil, status.Error(codes.NotFound, "account has no primary name set")
	}
	// A migrated member's names stay recorded under the old address.
	owns := q.k.sameMember(ctx, record.Owner, req.Address)
	accepted := q.k.sameMember(ctx, record.Target, req.Address) && record.TargetAccepted
	if !owns && !accepted {
		return nil, status.Error(codes.NotFound, "account has no primary name set")
	}

	return &types.QueryReverseResolveResponse{Name: ownerInfo.PrimaryName}, nil
}
