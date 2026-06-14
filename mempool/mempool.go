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
// to the client's settings. The caller owns nothing extra; the body is read
// fully and closed here.
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

// Price holds the current BTC exchange rates.
type Price struct {
	Time  int64            `json:"time"`
	Rates map[string]int64 `json:"rates"`
}

// Fees holds the current recommended fee rates in sat/vByte.
type Fees struct {
	FastestFee  int `kit:"id" json:"fastest_fee"`
	HalfHourFee int `json:"half_hour_fee"`
	HourFee     int `json:"hour_fee"`
	EconomyFee  int `json:"economy_fee"`
	MinimumFee  int `json:"minimum_fee"`
}

// Block holds summary data for a single Bitcoin block.
type Block struct {
	ID        string  `kit:"id" json:"id"`
	Height    int     `json:"height"`
	Timestamp int64   `json:"timestamp"`
	Size      int     `json:"size"`
	Weight    int     `json:"weight"`
	TxCount   int     `json:"tx_count"`
	TotalFees int64   `json:"total_fees"`
	MedianFee float64 `json:"median_fee"`
	AvgFee    float64 `json:"avg_fee"`
}

// Address holds the chain stats for a Bitcoin address.
type Address struct {
	Address     string `kit:"id" json:"address"`
	TxCount     int    `json:"tx_count"`
	FundedSum   int64  `json:"funded_sum"`
	FundedCount int    `json:"funded_count"`
	SpentSum    int64  `json:"spent_sum"`
	Balance     int64  `json:"balance"`
}

// Transaction holds the data for a single Bitcoin transaction.
type Transaction struct {
	TXID        string `kit:"id" json:"txid"`
	Size        int    `json:"size"`
	Weight      int    `json:"weight"`
	Fee         int64  `json:"fee"`
	VinCount    int    `json:"vin_count"`
	VoutCount   int    `json:"vout_count"`
	Confirmed   bool   `json:"confirmed"`
	BlockHeight int    `json:"block_height"`
}

// GetPrice fetches the current BTC price in multiple currencies.
func (c *Client) GetPrice(ctx context.Context) (*Price, error) {
	body, err := c.get(ctx, BaseURL+"/api/v1/prices")
	if err != nil {
		return nil, err
	}
	var raw map[string]json.Number
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse price: %w", err)
	}
	p := &Price{Rates: make(map[string]int64)}
	for k, v := range raw {
		n, err := v.Int64()
		if err != nil {
			continue
		}
		if k == "time" {
			p.Time = n
		} else {
			p.Rates[k] = n
		}
	}
	return p, nil
}

// GetFees fetches the current recommended fee rates.
func (c *Client) GetFees(ctx context.Context) (*Fees, error) {
	body, err := c.get(ctx, BaseURL+"/api/v1/fees/recommended")
	if err != nil {
		return nil, err
	}
	var raw struct {
		FastestFee  int `json:"fastestFee"`
		HalfHourFee int `json:"halfHourFee"`
		HourFee     int `json:"hourFee"`
		EconomyFee  int `json:"economyFee"`
		MinimumFee  int `json:"minimumFee"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse fees: %w", err)
	}
	return &Fees{
		FastestFee:  raw.FastestFee,
		HalfHourFee: raw.HalfHourFee,
		HourFee:     raw.HourFee,
		EconomyFee:  raw.EconomyFee,
		MinimumFee:  raw.MinimumFee,
	}, nil
}

// GetBlocks fetches the most recent blocks. The API returns the last 10 blocks;
// limit <= 10 slices the result; limit 0 or > 10 returns all 10.
func (c *Client) GetBlocks(ctx context.Context, limit int) ([]*Block, error) {
	body, err := c.get(ctx, BaseURL+"/api/blocks")
	if err != nil {
		return nil, err
	}

	var raw []struct {
		ID        string `json:"id"`
		Height    int    `json:"height"`
		Timestamp int64  `json:"timestamp"`
		Size      int    `json:"size"`
		Weight    int    `json:"weight"`
		TxCount   int    `json:"tx_count"`
		Extras    struct {
			TotalFees int64   `json:"totalFees"`
			MedianFee float64 `json:"medianFee"`
			AvgFee    float64 `json:"avgFee"`
		} `json:"extras"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse blocks: %w", err)
	}

	var out []*Block
	for i, r := range raw {
		if limit > 0 && i >= limit {
			break
		}
		out = append(out, &Block{
			ID:        r.ID,
			Height:    r.Height,
			Timestamp: r.Timestamp,
			Size:      r.Size,
			Weight:    r.Weight,
			TxCount:   r.TxCount,
			TotalFees: r.Extras.TotalFees,
			MedianFee: r.Extras.MedianFee,
			AvgFee:    r.Extras.AvgFee,
		})
	}
	return out, nil
}

// GetBlock fetches a single block by its hash.
func (c *Client) GetBlock(ctx context.Context, hash string) (*Block, error) {
	body, err := c.get(ctx, BaseURL+"/api/block/"+hash)
	if err != nil {
		return nil, err
	}
	var raw struct {
		ID        string `json:"id"`
		Height    int    `json:"height"`
		Timestamp int64  `json:"timestamp"`
		Size      int    `json:"size"`
		Weight    int    `json:"weight"`
		TxCount   int    `json:"tx_count"`
		Extras    struct {
			TotalFees int64   `json:"totalFees"`
			MedianFee float64 `json:"medianFee"`
			AvgFee    float64 `json:"avgFee"`
		} `json:"extras"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse block: %w", err)
	}
	return &Block{
		ID:        raw.ID,
		Height:    raw.Height,
		Timestamp: raw.Timestamp,
		Size:      raw.Size,
		Weight:    raw.Weight,
		TxCount:   raw.TxCount,
		TotalFees: raw.Extras.TotalFees,
		MedianFee: raw.Extras.MedianFee,
		AvgFee:    raw.Extras.AvgFee,
	}, nil
}

// GetAddress fetches the chain stats for a Bitcoin address.
func (c *Client) GetAddress(ctx context.Context, address string) (*Address, error) {
	body, err := c.get(ctx, BaseURL+"/api/address/"+address)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Address    string `json:"address"`
		ChainStats struct {
			FundedTxoCount int   `json:"funded_txo_count"`
			FundedTxoSum   int64 `json:"funded_txo_sum"`
			SpentTxoSum    int64 `json:"spent_txo_sum"`
			TxCount        int   `json:"tx_count"`
		} `json:"chain_stats"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse address: %w", err)
	}
	return &Address{
		Address:     raw.Address,
		TxCount:     raw.ChainStats.TxCount,
		FundedSum:   raw.ChainStats.FundedTxoSum,
		FundedCount: raw.ChainStats.FundedTxoCount,
		SpentSum:    raw.ChainStats.SpentTxoSum,
		Balance:     raw.ChainStats.FundedTxoSum - raw.ChainStats.SpentTxoSum,
	}, nil
}

// GetTransaction fetches a single transaction by its TXID.
func (c *Client) GetTransaction(ctx context.Context, txid string) (*Transaction, error) {
	body, err := c.get(ctx, BaseURL+"/api/tx/"+txid)
	if err != nil {
		return nil, err
	}
	var raw struct {
		TXID   string            `json:"txid"`
		Size   int               `json:"size"`
		Weight int               `json:"weight"`
		Fee    int64             `json:"fee"`
		Vin    []json.RawMessage `json:"vin"`
		Vout   []json.RawMessage `json:"vout"`
		Status struct {
			Confirmed   bool `json:"confirmed"`
			BlockHeight int  `json:"block_height"`
		} `json:"status"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse tx: %w", err)
	}
	return &Transaction{
		TXID:        raw.TXID,
		Size:        raw.Size,
		Weight:      raw.Weight,
		Fee:         raw.Fee,
		VinCount:    len(raw.Vin),
		VoutCount:   len(raw.Vout),
		Confirmed:   raw.Status.Confirmed,
		BlockHeight: raw.Status.BlockHeight,
	}, nil
}
