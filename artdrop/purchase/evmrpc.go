package purchase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// evmCall is a single eth_call: invoke method selector + args (Data) on
// contract To, at the latest block.
type evmCall struct {
	To   string // 0x-prefixed 20-byte contract address
	Data string // 0x-prefixed calldata (4-byte selector + ABI args)
}

// rpcTransport performs one JSON-RPC POST to a single URL and returns the raw
// response body. It is an interface so tests can inject canned responses
// without a live node; the production implementation is httpTransport.
type rpcTransport interface {
	Post(ctx context.Context, url string, body []byte) ([]byte, error)
}

// httpTransport is the net/http-backed rpcTransport.
type httpTransport struct {
	client *http.Client
}

func (t httpTransport) Post(ctx context.Context, url string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("rpc http status %d: %s", resp.StatusCode, strings.TrimSpace(buf.String()))
	}
	return buf.Bytes(), nil
}

// evmClient is a minimal batched JSON-RPC eth_call client. It has no direct
// go-ethereum dependency: requests are built with encoding/json and results
// decoded with math/big. urls are tried in order (failover) on transport error.
type evmClient struct {
	urls []string
	tr   rpcTransport
}

// newEVMClient builds an evmClient over net/http with the given per-request
// timeout. urls is the failover list (first that answers wins).
func newEVMClient(urls []string, timeout time.Duration) *evmClient {
	return &evmClient{
		urls: urls,
		tr:   httpTransport{client: &http.Client{Timeout: timeout}},
	}
}

type jsonRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type jsonRPCResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *jsonRPCError   `json:"error"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ethCallParams struct {
	To   string `json:"to"`
	Data string `json:"data"`
}

// BatchCall executes every call in a single JSON-RPC array POST (one
// round-trip). URLs are tried in order until one returns a well-formed batch
// response. The returned slice is parallel to calls: an entry is nil when that
// specific call reverted or was absent from the response (a per-call failure,
// e.g. a bad pool address, which the caller drops without failing the whole
// read). An error is returned only when every URL failed at the transport or
// batch-decode level.
func (c *evmClient) BatchCall(ctx context.Context, calls []evmCall) ([]json.RawMessage, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	reqs := make([]jsonRPCRequest, len(calls))
	for i, call := range calls {
		reqs[i] = jsonRPCRequest{
			JSONRPC: "2.0",
			ID:      i,
			Method:  "eth_call",
			Params:  []interface{}{ethCallParams{To: call.To, Data: call.Data}, "latest"},
		}
	}
	body, err := json.Marshal(reqs)
	if err != nil {
		return nil, fmt.Errorf("marshal rpc batch: %w", err)
	}

	var lastErr error
	for _, url := range c.urls {
		raw, err := c.tr.Post(ctx, strings.TrimSpace(url), body)
		if err != nil {
			lastErr = err
			continue
		}
		var responses []jsonRPCResponse
		if err := json.Unmarshal(raw, &responses); err != nil {
			lastErr = fmt.Errorf("decode rpc batch from %s: %w", url, err)
			continue
		}
		out := make([]json.RawMessage, len(calls))
		for _, r := range responses {
			if r.ID < 0 || r.ID >= len(calls) {
				continue
			}
			if r.Error != nil {
				// Per-call revert (e.g. the pool address is wrong): leave the
				// slot nil so the caller drops just that pool.
				continue
			}
			out[r.ID] = r.Result
		}
		return out, nil
	}
	return nil, fmt.Errorf("all rpc urls failed: %w", lastErr)
}

// decodeHexBytes unmarshals a JSON-RPC 0x-hex result string into its raw bytes.
func decodeHexBytes(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty result")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("result is not a hex string: %w", err)
	}
	s = strings.TrimPrefix(strings.TrimSpace(s), "0x")
	if s == "" {
		return nil, fmt.Errorf("empty hex result (likely a reverted call)")
	}
	if len(s)%2 != 0 {
		s = "0" + s
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(b); i++ {
		var v int
		if _, err := fmt.Sscanf(s[i*2:i*2+2], "%02x", &v); err != nil {
			return nil, fmt.Errorf("bad hex in result: %w", err)
		}
		b[i] = byte(v)
	}
	return b, nil
}

// word returns the i-th 32-byte ABI word of b as a big.Int, or an error if b is
// too short.
func word(b []byte, i int) (*big.Int, error) {
	start := i * 32
	end := start + 32
	if end > len(b) {
		return nil, fmt.Errorf("result too short for word %d (have %d bytes)", i, len(b))
	}
	return new(big.Int).SetBytes(b[start:end]), nil
}

// decodeUint256 decodes a single 32-byte ABI word (uint256/uint160/uint112) as
// a big.Int. Used for balanceOf and, via decodeSlot0, sqrtPriceX96.
func decodeUint256(raw json.RawMessage) (*big.Int, error) {
	b, err := decodeHexBytes(raw)
	if err != nil {
		return nil, err
	}
	return word(b, 0)
}

// decodeReserves decodes a UniswapV2 getReserves() result: three ABI words
// (uint112 reserve0, uint112 reserve1, uint32 blockTimestampLast). Only the two
// reserves are returned.
func decodeReserves(raw json.RawMessage) (reserve0, reserve1 *big.Int, err error) {
	b, err := decodeHexBytes(raw)
	if err != nil {
		return nil, nil, err
	}
	reserve0, err = word(b, 0)
	if err != nil {
		return nil, nil, err
	}
	reserve1, err = word(b, 1)
	if err != nil {
		return nil, nil, err
	}
	return reserve0, reserve1, nil
}

// decodeSlot0 decodes a UniswapV3 slot0() result and returns sqrtPriceX96 (the
// first 32-byte word; the low 160 bits hold the value, right-aligned).
func decodeSlot0(raw json.RawMessage) (*big.Int, error) {
	return decodeUint256(raw)
}
