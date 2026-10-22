package simulation

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

// Commit-reveal classes in the simulator derive every secret from the class
// id, so SimulateMsgReveal and SimulateMsgRevealSeed can recompute the
// commitments without shared state. simBlindLeaves bounds a blind class's
// supply so its Merkle tree stays small.
const simBlindLeaves = 16

func simSeed(classID uint64) []byte {
	var be [8]byte
	binary.BigEndian.PutUint64(be[:], classID)
	sum := sha256.Sum256(append([]byte("sim-seed/"), be[:]...))
	return sum[:]
}

func simSalt(classID, tokenID uint64) []byte {
	var be [16]byte
	binary.BigEndian.PutUint64(be[:8], classID)
	binary.BigEndian.PutUint64(be[8:], tokenID)
	sum := sha256.Sum256(append([]byte("sim-salt/"), be[:]...))
	return sum[:]
}

func simBlindMetadata(classID, tokenID uint64) types.TokenMetadata {
	return types.TokenMetadata{Name: fmt.Sprintf("blind-%d-%d", classID, tokenID), Attributes: []types.Attribute{{Key: "rank", Value: fmt.Sprint(tokenID % 5)}}}
}

// simBlindTree builds the commitment tree of a simulated blind class.
func simBlindTree(classID uint64) ([]byte, [][][]byte) {
	leaves := make([][]byte, simBlindLeaves)
	for i := range leaves {
		tokenID := uint64(i + 1)
		leaves[i] = types.RevealLeaf(classID, tokenID, simSalt(classID, tokenID), simBlindMetadata(classID, tokenID))
	}
	return types.BuildRevealTree(leaves)
}

// SimulateMsgCreateClass creates a class from a random sim account that is
// an active x/rep member (class creation is member-gated). A share of the
// classes exercise the wave-1/2 policies: expiring, redeemable, mutual,
// price curves, blind mints and generative seeds; and the wave-3/4 shapes:
// editions, Harberger and permanent (NEITHER) classes.
func SimulateMsgCreateClass(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgCreateClass{}
		rk := k.GetRepKeeper()
		if rk == nil {
			return noop(msg, "rep keeper not wired")
		}
		p := k.GetParams(ctx)
		var acc simtypes.Account
		found := false
		for _, i := range r.Perm(len(accs)) {
			if !rk.IsActiveMember(ctx, accs[i].Address) {
				continue
			}
			if lvl, err := rk.GetTrustLevel(ctx, accs[i].Address); err == nil && uint32(lvl) >= p.MinTrustLevelCreateClass {
				acc, found = accs[i], true
				break
			}
		}
		if !found {
			return noop(msg, "no sim account is a member at the required trust level")
		}
		if count, _ := k.ClassCountByCreator.Get(ctx, acc.Address.String()); count >= uint64(p.MaxClassesPerCreator) {
			return noop(msg, "creator class cap reached")
		}
		seq, _ := k.ClassSeq.Peek(ctx)
		nextID := seq + 1
		now := ctx.BlockTime().Unix()

		soulbound := r.Intn(4) == 0
		burnAuth := types.BurnAuthorization_BURN_AUTHORIZATION_HOLDER
		if soulbound {
			burnAuth = types.BurnAuthorization(r.Intn(3)) // any mode is valid on a soulbound class
		}
		mutual := soulbound && r.Intn(3) == 0
		if mutual {
			burnAuth = types.BurnAuthorization_BURN_AUTHORIZATION_HOLDER_OR_ISSUER
		}
		redeemable := r.Intn(3) == 0
		msg = &types.MsgCreateClass{
			Creator:     acc.Address.String(),
			Name:        "Phoenix " + simtypes.RandStringOfLength(r, 6),
			Symbol:      "PHX",
			Description: "simulated class",
			Uri:         "ipfs://bafy" + simtypes.RandStringOfLength(r, 16),
			Flags: types.ClassFlags{
				Transferable: !soulbound, BurnAuthorization: burnAuth, TokenMetadataMutable: r.Intn(2) == 0,
				Mutual: mutual, Redeemable: redeemable, RedeemedTransferable: redeemable && !soulbound && r.Intn(2) == 0,
			},
			MaxSupply:              uint64(r.Intn(3)) * 50,
			AcceptedContentLicense: types.ContentLicense,
		}
		if r.Intn(3) == 0 {
			mode := types.ExpiryMode_EXPIRY_MODE_MARK
			if r.Intn(2) == 0 {
				mode = types.ExpiryMode_EXPIRY_MODE_BURN
			}
			term := types.MinTokenTerm + int64(r.Intn(600))
			msg.Expiry = types.ExpiryPolicy{Mode: mode, Term: term, RenewalPrice: sdk.NewInt64Coin(sdk.DefaultBondDenom, int64(r.Intn(2))*500_000)}
		}
		if !soulbound {
			msg.RoyaltyBps = uint32(r.Intn(int(p.MaxRoyaltyBps) + 1))
			price := int64(r.Intn(3)) * 1_000_000
			mp := types.MintPolicy{PublicMintEnabled: true, Price: sdk.NewInt64Coin(sdk.DefaultBondDenom, price)}
			switch r.Intn(4) {
			case 1:
				mp.Pricing = types.MintPricing_MINT_PRICING_LINEAR_SUPPLY
				mp.PriceStep = math.NewInt(int64(1+r.Intn(5)) * 10_000)
			case 2:
				mp.Pricing = types.MintPricing_MINT_PRICING_DUTCH
				mp.Price = sdk.NewInt64Coin(sdk.DefaultBondDenom, 2_000_000)
				mp.FloorPrice = math.NewInt(500_000)
				mp.StartTime = now
				mp.EndTime = now + 3600
			}
			msg.MintPolicy = mp
			if !mutual {
				switch r.Intn(4) {
				case 1:
					root, _ := simBlindTree(nextID)
					msg.MaxSupply = simBlindLeaves
					msg.Reveal = types.RevealPolicy{Kind: types.RevealKind_REVEAL_KIND_BLIND_METADATA, Commitment: hex.EncodeToString(root), Deadline: now + types.MinRevealWindow + int64(r.Intn(600))}
				case 2:
					msg.Reveal = types.RevealPolicy{Kind: types.RevealKind_REVEAL_KIND_GENERATIVE_SEED, Commitment: types.SeedCommitment(simSeed(nextID)), Deadline: now + types.MinRevealWindow + int64(r.Intn(600))}
				}
			}
		}
		// Waves 3 and 4: some plain classes become editions, Harberger or
		// permanent (NEITHER) classes instead.
		plain := !msg.Flags.Mutual && !msg.Flags.Redeemable && !msg.Expiry.Expires() && !msg.Reveal.Reveals()
		if plain {
			switch r.Intn(4) {
			case 0:
				msg.TokenModel = types.TokenModel_TOKEN_MODEL_EDITIONS
				msg.MintPolicy = types.MintPolicy{}
			case 1:
				if msg.Flags.Transferable {
					msg.Harberger = types.HarbergerPolicy{TaxBpsPerYear: uint32(100 + r.Intn(2000)),
						TaxPeriod: types.MinHarbergerPeriod + int64(r.Intn(600)), ForeclosureDuration: types.MinHarbergerPeriod + int64(r.Intn(600))}
				}
			case 2:
				msg.Flags.BurnAuthorization = types.BurnAuthorization_BURN_AUTHORIZATION_NEITHER
			}
		}
		return deliver(r, app, ctx, ak, bk, txGen, acc, msg, p.ClassCreationFee)
	}
}
