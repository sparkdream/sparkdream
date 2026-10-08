package types

import (
	"crypto/sha256"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// publicSubmitterSeed derives the shared anonymous submitter key. The private
// key is public on purpose: every anonymous client signs its MsgShieldedExec
// with it, so all anonymous actions come from one address and the outer
// signer reveals nothing. The ZK proof, not the signature, authorizes an exec;
// clients send unordered txs so they never race on the account's sequence.
// The ante handler refuses any other message signed by this account.
const publicSubmitterSeed = "sparkdream/shield/public-submitter/v1"

var publicSubmitterKey = func() *secp256k1.PrivKey {
	h := sha256.Sum256([]byte(publicSubmitterSeed))
	return &secp256k1.PrivKey{Key: h[:]}
}()

// PublicSubmitterPrivKey returns the shared anonymous submitter's private key.
func PublicSubmitterPrivKey() []byte {
	return append([]byte(nil), publicSubmitterKey.Key...)
}

// PublicSubmitterAddress returns the shared anonymous submitter's address.
func PublicSubmitterAddress() sdk.AccAddress {
	return sdk.AccAddress(publicSubmitterKey.PubKey().Address())
}
