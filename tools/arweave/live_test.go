package arweave

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestAgainstRealTransactions checks this package's hashing and signature
// rules against transactions that Arweave nodes accepted: for each id in
// ARWEAVE_LIVE_TX_IDS (comma separated) it fetches the transaction and its
// data from ARWEAVE_GATEWAY (default https://arweave.net), recomputes the
// data root from the data, and verifies the signature and id. It needs the
// network, so it is skipped unless the variable is set. The offline fixture
// in tx_test.go was produced by running it.
func TestAgainstRealTransactions(t *testing.T) {
	ids := os.Getenv("ARWEAVE_LIVE_TX_IDS")
	if ids == "" {
		t.Skip("set ARWEAVE_LIVE_TX_IDS to run against the network")
	}
	gateway := os.Getenv("ARWEAVE_GATEWAY")
	if gateway == "" {
		gateway = "https://arweave.net"
	}
	client := &http.Client{Timeout: 120 * time.Second}
	get := func(path string) []byte {
		resp, err := client.Get(gateway + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status %d, %v", path, resp.StatusCode, err)
		}
		return body
	}
	for _, id := range strings.Split(ids, ",") {
		id = strings.TrimSpace(id)
		t.Run(id, func(t *testing.T) {
			var tx Transaction
			if err := json.Unmarshal(get("/tx/"+id), &tx); err != nil {
				t.Fatal(err)
			}
			if err := tx.Verify(); err != nil {
				t.Fatalf("signature: %v", err)
			}
			data := get("/" + id)
			if strconv.Itoa(len(data)) != tx.DataSize {
				t.Fatalf("gateway served %d bytes, data_size is %s", len(data), tx.DataSize)
			}
			root := DataRoot(data)
			if got := b64.EncodeToString(root[:]); got != tx.DataRoot {
				t.Fatalf("data root %s, transaction has %s", got, tx.DataRoot)
			}
			t.Logf("verified: %s bytes, %d chunk(s), %d tags", tx.DataSize, len(chunkEnds(len(data))), len(tx.Tags))
		})
	}
}
