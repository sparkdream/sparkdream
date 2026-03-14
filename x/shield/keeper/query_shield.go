package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/shield/types"
)

func (q queryServer) ShieldedOp(ctx context.Context, req *types.QueryShieldedOpRequest) (*types.QueryShieldedOpResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	reg, found := q.k.GetShieldedOp(ctx, req.MessageTypeUrl)
	if !found {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("shielded op not found: %s", req.MessageTypeUrl))
	}
	return &types.QueryShieldedOpResponse{Registration: reg}, nil
}

func (q queryServer) ShieldedOps(ctx context.Context, req *types.QueryShieldedOpsRequest) (*types.QueryShieldedOpsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	storeAdapter := runtime.KVStoreAdapter(q.k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, types.ShieldedOpsKey.Bytes())

	var regs []types.ShieldedOpRegistration
	pageRes, err := query.Paginate(store, req.Pagination, func(key []byte, value []byte) error {
		var reg types.ShieldedOpRegistration
		if err := q.k.cdc.Unmarshal(value, &reg); err != nil {
			return err
		}
		regs = append(regs, reg)
		return nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryShieldedOpsResponse{Registrations: regs, Pagination: pageRes}, nil
}

func (q queryServer) ModuleBalance(ctx context.Context, req *types.QueryModuleBalanceRequest) (*types.QueryModuleBalanceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	moduleAddr := q.k.accountKeeper.GetModuleAddress(types.ModuleName)
	balance := q.k.bankKeeper.GetBalance(ctx, moduleAddr, "uspark")
	return &types.QueryModuleBalanceResponse{
		Balance: balance,
	}, nil
}

func (q queryServer) NullifierUsed(ctx context.Context, req *types.QueryNullifierUsedRequest) (*types.QueryNullifierUsedResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	used := q.k.IsNullifierUsed(ctx, req.Domain, req.Scope, req.NullifierHex)
	var usedAtHeight int64
	if used {
		n, _ := q.k.GetUsedNullifier(ctx, req.Domain, req.Scope, req.NullifierHex)
		usedAtHeight = n.UsedAtHeight
	}
	return &types.QueryNullifierUsedResponse{
		Used:         used,
		UsedAtHeight: usedAtHeight,
	}, nil
}

func (q queryServer) DayFunding(ctx context.Context, req *types.QueryDayFundingRequest) (*types.QueryDayFundingResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	amount := q.k.GetDayFunding(ctx, req.Day)
	return &types.QueryDayFundingResponse{
		DayFunding: types.DayFunding{
			Day:          req.Day,
			AmountFunded: amount,
		},
	}, nil
}

func (q queryServer) ShieldEpoch(ctx context.Context, req *types.QueryShieldEpochRequest) (*types.QueryShieldEpochResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	state, found := q.k.GetShieldEpochStateVal(ctx)
	if !found {
		state = types.ShieldEpochState{}
	}
	return &types.QueryShieldEpochResponse{EpochState: state}, nil
}

func (q queryServer) PendingOps(ctx context.Context, req *types.QueryPendingOpsRequest) (*types.QueryPendingOpsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	storeAdapter := runtime.KVStoreAdapter(q.k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, types.PendingOpsKey.Bytes())

	var ops []types.PendingShieldedOp
	pageRes, err := query.Paginate(store, req.Pagination, func(key []byte, value []byte) error {
		var op types.PendingShieldedOp
		if err := q.k.cdc.Unmarshal(value, &op); err != nil {
			return err
		}
		// Apply epoch filter (0 = no filter)
		if req.Epoch != 0 && op.TargetEpoch != req.Epoch {
			return nil
		}
		ops = append(ops, op)
		return nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryPendingOpsResponse{PendingOps: ops, Pagination: pageRes}, nil
}

func (q queryServer) PendingOpCount(ctx context.Context, req *types.QueryPendingOpCountRequest) (*types.QueryPendingOpCountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	count := q.k.GetPendingOpCountVal(ctx)
	return &types.QueryPendingOpCountResponse{Count: count}, nil
}

func (q queryServer) TLEMasterPublicKey(ctx context.Context, req *types.QueryTLEMasterPublicKeyRequest) (*types.QueryTLEMasterPublicKeyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ks, found := q.k.GetTLEKeySetVal(ctx)
	if !found {
		return &types.QueryTLEMasterPublicKeyResponse{}, nil
	}
	return &types.QueryTLEMasterPublicKeyResponse{
		MasterPublicKey: ks.MasterPublicKey,
	}, nil
}

func (q queryServer) TLEKeySet(ctx context.Context, req *types.QueryTLEKeySetRequest) (*types.QueryTLEKeySetResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ks, found := q.k.GetTLEKeySetVal(ctx)
	if !found {
		return &types.QueryTLEKeySetResponse{}, nil
	}
	return &types.QueryTLEKeySetResponse{KeySet: ks}, nil
}

func (q queryServer) VerificationKey(ctx context.Context, req *types.QueryVerificationKeyRequest) (*types.QueryVerificationKeyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	vk, found := q.k.GetVerificationKeyVal(ctx, req.CircuitId)
	if !found {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("verification key not found: %s", req.CircuitId))
	}
	return &types.QueryVerificationKeyResponse{VerificationKey: vk}, nil
}

func (q queryServer) TLEMissCount(ctx context.Context, req *types.QueryTLEMissCountRequest) (*types.QueryTLEMissCountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	count := q.k.GetTLEMissCount(ctx, req.ValidatorAddress)
	return &types.QueryTLEMissCountResponse{MissCount: count}, nil
}

func (q queryServer) DecryptionShares(ctx context.Context, req *types.QueryDecryptionSharesRequest) (*types.QueryDecryptionSharesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	shares := q.k.GetDecryptionSharesForEpoch(ctx, req.Epoch)
	return &types.QueryDecryptionSharesResponse{Shares: shares}, nil
}

func (q queryServer) IdentityRateLimit(ctx context.Context, req *types.QueryIdentityRateLimitRequest) (*types.QueryIdentityRateLimitResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	epoch := q.k.GetCurrentEpoch(ctx)
	key := collections.Join(epoch, req.RateLimitNullifierHex)
	count, err := q.k.IdentityRateLimits.Get(ctx, key)
	if err != nil {
		count = 0
	}

	remaining := params.MaxExecsPerIdentityPerEpoch - count
	if count > params.MaxExecsPerIdentityPerEpoch {
		remaining = 0
	}

	return &types.QueryIdentityRateLimitResponse{
		UsedCount: count,
		MaxCount:  params.MaxExecsPerIdentityPerEpoch,
		Remaining: remaining,
	}, nil
}

func (q queryServer) DKGState(ctx context.Context, req *types.QueryDKGStateRequest) (*types.QueryDKGStateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	dkgState, found := q.k.GetDKGStateVal(ctx)
	if !found {
		dkgState = types.DKGState{}
	}
	return &types.QueryDKGStateResponse{DkgState: dkgState}, nil
}

func (q queryServer) DKGContributions(ctx context.Context, req *types.QueryDKGContributionsRequest) (*types.QueryDKGContributionsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	contributions := q.k.GetAllDKGContributions(ctx)
	return &types.QueryDKGContributionsResponse{Contributions: contributions}, nil
}
