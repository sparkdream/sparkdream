package keeper

import (
	"context"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/sparkdream/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ContentLicense reports the chain's content license. It reads no state: the
// license is a compiled constant (see x/common/types/content_license.go).
func (q queryServer) ContentLicense(ctx context.Context, req *types.QueryContentLicenseRequest) (*types.QueryContentLicenseResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	return &types.QueryContentLicenseResponse{
		License:                   commontypes.ChainContentLicense,
		Name:                      commontypes.ChainContentLicenseName,
		Url:                       commontypes.ChainContentLicenseURL,
		Dedication:                commontypes.ContentDedication,
		AcceptedFederatedLicenses: commontypes.UnencumberedLicenses(),
	}, nil
}
