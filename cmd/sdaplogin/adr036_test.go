package main

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

// A vector made with cosmjs (serializeSignDoc + Secp256k1.createSignature),
// the library whose sign doc Keplr's signArbitrary produces: the Go sign
// bytes must match it byte for byte or no real wallet signature verifies.
const (
	vecPub       = "AgLXVXtcu4Cs8rIbxfCep3tfgRhOMN3fT0LV9B4TqyAi"
	vecSigner    = "sprkdrm1f6d7jjsknyvthgy5e0c8heragxt4tq95xcmpgh"
	vecAddrHex   = "4e9be94a169918bba094cbf07be47d41975580b4"
	vecData      = "Sign in to social.phoenix.example as a Spark Dream member.\n\nNonce: abc<&>\nIssued: 2026-09-27T00:00:00Z"
	vecSignature = "ReWq9zx99uonmG4f5P/ssSu5NG215fjWxSIZui54I4srxl+4MUhHaDfZea7TtA8eDBdVB6pZsvAbXCu5hG36oA=="
	vecSignBytes = `{"account_number":"0","chain_id":"","fee":{"amount":[],"gas":"0"},"memo":"","msgs":[{"type":"sign/MsgSignData","value":{"data":"U2lnbiBpbiB0byBzb2NpYWwucGhvZW5peC5leGFtcGxlIGFzIGEgU3BhcmsgRHJlYW0gbWVtYmVyLgoKTm9uY2U6IGFiYzwmPgpJc3N1ZWQ6IDIwMjYtMDktMjdUMDA6MDA6MDBa","signer":"sprkdrm1f6d7jjsknyvthgy5e0c8heragxt4tq95xcmpgh"}}],"sequence":"0"}`
)

func vecSig() WalletSignature {
	var s WalletSignature
	s.PubKey.Type = secp256k1PubKeyType
	s.PubKey.Value = vecPub
	s.Signature = vecSignature
	return s
}

func TestADR036SignBytesMatchCosmjs(t *testing.T) {
	got, err := adr036SignBytes(vecSigner, vecData)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != vecSignBytes {
		t.Fatalf("sign bytes differ from cosmjs:\n got %s\nwant %s", got, vecSignBytes)
	}
}

func TestVerifyADR036(t *testing.T) {
	flip := func(b64 string) string {
		b, _ := base64.StdEncoding.DecodeString(b64)
		b[10] ^= 1
		return base64.StdEncoding.EncodeToString(b)
	}
	cases := []struct {
		name    string
		prefix  string
		signer  string
		data    string
		mutate  func(*WalletSignature)
		wantErr string
	}{
		{name: "valid", prefix: "sprkdrm", signer: vecSigner, data: vecData},
		{name: "tampered data", prefix: "sprkdrm", signer: vecSigner, data: vecData + ".", wantErr: "does not verify"},
		{name: "tampered signature", prefix: "sprkdrm", signer: vecSigner, data: vecData,
			mutate: func(s *WalletSignature) { s.Signature = flip(s.Signature) }, wantErr: "does not verify"},
		{name: "another signer", prefix: "sprkdrm", signer: "sprkdrm1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqn2ccpe", data: vecData, wantErr: "belongs to"},
		{name: "another chain's prefix", prefix: "aurora", signer: vecSigner, data: vecData, wantErr: "belongs to"},
		{name: "wrong key type", prefix: "sprkdrm", signer: vecSigner, data: vecData,
			mutate: func(s *WalletSignature) { s.PubKey.Type = "tendermint/PubKeyEd25519" }, wantErr: "unsupported key type"},
		{name: "malformed key", prefix: "sprkdrm", signer: vecSigner, data: vecData,
			mutate: func(s *WalletSignature) { s.PubKey.Value = "AAAA" }, wantErr: "malformed public key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sig := vecSig()
			if tc.mutate != nil {
				tc.mutate(&sig)
			}
			addr, err := verifyADR036(tc.prefix, tc.signer, tc.data, sig)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if hex.EncodeToString(addr) != vecAddrHex {
				t.Fatalf("address %x, want %s", addr, vecAddrHex)
			}
		})
	}
}
