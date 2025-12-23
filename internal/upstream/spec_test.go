package upstream

import "testing"

func TestStableNodeID_Deterministic(t *testing.T) {
	s := Spec{
		Name:   "n1",
		Type:   TypeSOCKS5,
		Server: "Example.COM",
		Port:   1080,
		SOCKS5: &SOCKS5Config{Username: "u", Password: "p"},
	}.Normalize()

	s2 := Spec{
		Name:   "ignored",
		Type:   TypeSOCKS5,
		Server: "example.com",
		Port:   1080,
		SOCKS5: &SOCKS5Config{Username: "u", Password: "p"},
	}.Normalize()

	if s.ID != s2.ID {
		t.Fatalf("expected stable id, got %q != %q", s.ID, s2.ID)
	}
}

func TestStableNodeID_ChangesWithSecret(t *testing.T) {
	a := Spec{
		Type:   TypeShadowsocks,
		Server: "1.2.3.4",
		Port:   8388,
		Shadowsocks: &ShadowsocksConfig{
			Method:   "aes-128-gcm",
			Password: "p1",
		},
	}.Normalize()
	b := Spec{
		Type:   TypeShadowsocks,
		Server: "1.2.3.4",
		Port:   8388,
		Shadowsocks: &ShadowsocksConfig{
			Method:   "aes-128-gcm",
			Password: "p2",
		},
	}.Normalize()

	if a.ID == b.ID {
		t.Fatalf("expected different ids for different secrets, got %q", a.ID)
	}
}

func TestSafeSummary_NoSecrets(t *testing.T) {
	s := Spec{
		Type:   TypeVMess,
		Server: "x",
		Port:   443,
		VMess: &VMessConfig{
			UUID:           "uuid",
			AlterID:        0,
			Security:       "auto",
			TLS:            true,
			SkipCertVerify: true,
			ServerName:     "sni",
			Network:        "ws",
			WSPath:         "/ws",
			Headers: map[string]string{
				"Host": "example.com",
			},
		},
	}.Normalize()

	m := s.SafeSummary()
	forbidden := []string{"uuid", "pass", "password", "p1", "p2"}
	for _, f := range forbidden {
		for k, v := range m {
			if k == f {
				t.Fatalf("unexpected secret key %q", k)
			}
			if vs, ok := v.(string); ok && vs == f {
				t.Fatalf("unexpected secret value %q", f)
			}
		}
	}
}

