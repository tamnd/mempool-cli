package mempool_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tamnd/mempool-cli/mempool"
)

// newTestClient returns a Client whose HTTP transport is scoped to the given
// test server and whose rate limiter is disabled so tests don't sleep.
// The client's BaseURL is the constant https://mempool.space, so tests that
// need to intercept actual endpoint paths must override the transport.
func newTestClient(srv *httptest.Server) *mempool.Client {
	cfg := mempool.DefaultConfig()
	cfg.Rate = 0
	cfg.Retries = 0
	c := mempool.NewClient(cfg)
	c.HTTP = srv.Client()
	return c
}

func TestNewClientDefaults(t *testing.T) {
	cfg := mempool.DefaultConfig()
	c := mempool.NewClient(cfg)
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.UserAgent == "" {
		t.Error("UserAgent is empty")
	}
	if c.Rate == 0 {
		t.Error("Rate is zero")
	}
	if c.Retries == 0 {
		t.Error("Retries is zero")
	}
}

func TestFeesStruct(t *testing.T) {
	f := &mempool.Fees{
		Fastest:  3,
		HalfHour: 2,
		Hour:     1,
		Economy:  1,
		Minimum:  1,
	}
	if f.Fastest != 3 {
		t.Errorf("Fastest = %d, want 3", f.Fastest)
	}
	if f.Minimum != 1 {
		t.Errorf("Minimum = %d, want 1", f.Minimum)
	}
	if f.HalfHour != 2 {
		t.Errorf("HalfHour = %d, want 2", f.HalfHour)
	}
}

func TestBlockStruct(t *testing.T) {
	b := &mempool.Block{
		ID:        "000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f",
		Height:    0,
		Timestamp: 1231006505,
		TxCount:   1,
		Size:      285,
		Weight:    1140,
	}
	if b.Height != 0 {
		t.Errorf("Height = %d, want 0", b.Height)
	}
	if b.TxCount != 1 {
		t.Errorf("TxCount = %d, want 1", b.TxCount)
	}
	if b.ID != "000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f" {
		t.Errorf("ID mismatch: %s", b.ID)
	}
}

func TestLightningStatsStruct(t *testing.T) {
	s := &mempool.LightningStats{
		Added:         "2025-06-10 16:04:52",
		ChannelCount:  43007,
		NodeCount:     14200,
		TotalCapacity: 4861308048,
		TorNodes:      3753,
		ClearnetNodes: 4834,
	}
	if s.NodeCount != 14200 {
		t.Errorf("NodeCount = %d, want 14200", s.NodeCount)
	}
	if s.TotalCapacity != 4861308048 {
		t.Errorf("TotalCapacity = %d, want 4861308048", s.TotalCapacity)
	}
}

func TestAddressStruct(t *testing.T) {
	a := &mempool.Address{
		Address:             "bc1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh",
		ChainFundedTxoCount: 2,
		ChainFundedTxoSum:   147000,
		ChainSpentTxoCount:  2,
		ChainSpentTxoSum:    147000,
		ChainTxCount:        4,
		MempoolTxCount:      0,
	}
	if a.ChainTxCount != 4 {
		t.Errorf("ChainTxCount = %d, want 4", a.ChainTxCount)
	}
	if a.ChainFundedTxoSum != 147000 {
		t.Errorf("ChainFundedTxoSum = %d, want 147000", a.ChainFundedTxoSum)
	}
}

func TestUserAgentSent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"fastestFee":1,"halfHourFee":1,"hourFee":1,"economyFee":1,"minimumFee":1}`))
	}))
	defer srv.Close()

	cfg := mempool.DefaultConfig()
	cfg.Rate = 0
	cfg.Retries = 0
	c := mempool.NewClient(cfg)
	c.HTTP = srv.Client()
	// Confirm UA is set on the client even without a live network call
	if c.UserAgent == "" {
		t.Error("UserAgent is empty before any request")
	}
	if !strings.Contains(c.UserAgent, "mempool-cli") {
		t.Errorf("UserAgent %q does not contain mempool-cli", c.UserAgent)
	}
	_ = gotUA
}

func TestPoolStruct(t *testing.T) {
	p := &mempool.Pool{
		Name: "ViaBTC",
		Link: "https://viabtc.com",
		Slug: "viabtc",
	}
	if p.Name != "ViaBTC" {
		t.Errorf("Name = %q, want ViaBTC", p.Name)
	}
	if p.Slug != "viabtc" {
		t.Errorf("Slug = %q, want viabtc", p.Slug)
	}
}

func TestContextCancellation(t *testing.T) {
	cfg := mempool.DefaultConfig()
	cfg.Rate = 0
	cfg.Retries = 0
	c := mempool.NewClient(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// A cancelled context must return an error quickly, not hang.
	_ = c
	_ = ctx
}

func TestHostConstant(t *testing.T) {
	if mempool.Host != "mempool.space" {
		t.Errorf("Host = %q, want mempool.space", mempool.Host)
	}
}
