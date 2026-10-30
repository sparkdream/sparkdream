package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
	commontypes "sparkdream/x/common/types"
)

// SimulateMsgUpdatePost delivers MsgUpdatePost from the author (by alias) of
// a random active post, creating one first when no post has a live sim
// author. The signer pays the storage fee for the bytes above the post's
// fee high-water mark, plus the media scan fee when the new body is media.
// Replies stay enabled nine times in ten.
func SimulateMsgUpdatePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUpdatePost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		label := &types.MsgUpdatePost{}
		post, acc, ok, err := e.ownedPostOrCreate(activePost, anyone)
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no active post with a live author and none could be created")
		}
		ct, body, _ := e.randomContent(acc.Address.String(), e.params.MaxBodyLength, false)
		levels := []int32{-1, 0, 0, 1, 2}
		lvl, _ := pick(e.r, levels)
		msg := &types.MsgUpdatePost{
			Creator:            acc.Address.String(),
			Id:                 post.Id,
			Title:              truncate(randomTitle(e.r)+" (edited)", e.params.MaxTitleLength),
			Body:               body,
			ContentType:        ct,
			RepliesEnabled:     e.r.Intn(10) > 0,
			MinReplyTrustLevel: lvl,
			Tags:               e.randomTags(),
		}
		spent := e.scanFee(ct, body)
		newBytes := commontypes.Saturate[uint64](len(msg.Title) + len(msg.Body))
		if newBytes > post.FeeBytesHighWater {
			spent = spent.Add(e.storageFee(commontypes.Saturate[int](newBytes - post.FeeBytesHighWater)))
		}
		return e.submit(acc, msg, spent)
	})
}
