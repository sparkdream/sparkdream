package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/types/bech32"
)

// secp256k1PubKeyType is the amino type a wallet reports for its key.
const secp256k1PubKeyType = "tendermint/PubKeySecp256k1"

// WalletSignature is what Keplr's signArbitrary (and Leap's, through the
// same window.keplr API) returns: the signer's public key and a 64-byte
// r||s signature, both base64.
type WalletSignature struct {
	PubKey struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	} `json:"pub_key"`
	Signature string `json:"signature"`
}

// The ADR-036 sign doc, with fields declared in the sorted order amino JSON
// requires. encoding/json escapes <, > and & exactly as cosmjs's
// serializeSignDoc does, so Marshal gives the bytes the wallet signed.
type adr036Doc struct {
	AccountNumber string      `json:"account_number"`
	ChainID       string      `json:"chain_id"`
	Fee           adr036Fee   `json:"fee"`
	Memo          string      `json:"memo"`
	Msgs          []adr036Msg `json:"msgs"`
	Sequence      string      `json:"sequence"`
}

type adr036Fee struct {
	Amount []struct{} `json:"amount"`
	Gas    string     `json:"gas"`
}

type adr036Msg struct {
	Type  string         `json:"type"`
	Value adr036MsgValue `json:"value"`
}

type adr036MsgValue struct {
	Data   string `json:"data"`
	Signer string `json:"signer"`
}

// adr036SignBytes is the document a wallet signs for signArbitrary(signer,
// data): chain id, account number and sequence all empty or zero, so the
// signature is valid on no chain as a transaction.
func adr036SignBytes(signer, data string) ([]byte, error) {
	return json.Marshal(adr036Doc{
		AccountNumber: "0",
		Fee:           adr036Fee{Amount: []struct{}{}, Gas: "0"},
		Msgs: []adr036Msg{{
			Type:  "sign/MsgSignData",
			Value: adr036MsgValue{Data: base64.StdEncoding.EncodeToString([]byte(data)), Signer: signer},
		}},
		Sequence: "0",
	})
}

// verifyADR036 checks that sig is signer's signature over data, and that
// signer is the key's address under prefix. It returns the raw 20-byte
// address, which is the same on every chain the key is used on.
func verifyADR036(prefix, signer, data string, sig WalletSignature) ([]byte, error) {
	if sig.PubKey.Type != secp256k1PubKeyType {
		return nil, fmt.Errorf("unsupported key type %q", sig.PubKey.Type)
	}
	keyBytes, err := base64.StdEncoding.DecodeString(sig.PubKey.Value)
	if err != nil || len(keyBytes) != secp256k1.PubKeySize {
		return nil, errors.New("malformed public key")
	}
	pub := &secp256k1.PubKey{Key: keyBytes}
	addr := pub.Address().Bytes()
	derived, err := bech32.ConvertAndEncode(prefix, addr)
	if err != nil {
		return nil, fmt.Errorf("encode address: %w", err)
	}
	if derived != signer {
		return nil, fmt.Errorf("the key belongs to %s, not %s", derived, signer)
	}
	sigBytes, err := base64.StdEncoding.DecodeString(sig.Signature)
	if err != nil {
		return nil, errors.New("malformed signature")
	}
	msg, err := adr036SignBytes(signer, data)
	if err != nil {
		return nil, err
	}
	// VerifySignature hashes msg with sha256 and rejects high-S signatures
	if !pub.VerifySignature(msg, sigBytes) {
		return nil, errors.New("signature does not verify")
	}
	return addr, nil
}
