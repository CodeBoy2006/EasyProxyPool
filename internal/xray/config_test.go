package xray

import (
	"encoding/json"
	"testing"

	"github.com/CodeBoy2006/EasyProxyPool/internal/config"
	"github.com/CodeBoy2006/EasyProxyPool/internal/upstream"
)

func TestGenerate_Minimal(t *testing.T) {
	specs := []upstream.Spec{
		upstream.Spec{
			Type:   upstream.TypeVLESS,
			Server: "example.com",
			Port:   443,
			VLESS: &upstream.VLESSConfig{
				UUID:           "11111111-1111-1111-1111-111111111111",
				TLS:            true,
				SkipCertVerify: true,
				ServerName:     "sni.example.com",
				Network:        "ws",
				WSPath:         "/ws",
			},
		}.Normalize(),
		upstream.Spec{
			Type:   upstream.TypeSOCKS5,
			Server: "1.2.3.4",
			Port:   1080,
		}.Normalize(),
	}

	gen, err := Generate(specs, GenerateOptions{
		Mode:          ModeRelaxed,
		SOCKSListen:   "127.0.0.1:17383",
		MetricsListen: "127.0.0.1:17387",
		UserPassword:  "pw",
		MaxNodes:      10,
		Observatory: config.ObservatoryConfig{
			Mode:            "burst",
			Destination:     "https://www.gstatic.com/generate_204",
			IntervalSeconds: 30,
			Sampling:        3,
			TimeoutSeconds:  5,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gen.Hash == "" || len(gen.ConfigJSON) == 0 {
		t.Fatalf("expected config output")
	}
	if len(gen.Included) != 2 {
		t.Fatalf("expected 2 included nodes, got %d", len(gen.Included))
	}

	var root map[string]any
	if err := json.Unmarshal(gen.ConfigJSON, &root); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if root["metrics"] == nil || root["burstObservatory"] == nil {
		t.Fatalf("expected metrics + burstObservatory")
	}
	inbounds, _ := root["inbounds"].([]any)
	if len(inbounds) != 1 {
		t.Fatalf("expected 1 inbound, got %d", len(inbounds))
	}
}

func TestGenerate_MaxNodes(t *testing.T) {
	specs := []upstream.Spec{
		upstream.Spec{Type: upstream.TypeSOCKS5, Server: "1.1.1.1", Port: 1080}.Normalize(),
		upstream.Spec{Type: upstream.TypeSOCKS5, Server: "2.2.2.2", Port: 1080}.Normalize(),
	}
	_, err := Generate(specs, GenerateOptions{
		Mode:          ModeStrict,
		SOCKSListen:   "127.0.0.1:17383",
		MetricsListen: "127.0.0.1:17387",
		UserPassword:  "pw",
		MaxNodes:      1,
		Observatory: config.ObservatoryConfig{
			Mode:        "observatory",
			Destination: "https://www.gstatic.com/generate_204",
		},
	})
	if err == nil {
		t.Fatalf("expected max_nodes error")
	}
}
