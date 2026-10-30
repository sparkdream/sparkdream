package simulation

import (
	"bytes"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgRegisterZkPublicKey has a member register (or rotate to) a ZK
// public key derived from its own account key. A first key without a
// cleared one is active at once; a change waits zk_key_activation_delay and
// is allowed once per season.
func SimulateMsgRegisterZkPublicKey(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgRegisterZkPublicKey{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		ms, err := e.members(func(ma memberAcc) bool { return len(ma.m.PendingZkPublicKey) == 0 })
		if err != nil {
			return noOpErr(label, err)
		}
		var cands []candidate
		for n, i := range e.r.Perm(len(ms)) {
			if n >= 8 {
				break
			}
			ma := ms[i]
			key, err := zkPublicKey(ma.acc, e.r.Intn(4))
			if err != nil {
				return noOpErr(label, err)
			}
			if bytes.Equal(key, ma.m.ZkPublicKey) {
				continue
			}
			cands = append(cands, candidate{acc: ma.acc, msg: &types.MsgRegisterZkPublicKey{
				Member: ma.addr(), ZkPublicKey: key,
			}, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 4, "no member may register a ZK key now")
	})
}
