package mempool

import (
	"context"
	"strings"
	"unicode"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes mempool.space as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/mempool-cli/mempool"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// mempool:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone mempool binary (see cli.NewApp), so the
// binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the mempool driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "mempool",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "mempool",
			Short:  "A command line for Mempool.space Bitcoin data.",
			Long: `A command line for Mempool.space Bitcoin data.

mempool reads public Bitcoin blockchain data from mempool.space over plain
HTTPS, shapes it into clean records, and prints output that pipes into the rest
of your tools. No API key, nothing to run alongside it.`,
			Site: Host,
			Repo: "https://github.com/tamnd/mempool-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{Name: "price", Group: "read", Single: true,
		Summary: "Fetch the current BTC price in multiple currencies"}, getPrice)

	kit.Handle(app, kit.OpMeta{Name: "fees", Group: "read", Single: true,
		Summary: "Fetch recommended transaction fee rates (sat/vByte)"}, getFees)

	kit.Handle(app, kit.OpMeta{Name: "blocks", Group: "read", List: true,
		Summary: "List the most recent blocks"}, getBlocks)

	kit.Handle(app, kit.OpMeta{Name: "address", Group: "read", Single: true,
		Summary: "Fetch chain stats for a Bitcoin address", URIType: "address", Resolver: true,
		Args: []kit.Arg{{Name: "address", Help: "Bitcoin address"}}}, getAddress)

	kit.Handle(app, kit.OpMeta{Name: "tx", Group: "read", Single: true,
		Summary: "Fetch a transaction by TXID", URIType: "tx", Resolver: true,
		Args: []kit.Arg{{Name: "txid", Help: "transaction ID"}}}, getTx)

	kit.Handle(app, kit.OpMeta{Name: "block", Group: "read", Single: true,
		Summary: "Fetch a block by hash", URIType: "block", Resolver: true,
		Args: []kit.Arg{{Name: "hash", Help: "block hash"}}}, getBlock)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := DefaultConfig()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.Timeout = cfg.Timeout
	}
	return NewClient(c), nil
}

// --- inputs ---

type priceInput struct {
	Client *Client `kit:"inject"`
}

type feesInput struct {
	Client *Client `kit:"inject"`
}

type blocksInput struct {
	Limit  int     `kit:"flag,inherit" help:"number of recent blocks" default:"10"`
	Client *Client `kit:"inject"`
}

type addressInput struct {
	Address string  `kit:"arg" help:"Bitcoin address"`
	Client  *Client `kit:"inject"`
}

type txInput struct {
	TXID   string  `kit:"arg" help:"transaction ID"`
	Client *Client `kit:"inject"`
}

type blockInput struct {
	Hash   string  `kit:"arg" help:"block hash"`
	Client *Client `kit:"inject"`
}

// --- handlers ---

func getPrice(ctx context.Context, in priceInput, emit func(*Price) error) error {
	p, err := in.Client.GetPrice(ctx)
	if err != nil {
		return mapErr(err)
	}
	return emit(p)
}

func getFees(ctx context.Context, in feesInput, emit func(*Fees) error) error {
	f, err := in.Client.GetFees(ctx)
	if err != nil {
		return mapErr(err)
	}
	return emit(f)
}

func getBlocks(ctx context.Context, in blocksInput, emit func(*Block) error) error {
	blocks, err := in.Client.GetBlocks(ctx, in.Limit)
	if err != nil {
		return mapErr(err)
	}
	for _, b := range blocks {
		if err := emit(b); err != nil {
			return err
		}
	}
	return nil
}

func getAddress(ctx context.Context, in addressInput, emit func(*Address) error) error {
	a, err := in.Client.GetAddress(ctx, in.Address)
	if err != nil {
		return mapErr(err)
	}
	return emit(a)
}

func getTx(ctx context.Context, in txInput, emit func(*Transaction) error) error {
	tx, err := in.Client.GetTransaction(ctx, in.TXID)
	if err != nil {
		return mapErr(err)
	}
	return emit(tx)
}

func getBlock(ctx context.Context, in blockInput, emit func(*Block) error) error {
	b, err := in.Client.GetBlock(ctx, in.Hash)
	if err != nil {
		return mapErr(err)
	}
	return emit(b)
}

// --- Resolver: URI string functions, pure and network-free ---

// Classify turns any accepted input into the canonical (type, id).
// Bitcoin address (starts with 1, 3, bc1) -> "address"
// 64-char hex -> "tx"
// numeric -> "height" (not directly addressable, but classified)
// otherwise -> "block"
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errs.Usage("empty input")
	}
	// strip scheme if any
	if after, ok := strings.CutPrefix(input, "https://"+Host); ok {
		input = strings.Trim(after, "/")
	}
	if after, ok := strings.CutPrefix(input, "http://"+Host); ok {
		input = strings.Trim(after, "/")
	}

	// URL paths like /address/1..., /tx/..., /block/...
	if rest, ok := strings.CutPrefix(input, "address/"); ok {
		return "address", rest, nil
	}
	if rest, ok := strings.CutPrefix(input, "tx/"); ok {
		return "tx", rest, nil
	}
	if rest, ok := strings.CutPrefix(input, "block/"); ok {
		return "block", rest, nil
	}

	// bare values
	if isBitcoinAddress(input) {
		return "address", input, nil
	}
	if is64Hex(input) {
		return "tx", input, nil
	}
	if isNumeric(input) {
		return "height", input, nil
	}
	// assume block hash
	return "block", input, nil
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "address":
		return BaseURL + "/address/" + id, nil
	case "tx":
		return BaseURL + "/tx/" + id, nil
	case "block", "height":
		return BaseURL + "/block/" + id, nil
	}
	return "", errs.Usage("mempool has no resource type %q", uriType)
}

// --- helpers ---

func isBitcoinAddress(s string) bool {
	if strings.HasPrefix(s, "bc1") || strings.HasPrefix(s, "1") || strings.HasPrefix(s, "3") {
		return len(s) >= 26 && len(s) <= 62
	}
	return false
}

func is64Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !unicode.IsDigit(c) {
			return false
		}
	}
	return true
}

// mapErr converts a library error into the kit error kind that carries the right
// exit code.
func mapErr(err error) error {
	return err
}
