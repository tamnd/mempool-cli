// Package mempool is the library behind the mempool command line:
// the HTTP client, request shaping, and the typed data models for mempool.space.
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public site throws under load.
package mempool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultUserAgent identifies the client to mempool.space.
const DefaultUserAgent = "mempool-cli/dev (+https://github.com/tamnd/mempool-cli)"

// Host is the site this client talks to.
const Host = "mempool.space"

// BaseURL is the root every request is built from.
const BaseURL = "https://" + Host

// Config holds the client configuration.
type Config struct {
	UserAgent string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		UserAgent: DefaultUserAgent,
		Rate:      300 * time.Millisecond,
		Retries:   5,
		Timeout:   30 * time.Second,
	}
}

// Client talks to mempool.space over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	Rate      time.Duration
	Retries   int

	last time.Time
}

// NewClient returns a Client with sensible defaults.
func NewClient(cfg Config) *Client {
	if cfg.UserAgent == "" {
		cfg.UserAgent = DefaultUserAgent
	}
	if cfg.Rate == 0 {
		cfg.Rate = 300 * time.Millisecond
	}
	if cfg.Retries == 0 {
		cfg.Retries = 5
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Client{
		HTTP:      &http.Client{Timeout: cfg.Timeout},
		UserAgent: cfg.UserAgent,
		Rate:      cfg.Rate,
		Retries:   cfg.Retries,
	}
}

// get fetches url and returns the response body. It paces and retries according
// to the client's settings.
func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", url, lastErr)
}

func (c *Client) do(ctx context.Context, url string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// --- output types ---

// Fees holds the current recommended fee rates in sat/vByte.
type Fees struct {
	Fastest  int `kit:"id" json:"fastestFee"`
	HalfHour int `json:"halfHourFee"`
	Hour     int `json:"hourFee"`
	Economy  int `json:"economyFee"`
	Minimum  int `json:"minimumFee"`
}

// Pool describes the mining pool that found a block.
type Pool struct {
	Name string `json:"name"`
	Link string `json:"link"`
	Slug string `json:"slug"`
}

// Block holds summary data for a single Bitcoin block.
type Block struct {
	ID        string `kit:"id" json:"id"`
	Height    int    `json:"height"`
	Timestamp int64  `json:"timestamp"`
	TxCount   int    `json:"tx_count"`
	Size      int    `json:"size"`
	Weight    int    `json:"weight"`
}

// LightningStats holds the latest Lightning Network statistics.
type LightningStats struct {
	Added         string `kit:"id" json:"added"`
	ChannelCount  int    `json:"channel_count"`
	NodeCount     int    `json:"node_count"`
	TotalCapacity int64  `json:"total_capacity"`
	TorNodes      int    `json:"tor_nodes"`
	ClearnetNodes int    `json:"clearnet_nodes"`
}

// Address holds the chain and mempool stats for a Bitcoin address.
type Address struct {
	Address              string `kit:"id" json:"address"`
	ChainFundedTxoCount  int    `json:"chain_funded_txo_count"`
	ChainFundedTxoSum    int64  `json:"chain_funded_txo_sum"`
	ChainSpentTxoCount   int    `json:"chain_spent_txo_count"`
	ChainSpentTxoSum     int64  `json:"chain_spent_txo_sum"`
	ChainTxCount         int    `json:"chain_tx_count"`
	MempoolFundedTxoCount int   `json:"mempool_funded_txo_count"`
	MempoolTxCount       int    `json:"mempool_tx_count"`
}

// wireAddress is the raw JSON shape from the mempool.space API.
type wireAddress struct {
	Address    string `json:"address"`
	ChainStats struct {
		FundedTxoCount int   `json:"funded_txo_count"`
		FundedTxoSum   int64 `json:"funded_txo_sum"`
		SpentTxoCount  int   `json:"spent_txo_count"`
		SpentTxoSum    int64 `json:"spent_txo_sum"`
		TxCount        int   `json:"tx_count"`
	} `json:"chain_stats"`
	MempoolStats struct {
		FundedTxoCount int `json:"funded_txo_count"`
		TxCount        int `json:"tx_count"`
	} `json:"mempool_stats"`
}

// --- client methods ---

// Fees fetches the current recommended fee rates in sat/vByte.
func (c *Client) Fees(ctx context.Context) (*Fees, error) {
	body, err := c.get(ctx, BaseURL+"/api/v1/fees/recommended")
	if err != nil {
		return nil, err
	}
	var f Fees
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("parse fees: %w", err)
	}
	return &f, nil
}

// Blocks fetches the most recent blocks, truncated to limit entries.
// A limit of 0 returns all blocks the API sends (usually 10).
func (c *Client) Blocks(ctx context.Context, limit int) ([]Block, error) {
	body, err := c.get(ctx, BaseURL+"/api/blocks")
	if err != nil {
		return nil, err
	}
	var raw []Block
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse blocks: %w", err)
	}
	if limit > 0 && limit < len(raw) {
		raw = raw[:limit]
	}
	return raw, nil
}

// Lightning fetches the latest Lightning Network statistics.
func (c *Client) Lightning(ctx context.Context) (*LightningStats, error) {
	body, err := c.get(ctx, BaseURL+"/api/v1/lightning/statistics/latest")
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		Latest LightningStats `json:"latest"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("parse lightning: %w", err)
	}
	return &wrapper.Latest, nil
}

// Address fetches chain and mempool stats for a Bitcoin address.
func (c *Client) Address(ctx context.Context, addr string) (*Address, error) {
	body, err := c.get(ctx, BaseURL+"/api/address/"+addr)
	if err != nil {
		return nil, err
	}
	var w wireAddress
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, fmt.Errorf("parse address: %w", err)
	}
	return &Address{
		Address:              w.Address,
		ChainFundedTxoCount:  w.ChainStats.FundedTxoCount,
		ChainFundedTxoSum:    w.ChainStats.FundedTxoSum,
		ChainSpentTxoCount:   w.ChainStats.SpentTxoCount,
		ChainSpentTxoSum:     w.ChainStats.SpentTxoSum,
		ChainTxCount:         w.ChainStats.TxCount,
		MempoolFundedTxoCount: w.MempoolStats.FundedTxoCount,
		MempoolTxCount:       w.MempoolStats.TxCount,
	}, nil
}
