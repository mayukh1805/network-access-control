package oidc

import (
	"context"
	"os"
	"testing"
)

func TestNewProvider(t *testing.T) {
	if os.Getenv("OIDC_ISSUER_URL") == "" {
		t.Skip("OIDC_ISSUER_URL is not set")
	}

	if os.Getenv("OIDC_CLIENT_ID") == "" {
		t.Skip("OIDC_CLIENT_ID is not set")
	}

	provider, err := NewProvider(context.Background())
	if err != nil {
		t.Fatalf("OIDC provider initialization failed: %v", err)
	}

	if provider.Verifier == nil {
		t.Fatal("OIDC verifier is nil")
	}
}
