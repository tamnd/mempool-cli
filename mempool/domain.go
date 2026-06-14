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
// The same Domain also builds the standalone mempool binary (see cli.NewApp),
// so the binary and a host share one source of truth.
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

	kit.Handle(app, kit.OpMeta{Name: "fees", Group: "read", Single: true,
		Summary: "Fetch recommended transaction fee rates (sat/vByte)"}, getFees)

	kit.Handle(app, kit.OpMeta{Name: "blocks", Group: "read", List: true,
		Summary: "List the most recent blocks"}, getBlocks)

	kit.Handle(app, kit.OpMeta{Name: "lightning", Group: "read", Single: true,
		Summary: "Fetch the latest Lightning Network statistics"}, getLightning)

	kit.Handle(app, kit.OpMeta{Name: "address", Group: "read", Single: true,
		Summary: "Fetch chain stats for a Bitcoin address", URIType: "address", Resolver: true,
		Args: []kit.Arg{{Name: "address", Help: "Bitcoin address"}}}, getAddress)
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

type feesInput struct {
	Client *Client `kit:"inject"`
}

type blocksInput struct {
	Limit  int     `kit:"flag,inherit" help:"max blocks" default:"10"`
	Client *Client `kit:"inject"`
}

type lightningInput struct {
	Client *Client `kit:"inject"`
}

type addressInput struct {
	Address string  `kit:"arg" help:"Bitcoin address"`
	Client  *Client `kit:"inject"`
}

// --- handlers ---

func getFees(ctx context.Context, in feesInput, emit func(*Fees) error) error {
	f, err := in.Client.Fees(ctx)
	if err != nil {
		return mapErr(err)
	}
	return emit(f)
}

func getBlocks(ctx context.Context, in blocksInput, emit func(*Block) error) error {
	blocks, err := in.Client.Blocks(ctx, in.Limit)
	if err != nil {
		return mapErr(err)
	}
	for i := range blocks {
		if err := emit(&blocks[i]); err != nil {
			return err
		}
	}
	return nil
}

func getLightning(ctx context.Context, in lightningInput, emit func(*LightningStats) error) error {
	s, err := in.Client.Lightning(ctx)
	if err != nil {
		return mapErr(err)
	}
	return emit(s)
}

func getAddress(ctx context.Context, in addressInput, emit func(*Address) error) error {
	a, err := in.Client.Address(ctx, in.Address)
	if err != nil {
		return mapErr(err)
	}
	return emit(a)
}

// --- Resolver: URI string functions, pure and network-free ---

// Classify turns any accepted input into the canonical (type, id).
// Bitcoin address (starts with 1, 3, bc1) -> "address"
// 64-char hex -> "block"
// otherwise -> "query"
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

	// URL paths like /address/1..., /block/...
	if rest, ok := strings.CutPrefix(input, "address/"); ok {
		return "address", rest, nil
	}
	if rest, ok := strings.CutPrefix(input, "block/"); ok {
		return "block", rest, nil
	}

	// bare values
	if isBitcoinAddress(input) {
		return "address", input, nil
	}
	if is64Hex(input) {
		return "block", input, nil
	}
	return "query", input, nil
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "address":
		return BaseURL + "/address/" + id, nil
	case "block":
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
