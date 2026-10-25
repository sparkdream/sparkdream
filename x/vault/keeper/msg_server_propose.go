package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/vault/types"
)

func (k msgServer) Propose(ctx context.Context, msg *types.MsgPropose) (*types.MsgProposeResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Signer); err != nil {
		return nil, errorsmod.Wrap(err, "invalid signer address")
	}
	p, err := k.Keeper.Propose(ctx, msg.Signer, msg.VaultId, msg.Messages, msg.Memo)
	if err != nil {
		return nil, err
	}
	executed := false
	if msg.Execute {
		v, err := k.Vault.Get(ctx, p.VaultId)
		if err != nil {
			return nil, err
		}
		if k.Tally(ctx, v, p) >= v.Threshold {
			if err := k.Keeper.Execute(ctx, msg.Signer, p.ProposalId); err != nil {
				return nil, err
			}
			executed = true
		}
	}
	return &types.MsgProposeResponse{ProposalId: p.ProposalId, Executed: executed}, nil
}
