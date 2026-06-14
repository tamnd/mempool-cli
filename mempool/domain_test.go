package mempool

import (
	"testing"
)

// These tests are offline: they exercise the URI driver's pure string functions
// without touching the network.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "mempool" {
		t.Errorf("Scheme = %q, want mempool", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "mempool" {
		t.Errorf("Identity.Binary = %q, want mempool", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		in  string
		typ string
		id  string
	}{
		// Bitcoin address starting with 1
		{"1A1zP1eP5QGefi2DMPTfTL5SLmv7Divf", "address", "1A1zP1eP5QGefi2DMPTfTL5SLmv7Divf"},
		// Bitcoin address starting with 3
		{"3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy", "address", "3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy"},
		// SegWit bech32 address
		{"bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq", "address", "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq"},
		// 64-char hex TXID
		{"4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b", "tx", "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b"},
		// block hash (64-char hex but not a txid; classify treats both same way)
		{"000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f", "tx", "000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f"},
		// numeric height
		{"953651", "height", "953651"},
		// URL with address path
		{"https://mempool.space/address/1A1zP1eP5QGefi2DMPTfTL5SLmv7Divf", "address", "1A1zP1eP5QGefi2DMPTfTL5SLmv7Divf"},
		// URL with tx path
		{"https://mempool.space/tx/4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b", "tx", "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b"},
		// URL with block path
		{"https://mempool.space/block/000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f", "block", "000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil {
			t.Errorf("Classify(%q) error: %v", tc.in, err)
			continue
		}
		if typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q), want (%q, %q)", tc.in, typ, id, tc.typ, tc.id)
		}
	}
}

func TestLocate(t *testing.T) {
	cases := []struct {
		uriType string
		id      string
		want    string
	}{
		{"address", "1A1zP1eP5QGefi2DMPTfTL5SLmv7Divf", "https://mempool.space/address/1A1zP1eP5QGefi2DMPTfTL5SLmv7Divf"},
		{"tx", "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b", "https://mempool.space/tx/4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b"},
		{"block", "000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f", "https://mempool.space/block/000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f"},
	}
	for _, tc := range cases {
		got, err := Domain{}.Locate(tc.uriType, tc.id)
		if err != nil {
			t.Errorf("Locate(%q, %q) error: %v", tc.uriType, tc.id, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Locate(%q, %q) = %q, want %q", tc.uriType, tc.id, got, tc.want)
		}
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("unknown", "foo")
	if err == nil {
		t.Error("expected error for unknown type, got nil")
	}
}

func TestIsBitcoinAddress(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"1A1zP1eP5QGefi2DMPTfTL5SLmv7Divf", true},
		{"3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy", true},
		{"bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq", true},
		{"4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b", false},
		{"953651", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isBitcoinAddress(tc.s); got != tc.want {
			t.Errorf("isBitcoinAddress(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestIs64Hex(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b", true},
		{"000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f", true},
		{"abc", false},
		{"1A1zP1eP5QGefi2DMPTfTL5SLmv7Divf", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := is64Hex(tc.s); got != tc.want {
			t.Errorf("is64Hex(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestIsNumeric(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"953651", true},
		{"0", true},
		{"abc", false},
		{"12a3", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isNumeric(tc.s); got != tc.want {
			t.Errorf("isNumeric(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}
