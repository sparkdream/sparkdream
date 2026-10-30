package abci

import (
	"bytes"
	"fmt"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	protoio "github.com/cosmos/gogoproto/io"

	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

// Injected DKG extensions are authenticated by the vote-extension signature
// CometBFT made with each validator's consensus key. The proposer copies the
// signature next to the extension; ProcessProposal and the PreBlocker verify
// it against the named validator's consensus public key. Without it the
// proposer could attribute registrations, contributions or complaints of its
// own making to any validator (for instance complain against every honest
// dealer, whose justifications then publish enough polynomial evaluations to
// reconstruct the TLE master secret). The proof of possession inside an
// extension does not help: it is made under a key the prover chooses.
//
// Decryption shares need no signature: each is pairing-verified against the
// validator's public share, which only the holder of the secret share can
// satisfy.

// extensionSignBytes is what CometBFT signs for a vote extension: the
// length-delimited CanonicalVoteExtension (see baseapp.ValidateVoteExtensions).
func extensionSignBytes(extension []byte, height int64, round int32, chainID string) ([]byte, error) {
	cve := cmtproto.CanonicalVoteExtension{
		Extension: extension,
		Height:    height,
		Round:     int64(round),
		ChainId:   chainID,
	}
	var buf bytes.Buffer
	if err := protoio.NewDelimitedWriter(&buf).WriteMsg(&cve); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// verifyExtensionSignature checks that the validator with consensus address
// consAddr signed ext (as marshaled) in the last commit: at height (the
// previous block) and round (the last commit's round) on this chain.
func verifyExtensionSignature(ctx sdk.Context, k keeper.Keeper, consAddr []byte, ext *types.DKGVoteExtension, sig []byte, height int64, round int32) error {
	if len(sig) == 0 {
		return fmt.Errorf("missing vote extension signature")
	}
	sk := k.GetStakingKeeper()
	if sk == nil {
		return fmt.Errorf("staking keeper not wired")
	}
	val, err := sk.GetValidatorByConsAddr(ctx, sdk.ConsAddress(consAddr))
	if err != nil {
		return fmt.Errorf("validator not found: %w", err)
	}
	pk, err := val.ConsPubKey()
	if err != nil || pk == nil {
		return fmt.Errorf("validator consensus key unavailable: %v", err)
	}
	if !bytes.Equal(pk.Address(), consAddr) {
		return fmt.Errorf("consensus key does not match the validator address")
	}
	bz, err := ext.Marshal()
	if err != nil {
		return fmt.Errorf("marshal extension: %w", err)
	}
	signBytes, err := extensionSignBytes(bz, height, round, ctx.ChainID())
	if err != nil {
		return err
	}
	if !pk.VerifySignature(signBytes, sig) {
		return fmt.Errorf("invalid vote extension signature")
	}
	return nil
}
