package arweave

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Client talks to an Arweave gateway or node.
type Client struct {
	// Gateway is the base URL, for example https://arweave.net.
	Gateway string
	HTTP    *http.Client
}

// NewClient returns a client for a gateway with a 60-second request timeout.
func NewClient(gateway string) *Client {
	return &Client{Gateway: strings.TrimRight(gateway, "/"), HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// maxResponse bounds what the client reads from a gateway.
const maxResponse = MaxInlineData + 1<<20

func (c *Client) do(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Gateway+path, rd)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if len(out) > maxResponse {
		return resp.StatusCode, nil, errors.New("arweave: response is too large")
	}
	return resp.StatusCode, out, nil
}

var (
	anchorRe = regexp.MustCompile(`^[A-Za-z0-9_-]{43,64}$`)
	digitsRe = regexp.MustCompile(`^[0-9]{1,30}$`)
	idRe     = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

// ValidID reports whether s has the shape of a transaction id.
func ValidID(s string) bool { return idRe.MatchString(s) }

// Anchor returns a recent block hash to put in a new transaction's last_tx.
func (c *Client) Anchor(ctx context.Context) (string, error) {
	status, body, err := c.do(ctx, http.MethodGet, "/tx_anchor", nil)
	if err != nil {
		return "", err
	}
	anchor := strings.TrimSpace(string(body))
	if status != http.StatusOK || !anchorRe.MatchString(anchor) {
		return "", fmt.Errorf("arweave: tx_anchor: status %d", status)
	}
	return anchor, nil
}

// Price returns the reward, in winston, for a transaction storing size bytes.
func (c *Client) Price(ctx context.Context, size int) (string, error) {
	status, body, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/price/%d", size), nil)
	if err != nil {
		return "", err
	}
	price := strings.TrimSpace(string(body))
	if status != http.StatusOK || !digitsRe.MatchString(price) {
		return "", fmt.Errorf("arweave: price: status %d", status)
	}
	return price, nil
}

// Balance returns an address's balance in winston.
func (c *Client) Balance(ctx context.Context, address string) (string, error) {
	status, body, err := c.do(ctx, http.MethodGet, "/wallet/"+address+"/balance", nil)
	if err != nil {
		return "", err
	}
	bal := strings.TrimSpace(string(body))
	if status != http.StatusOK || !digitsRe.MatchString(bal) {
		return "", fmt.Errorf("arweave: balance: status %d", status)
	}
	return bal, nil
}

// Submit posts a signed transaction. A transaction the node already has is
// not an error: posting is safe to repeat after a restart.
func (c *Client) Submit(ctx context.Context, tx *Transaction) error {
	if tx.Signature == "" || tx.ID == "" {
		return errors.New("arweave: transaction is not signed")
	}
	raw, err := json.Marshal(tx)
	if err != nil {
		return err
	}
	status, body, err := c.do(ctx, http.MethodPost, "/tx", raw)
	if err != nil {
		return err
	}
	if status == http.StatusOK || status == http.StatusAccepted || status == http.StatusAlreadyReported {
		return nil
	}
	msg := strings.TrimSpace(string(body))
	if len(msg) > 200 {
		msg = msg[:200]
	}
	if strings.Contains(strings.ToLower(msg), "already processed") {
		return nil
	}
	return fmt.Errorf("arweave: submit %s: status %d: %s", tx.ID, status, msg)
}

// ErrNotFound means the gateway does not know the transaction: it was never
// posted there, or it was dropped before being mined.
var ErrNotFound = errors.New("arweave: transaction not found")

// Confirmations returns how many blocks have confirmed a transaction: 0
// while it is pending, and ErrNotFound when the gateway does not know it.
func (c *Client) Confirmations(ctx context.Context, id string) (int64, error) {
	if !ValidID(id) {
		return 0, errors.New("arweave: not a transaction id")
	}
	status, body, err := c.do(ctx, http.MethodGet, "/tx/"+id+"/status", nil)
	if err != nil {
		return 0, err
	}
	switch status {
	case http.StatusOK:
		var st struct {
			Confirmations int64 `json:"number_of_confirmations"`
		}
		if err := json.Unmarshal(body, &st); err != nil {
			return 0, fmt.Errorf("arweave: status of %s: %w", id, err)
		}
		return st.Confirmations, nil
	case http.StatusAccepted:
		return 0, nil // pending
	case http.StatusNotFound:
		return 0, ErrNotFound
	default:
		return 0, fmt.Errorf("arweave: status of %s: status %d", id, status)
	}
}

// Data fetches a transaction's data through the gateway.
func (c *Client) Data(ctx context.Context, id string) ([]byte, error) {
	if !ValidID(id) {
		return nil, errors.New("arweave: not a transaction id")
	}
	status, body, err := c.do(ctx, http.MethodGet, "/"+id, nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("arweave: data of %s: status %d", id, status)
	}
	return body, nil
}

// Store builds, signs and posts a data transaction and returns it. The
// caller keeps the returned transaction (or at least its id) so that a
// restart resubmits the same one instead of paying for a second.
func (c *Client) Store(ctx context.Context, w *Wallet, data []byte, tags []Tag) (*Transaction, error) {
	anchor, err := c.Anchor(ctx)
	if err != nil {
		return nil, err
	}
	// The price covers the data and the transaction's own fields.
	reward, err := c.Price(ctx, len(data))
	if err != nil {
		return nil, err
	}
	tx, err := NewDataTx(data, tags, anchor, reward)
	if err != nil {
		return nil, err
	}
	if err := w.Sign(tx); err != nil {
		return nil, err
	}
	if err := c.Submit(ctx, tx); err != nil {
		return tx, err
	}
	return tx, nil
}
