// Package arweave builds, signs and posts Arweave data transactions, and
// reads them back. It is a clean-room implementation of the parts of the
// Arweave transaction format (format 2) a storage operator needs, written
// from the protocol's public description; it uses only the standard library.
//
// What it covers: loading a wallet (an RSA key in JWK form), computing a
// transaction's data root (the merkle root over its chunks), the "deep hash"
// that is signed, RSA-PSS signing and verification, and a small HTTP client
// for a gateway (anchor, price, submit, status, data).
//
// What it does not cover: chunk uploads. A transaction's data is sent inline
// with it, which nodes accept up to MaxInlineData. That fits x/storage,
// whose files are at most a few megabytes; larger data needs the chunk
// endpoint and is refused here.
//
// The hashing and signature rules are checked in the tests against real
// transactions fetched from the network: their data roots are recomputed
// from their data and their signatures verified with this code.
package arweave

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strconv"
)

// MaxInlineData is the most data this package sends inside a transaction.
// Nodes accept more (about 12 MiB), with a margin kept here.
const MaxInlineData = 10 << 20

var b64 = base64.RawURLEncoding

// Tag is a name and value pair on a transaction, both base64url in JSON.
type Tag struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// NewTag builds a tag from plain text.
func NewTag(name, value string) Tag {
	return Tag{Name: b64.EncodeToString([]byte(name)), Value: b64.EncodeToString([]byte(value))}
}

// Transaction is a format-2 Arweave transaction as its JSON form.
type Transaction struct {
	Format    int    `json:"format"`
	ID        string `json:"id"`
	LastTx    string `json:"last_tx"`
	Owner     string `json:"owner"`
	Tags      []Tag  `json:"tags"`
	Target    string `json:"target"`
	Quantity  string `json:"quantity"`
	Data      string `json:"data"`
	DataSize  string `json:"data_size"`
	DataRoot  string `json:"data_root"`
	Reward    string `json:"reward"`
	Signature string `json:"signature"`
}

// Wallet is an Arweave wallet: an RSA key.
type Wallet struct {
	key *rsa.PrivateKey
}

type jwk struct {
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
	D   string `json:"d"`
	P   string `json:"p"`
	Q   string `json:"q"`
}

// ParseWallet reads a wallet from the JWK JSON a wallet tool exports.
func ParseWallet(raw []byte) (*Wallet, error) {
	var k jwk
	if err := json.Unmarshal(raw, &k); err != nil {
		return nil, fmt.Errorf("arweave: wallet is not JSON: %w", err)
	}
	if k.Kty != "RSA" {
		return nil, errors.New("arweave: wallet is not an RSA key")
	}
	num := func(name, s string) (*big.Int, error) {
		b, err := b64.DecodeString(s)
		if err != nil || len(b) == 0 {
			return nil, fmt.Errorf("arweave: wallet field %q is missing or not base64url", name)
		}
		return new(big.Int).SetBytes(b), nil
	}
	n, err := num("n", k.N)
	if err != nil {
		return nil, err
	}
	e, err := num("e", k.E)
	if err != nil {
		return nil, err
	}
	d, err := num("d", k.D)
	if err != nil {
		return nil, err
	}
	p, err := num("p", k.P)
	if err != nil {
		return nil, err
	}
	q, err := num("q", k.Q)
	if err != nil {
		return nil, err
	}
	if !e.IsInt64() || e.Int64() > 1<<31-1 {
		return nil, errors.New("arweave: wallet has an unusable public exponent")
	}
	key := &rsa.PrivateKey{
		PublicKey: rsa.PublicKey{N: n, E: int(e.Int64())},
		D:         d,
		Primes:    []*big.Int{p, q},
	}
	if err := key.Validate(); err != nil {
		return nil, fmt.Errorf("arweave: wallet key is not valid: %w", err)
	}
	key.Precompute()
	return &Wallet{key: key}, nil
}

// LoadWallet reads a wallet file.
func LoadWallet(path string) (*Wallet, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseWallet(raw)
}

// GenerateWallet makes a new wallet. Tests use it; a real wallet comes from
// a wallet tool and must be funded before it can post anything.
func GenerateWallet(bits int) (*Wallet, error) {
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, err
	}
	return &Wallet{key: key}, nil
}

// JWK returns the wallet in the JSON form ParseWallet reads.
func (w *Wallet) JWK() ([]byte, error) {
	e := big.NewInt(int64(w.key.E))
	return json.Marshal(jwk{
		Kty: "RSA",
		N:   b64.EncodeToString(w.key.N.Bytes()),
		E:   b64.EncodeToString(e.Bytes()),
		D:   b64.EncodeToString(w.key.D.Bytes()),
		P:   b64.EncodeToString(w.key.Primes[0].Bytes()),
		Q:   b64.EncodeToString(w.key.Primes[1].Bytes()),
	})
}

// Owner is the wallet's public modulus, as transactions carry it.
func (w *Wallet) Owner() string { return b64.EncodeToString(w.key.N.Bytes()) }

// Address is the wallet's address: the sha256 of its public modulus.
func (w *Wallet) Address() string { return AddressOf(w.Owner()) }

// AddressOf returns the address of an owner field ("" if it is not base64url).
func AddressOf(owner string) string {
	n, err := b64.DecodeString(owner)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(n)
	return b64.EncodeToString(sum[:])
}

// deepHash is Arweave's hash over a nested list of byte strings (SHA-384).
// A leaf is a []byte; a list is a []any of leaves and lists.
func deepHash(v any) [48]byte {
	switch x := v.(type) {
	case []byte:
		tag := sha512.Sum384([]byte("blob" + strconv.Itoa(len(x))))
		body := sha512.Sum384(x)
		return sha512.Sum384(append(tag[:], body[:]...))
	case []any:
		acc := sha512.Sum384([]byte("list" + strconv.Itoa(len(x))))
		for _, item := range x {
			h := deepHash(item)
			acc = sha512.Sum384(append(acc[:], h[:]...))
		}
		return acc
	default:
		panic(fmt.Sprintf("arweave: deepHash of %T", v))
	}
}

// signingHash is what a format-2 transaction's signature covers.
func (tx *Transaction) signingHash() ([48]byte, error) {
	if tx.Format != 2 {
		return [48]byte{}, fmt.Errorf("arweave: only format 2 transactions are supported, got %d", tx.Format)
	}
	dec := func(name, s string) ([]byte, error) {
		b, err := b64.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("arweave: field %q is not base64url", name)
		}
		return b, nil
	}
	owner, err := dec("owner", tx.Owner)
	if err != nil {
		return [48]byte{}, err
	}
	target, err := dec("target", tx.Target)
	if err != nil {
		return [48]byte{}, err
	}
	lastTx, err := dec("last_tx", tx.LastTx)
	if err != nil {
		return [48]byte{}, err
	}
	dataRoot, err := dec("data_root", tx.DataRoot)
	if err != nil {
		return [48]byte{}, err
	}
	tags := make([]any, 0, len(tx.Tags))
	for _, t := range tx.Tags {
		name, err := dec("tag name", t.Name)
		if err != nil {
			return [48]byte{}, err
		}
		value, err := dec("tag value", t.Value)
		if err != nil {
			return [48]byte{}, err
		}
		tags = append(tags, []any{name, value})
	}
	return deepHash([]any{
		[]byte(strconv.Itoa(tx.Format)),
		owner,
		target,
		[]byte(tx.Quantity),
		[]byte(tx.Reward),
		lastTx,
		tags,
		[]byte(tx.DataSize),
		dataRoot,
	}), nil
}

// pssOptions: RSA-PSS with SHA-256 and a 32-byte salt, as wallets sign.
// Verification accepts any salt length, as nodes do.
var pssSign = &rsa.PSSOptions{SaltLength: 32, Hash: crypto.SHA256}
var pssVerify = &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthAuto, Hash: crypto.SHA256}

// Sign fills in the owner, signature and id.
func (w *Wallet) Sign(tx *Transaction) error {
	tx.Owner = w.Owner()
	h, err := tx.signingHash()
	if err != nil {
		return err
	}
	digest := sha256.Sum256(h[:])
	sig, err := rsa.SignPSS(rand.Reader, w.key, crypto.SHA256, digest[:], pssSign)
	if err != nil {
		return err
	}
	tx.Signature = b64.EncodeToString(sig)
	id := sha256.Sum256(sig)
	tx.ID = b64.EncodeToString(id[:])
	return nil
}

// Verify checks a transaction's signature against its owner and that its id
// is the hash of the signature.
func (tx *Transaction) Verify() error {
	n, err := b64.DecodeString(tx.Owner)
	if err != nil || len(n) == 0 {
		return errors.New("arweave: owner is not base64url")
	}
	sig, err := b64.DecodeString(tx.Signature)
	if err != nil {
		return errors.New("arweave: signature is not base64url")
	}
	h, err := tx.signingHash()
	if err != nil {
		return err
	}
	digest := sha256.Sum256(h[:])
	pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: 65537}
	if err := rsa.VerifyPSS(pub, crypto.SHA256, digest[:], sig, pssVerify); err != nil {
		return fmt.Errorf("arweave: signature does not verify: %w", err)
	}
	id := sha256.Sum256(sig)
	if tx.ID != b64.EncodeToString(id[:]) {
		return errors.New("arweave: id is not the hash of the signature")
	}
	return nil
}

// EncodeData encodes data as a transaction's data field carries it. A caller
// that kept a signed transaction without its data puts the data back with
// this before posting it again; the id does not change.
func EncodeData(data []byte) string { return b64.EncodeToString(data) }

// NewDataTx builds an unsigned transaction that stores data. anchor is a
// recent block hash from the gateway (Client.Anchor) and reward the price
// for the data's size in winston (Client.Price).
func NewDataTx(data []byte, tags []Tag, anchor, reward string) (*Transaction, error) {
	if len(data) == 0 {
		return nil, errors.New("arweave: no data")
	}
	if len(data) > MaxInlineData {
		return nil, fmt.Errorf("arweave: %d bytes is more than this package sends inline (%d)", len(data), MaxInlineData)
	}
	if tags == nil {
		tags = []Tag{}
	}
	root := DataRoot(data)
	return &Transaction{
		Format:   2,
		LastTx:   anchor,
		Tags:     tags,
		Target:   "",
		Quantity: "0",
		Data:     b64.EncodeToString(data),
		DataSize: strconv.Itoa(len(data)),
		DataRoot: b64.EncodeToString(root[:]),
		Reward:   reward,
	}, nil
}
