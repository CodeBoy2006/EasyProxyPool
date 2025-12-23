package sources

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/CodeBoy2006/EasyProxyPool/internal/config"
)

func TestLoader_Load_FileSources(t *testing.T) {
	dir := t.TempDir()

	rawPath := filepath.Join(dir, "list.txt")
	if err := os.WriteFile(rawPath, []byte("1.1.1.1:1080\n# c\n\nsocks5://2.2.2.2:1080\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	clashPath := filepath.Join(dir, "clash.yaml")
	if err := os.WriteFile(clashPath, []byte(`
proxies:
  - name: s1
    type: socks5
    server: 3.3.3.3
    port: 1080
  - name: x
    type: vmess
    server: example.com
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
`), 0o600); err != nil {
		t.Fatal(err)
	}

	l := New(nilLogger(t))
	res, err := l.Load(context.Background(), []config.SourceConfig{
		{Type: "raw_list", Path: rawPath},
		{Type: "clash_yaml", Path: clashPath},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.SOCKS5Addrs) != 3 {
		t.Fatalf("expected 3 socks5 addrs, got %d", len(res.SOCKS5Addrs))
	}
	want := map[string]bool{
		"1.1.1.1:1080": true,
		"2.2.2.2:1080": true, // socks5:// should be normalized
		"3.3.3.3:1080": true,
	}
	for _, a := range res.SOCKS5Addrs {
		if !want[a] {
			t.Fatalf("unexpected addr %q", a)
		}
		delete(want, a)
	}
	if len(want) != 0 {
		t.Fatalf("missing addrs: %+v", want)
	}
	if len(res.Specs) == 0 {
		t.Fatalf("expected specs from clash yaml")
	}
}

// nilLogger returns a no-op logger for tests.
func nilLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
