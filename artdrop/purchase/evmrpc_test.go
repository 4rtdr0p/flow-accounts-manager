package purchase

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"
)

func TestDecodeUint256(t *testing.T) {
	got, err := decodeUint256(json.RawMessage(`"0x00000000000000000000000000000000000000000000000000000000000003e8"`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("got %s, want 1000", got)
	}
}

func TestDecodeReserves(t *testing.T) {
	// reserve0 = 1234, reserve1 = 5678, timestamp = 1700000000
	raw := hexResult(big.NewInt(1234), big.NewInt(5678), big.NewInt(1700000000))
	r0, r1, err := decodeReserves(raw)
	if err != nil {
		t.Fatal(err)
	}
	if r0.Cmp(big.NewInt(1234)) != 0 || r1.Cmp(big.NewInt(5678)) != 0 {
		t.Fatalf("got r0=%s r1=%s, want 1234/5678", r0, r1)
	}
}

func TestDecodeSlot0(t *testing.T) {
	// A representative sqrtPriceX96; slot0 has extra words after it, decode must
	// read only the first word.
	sqrtP := new(big.Int).Lsh(big.NewInt(6178024), 96)
	raw := hexResult(sqrtP, big.NewInt(-100000), big.NewInt(3))
	got, err := decodeSlot0(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(sqrtP) != 0 {
		t.Fatalf("got %s, want %s", got, sqrtP)
	}
}

func TestDecodeHexBytesRevert(t *testing.T) {
	if _, err := decodeHexBytes(json.RawMessage(`"0x"`)); err == nil {
		t.Fatal("expected error for empty (reverted) result")
	}
	if _, err := decodeHexBytes(nil); err == nil {
		t.Fatal("expected error for nil result")
	}
}

func TestBatchCallURLFailover(t *testing.T) {
	fake := newFake()
	fake.responses[fkey("0xpool", selGetReserves)] = hexResult(big.NewInt(1), big.NewInt(2), big.NewInt(3))
	fake.failFirstURLs = 1 // first URL times out, second succeeds

	c := &evmClient{urls: []string{"http://dead", "http://live"}, tr: fake}
	results, err := c.BatchCall(context.Background(), []evmCall{{To: "0xpool", Data: selGetReserves}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0] == nil {
		t.Fatalf("expected 1 non-nil result after failover, got %#v", results)
	}
	if fake.posts != 2 {
		t.Fatalf("expected 2 Post attempts (failover), got %d", fake.posts)
	}
}

func TestBatchCallPerCallRevert(t *testing.T) {
	fake := newFake()
	fake.responses[fkey("0xok", selGetReserves)] = hexResult(big.NewInt(1), big.NewInt(2), big.NewInt(3))
	// "0xbad" has no canned response -> per-call revert -> nil slot, no batch error.

	c := &evmClient{urls: []string{"http://x"}, tr: fake}
	results, err := c.BatchCall(context.Background(), []evmCall{
		{To: "0xok", Data: selGetReserves},
		{To: "0xbad", Data: selGetReserves},
	})
	if err != nil {
		t.Fatal(err)
	}
	if results[0] == nil {
		t.Fatal("expected result[0] present")
	}
	if results[1] != nil {
		t.Fatal("expected result[1] nil (reverted call)")
	}
}

func TestBatchCallAllURLsFail(t *testing.T) {
	fake := newFake()
	fake.failFirstURLs = 2
	c := &evmClient{urls: []string{"http://a", "http://b"}, tr: fake}
	if _, err := c.BatchCall(context.Background(), []evmCall{{To: "0xpool", Data: selGetReserves}}); err == nil {
		t.Fatal("expected error when all URLs fail")
	}
}
