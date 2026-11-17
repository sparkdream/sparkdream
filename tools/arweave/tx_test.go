package arweave

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

var (
	testWalletOnce sync.Once
	testWallet     *Wallet
)

// wallet returns one small test wallet (2048 bits keeps the tests quick; a
// real Arweave wallet is 4096).
func wallet(t *testing.T) *Wallet {
	t.Helper()
	testWalletOnce.Do(func() {
		w, err := GenerateWallet(2048)
		if err != nil {
			t.Fatal(err)
		}
		testWallet = w
	})
	return testWallet
}

// A transaction the network accepted verifies with this package's signing
// rules. The header in testdata is a real format-2 transaction (its data,
// 344 KiB in two chunks, is not kept; TestAgainstRealTransactions fetches it
// and checks the data root too).
func TestVerifyRealTransaction(t *testing.T) {
	raw, err := os.ReadFile("testdata/real_tx_header.json")
	if err != nil {
		t.Fatal(err)
	}
	var tx Transaction
	if err := json.Unmarshal(raw, &tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Verify(); err != nil {
		t.Fatalf("a real transaction must verify: %v", err)
	}
	if len(AddressOf(tx.Owner)) != 43 {
		t.Fatalf("address %q", AddressOf(tx.Owner))
	}

	// Any signed field changed breaks it.
	for name, mut := range map[string]func(*Transaction){
		"reward":    func(x *Transaction) { x.Reward += "0" },
		"data_size": func(x *Transaction) { x.DataSize = "1" },
		"data_root": func(x *Transaction) { x.DataRoot = b64.EncodeToString(make([]byte, 32)) },
		"last_tx":   func(x *Transaction) { x.LastTx = b64.EncodeToString(make([]byte, 48)) },
		"quantity":  func(x *Transaction) { x.Quantity = "5" },
		"tag value": func(x *Transaction) { x.Tags[0].Value = b64.EncodeToString([]byte("other")) },
		"tag order": func(x *Transaction) { x.Tags[0], x.Tags[1] = x.Tags[1], x.Tags[0] },
		"extra tag": func(x *Transaction) { x.Tags = append(x.Tags, NewTag("a", "b")) },
		"id":        func(x *Transaction) { x.ID = b64.EncodeToString(make([]byte, 32)) },
	} {
		bad := tx
		bad.Tags = append([]Tag(nil), tx.Tags...)
		mut(&bad)
		if bad.Verify() == nil {
			t.Errorf("verified after changing %s", name)
		}
	}
}

func TestSignAndVerify(t *testing.T) {
	w := wallet(t)
	data := []byte("a permanent copy")
	tx, err := NewDataTx(data, []Tag{NewTag("Content-Type", "image/png"), NewTag("App-Name", "SparkDream")},
		b64.EncodeToString(make([]byte, 48)), "12345")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Verify(); err == nil {
		t.Fatal("an unsigned transaction verified")
	}
	if err := w.Sign(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Verify(); err != nil {
		t.Fatal(err)
	}
	if !ValidID(tx.ID) || tx.Owner != w.Owner() || tx.Format != 2 || tx.Quantity != "0" || tx.DataSize != "16" {
		t.Fatalf("transaction: %+v", tx)
	}
	sig, _ := b64.DecodeString(tx.Signature)
	if id := sha256.Sum256(sig); tx.ID != b64.EncodeToString(id[:]) {
		t.Fatal("id is not the hash of the signature")
	}
	if tx.Tags[0].Name != b64.EncodeToString([]byte("Content-Type")) {
		t.Fatalf("tag encoding: %+v", tx.Tags[0])
	}

	if _, err := NewDataTx(nil, nil, "", "1"); err == nil {
		t.Fatal("accepted empty data")
	}
	if _, err := NewDataTx(make([]byte, MaxInlineData+1), nil, "", "1"); err == nil {
		t.Fatal("accepted data too large to send inline")
	}
}

func TestWalletRoundTrip(t *testing.T) {
	w := wallet(t)
	raw, err := w.JWK()
	if err != nil {
		t.Fatal(err)
	}
	again, err := ParseWallet(raw)
	if err != nil {
		t.Fatal(err)
	}
	if again.Address() != w.Address() || len(w.Address()) != 43 {
		t.Fatalf("address changed: %s / %s", w.Address(), again.Address())
	}
	// A key signed by the reloaded wallet verifies.
	tx, _ := NewDataTx([]byte("x"), nil, "", "1")
	if err := again.Sign(tx); err != nil || tx.Verify() != nil {
		t.Fatalf("reloaded wallet: %v", err)
	}

	path := t.TempDir() + "/wallet.json"
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWallet(path); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "{}", `{"kty":"EC"}`, `{"kty":"RSA","n":"!!","e":"AQAB"}`, strings.Replace(string(raw), `"d":"`, `"d":"AA`, 1)} {
		if _, err := ParseWallet([]byte(bad)); err == nil {
			t.Errorf("parsed a bad wallet: %.40s", bad)
		}
	}
}

// The chunking rule of inline data: 256 KiB chunks, the rest as the last
// chunk however small (never the half-and-half split of chunked uploads),
// and a final (possibly empty) chunk always.
func TestChunkEnds(t *testing.T) {
	const k = 1024
	cases := []struct {
		n    int
		want []int
	}{
		{1, []int{1}},
		{256*k - 1, []int{256*k - 1}},
		{256 * k, []int{256 * k, 256 * k}}, // exact multiple: an empty final chunk
		{256*k + 1, []int{256 * k, 256*k + 1}},
		{256*k + 32*k - 1, []int{256 * k, 288*k - 1}},
		{256*k + 32*k, []int{256 * k, 288 * k}},
		{344192, []int{262144, 344192}},
		{512 * k, []int{256 * k, 512 * k, 512 * k}},
	}
	for _, c := range cases {
		got := chunkEnds(c.n)
		if len(got) != len(c.want) {
			t.Errorf("chunkEnds(%d) = %v, want %v", c.n, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("chunkEnds(%d) = %v, want %v", c.n, got, c.want)
				break
			}
		}
	}
}

// The root depends on every byte and on the length.
func TestDataRootIsSensitive(t *testing.T) {
	data := bytes.Repeat([]byte{7}, 300*1024)
	root := DataRoot(data)
	flipped := append([]byte(nil), data...)
	flipped[299*1024] ^= 1
	if DataRoot(flipped) == root {
		t.Fatal("root unchanged after a byte changed")
	}
	if DataRoot(data[:len(data)-1]) == root {
		t.Fatal("root unchanged after truncation")
	}
	if DataRoot(data) != root {
		t.Fatal("root is not deterministic")
	}
}

func TestClient(t *testing.T) {
	w := wallet(t)
	var posted []Transaction
	stored := map[string][]byte{}
	confirmations := map[string]int64{}
	mux := http.NewServeMux()
	write := func(rw http.ResponseWriter, body string) {
		if _, err := io.WriteString(rw, body); err != nil {
			t.Error(err)
		}
	}
	mux.HandleFunc("/tx_anchor", func(rw http.ResponseWriter, _ *http.Request) {
		write(rw, b64.EncodeToString(make([]byte, 48)))
	})
	mux.HandleFunc("/price/", func(rw http.ResponseWriter, r *http.Request) {
		write(rw, "1000"+strings.TrimPrefix(r.URL.Path, "/price/"))
	})
	mux.HandleFunc("/wallet/", func(rw http.ResponseWriter, _ *http.Request) { write(rw, "5000000") })
	mux.HandleFunc("/tx", func(rw http.ResponseWriter, r *http.Request) {
		var tx Transaction
		if err := json.NewDecoder(r.Body).Decode(&tx); err != nil || tx.Verify() != nil {
			http.Error(rw, "invalid transaction", http.StatusBadRequest)
			return
		}
		data, _ := b64.DecodeString(tx.Data)
		if root := DataRoot(data); b64.EncodeToString(root[:]) != tx.DataRoot {
			http.Error(rw, "bad data root", http.StatusBadRequest)
			return
		}
		if _, dup := stored[tx.ID]; dup {
			http.Error(rw, "Transaction already processed.", http.StatusBadRequest)
			return
		}
		stored[tx.ID] = data
		posted = append(posted, tx)
	})
	mux.HandleFunc("/tx/", func(rw http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/tx/"), "/status")
		if _, ok := stored[id]; !ok {
			http.NotFound(rw, r)
			return
		}
		n, mined := confirmations[id]
		if !mined {
			rw.WriteHeader(http.StatusAccepted)
			write(rw, "Pending")
			return
		}
		if err := json.NewEncoder(rw).Encode(map[string]any{"block_height": 10, "number_of_confirmations": n}); err != nil {
			t.Error(err)
		}
	})
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		data, ok := stored[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(rw, r)
			return
		}
		write(rw, string(data))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(srv.URL + "/")
	ctx := context.Background()

	if bal, err := c.Balance(ctx, w.Address()); err != nil || bal != "5000000" {
		t.Fatalf("balance %q %v", bal, err)
	}
	data := []byte("the file")
	tx, err := c.Store(ctx, w, data, []Tag{NewTag("SHA-256", "abc")})
	if err != nil {
		t.Fatal(err)
	}
	if len(posted) != 1 || posted[0].ID != tx.ID || tx.Reward != "10008" {
		t.Fatalf("posted %+v", posted)
	}
	// Posting the same transaction again (after a restart) is not an error
	// and stores nothing twice.
	if err := c.Submit(ctx, tx); err != nil {
		t.Fatalf("resubmit: %v", err)
	}
	if len(posted) != 1 {
		t.Fatal("stored twice")
	}

	if n, err := c.Confirmations(ctx, tx.ID); err != nil || n != 0 {
		t.Fatalf("pending: %d %v", n, err)
	}
	confirmations[tx.ID] = 12
	if n, err := c.Confirmations(ctx, tx.ID); err != nil || n != 12 {
		t.Fatalf("confirmed: %d %v", n, err)
	}
	got, err := c.Data(ctx, tx.ID)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("data: %q %v", got, err)
	}

	unknown := b64.EncodeToString(make([]byte, 32))
	if _, err := c.Confirmations(ctx, unknown); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown transaction: %v", err)
	}
	if _, err := c.Data(ctx, unknown); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown data: %v", err)
	}
	if _, err := c.Confirmations(ctx, "not an id"); err == nil {
		t.Fatal("accepted a malformed id")
	}
	if err := c.Submit(ctx, &Transaction{Format: 2}); err == nil {
		t.Fatal("submitted an unsigned transaction")
	}
	// A node that refuses the transaction is an error.
	bad := *tx
	bad.Reward = "1"
	if err := c.Submit(ctx, &bad); err == nil {
		t.Fatal("a refused transaction was reported as stored")
	}
}
