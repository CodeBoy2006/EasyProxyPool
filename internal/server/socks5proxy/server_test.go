package socks5proxy

import (
	"testing"

	"github.com/CodeBoy2006/EasyProxyPool/internal/config"
)

func TestCredentialStoreFromAuth(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		if got := credentialStoreFromAuth(config.AuthConfig{Mode: "disabled"}); got != nil {
			t.Fatalf("expected nil")
		}
	})

	t.Run("basic", func(t *testing.T) {
		got := credentialStoreFromAuth(config.AuthConfig{Mode: "basic", Username: "u", Password: "p"})
		if got == nil {
			t.Fatalf("expected non-nil")
		}
		if !got.Valid("u", "p") {
			t.Fatalf("expected valid")
		}
		if got.Valid("x", "p") {
			t.Fatalf("expected invalid username")
		}
	})

	t.Run("shared_password", func(t *testing.T) {
		got := credentialStoreFromAuth(config.AuthConfig{Mode: "shared_password", Password: "p"})
		if got == nil {
			t.Fatalf("expected non-nil")
		}
		if !got.Valid("any", "p") {
			t.Fatalf("expected valid")
		}
		if got.Valid("any", "bad") {
			t.Fatalf("expected invalid password")
		}
	})
}
