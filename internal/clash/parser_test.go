package clash

import "testing"

func TestParseYAML_ParsesAndSkipsUnsupported(t *testing.T) {
	yml := []byte(`
proxies:
  - name: ss1
    type: ss
    server: 1.2.3.4
    port: 8388
    cipher: aes-128-gcm
    password: secret
  - name: vless1
    type: vless
    server: example.com
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
    tls: true
    skip-cert-verify: true
    servername: sni.example.com
    network: ws
    ws-opts:
      path: /ws
      headers:
        Host: h.example.com
  - name: trojan1
    type: trojan
    server: t.example.com
    port: 443
    password: p
    sni: sni.t.example.com
  - name: socks1
    type: socks5
    server: 127.0.0.2
    port: 1080
    username: u
    password: p
  - name: unsupported
    type: hysteria
    server: 1.1.1.1
    port: 443
`)

	report, err := ParseYAML(yml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Specs) != 4 {
		t.Fatalf("expected 4 specs, got %d", len(report.Specs))
	}
	if report.SkippedByType["hysteria"] != 1 {
		t.Fatalf("expected hysteria skipped=1, got %d", report.SkippedByType["hysteria"])
	}
	for _, s := range report.Specs {
		if s.ID == "" {
			t.Fatalf("expected normalized id")
		}
	}
}

func TestParseYAML_AggregatesProblems(t *testing.T) {
	yml := []byte(`
proxies:
  - name: bad1
    type: ss
    server: 1.2.3.4
    port: 8388
    cipher: aes-128-gcm
`)
	report, err := ParseYAML(yml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Specs) != 0 {
		t.Fatalf("expected 0 specs, got %d", len(report.Specs))
	}
	if len(report.Problems) == 0 {
		t.Fatalf("expected problems to be reported")
	}
}
