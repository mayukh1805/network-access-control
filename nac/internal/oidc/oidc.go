package oidc

import (
	"context"
	"fmt"
	"os"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Provider struct {
	OIDCProvider *oidc.Provider
	Verifier     *oidc.IDTokenVerifier
	OAuthConfig  *oauth2.Config
}

type Claims struct {
	Subject           string `json:"sub"`
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
}

func NewProvider(ctx context.Context) (*Provider, error) {
	issuer := os.Getenv("OIDC_ISSUER_URL")
	clientID := os.Getenv("OIDC_CLIENT_ID")
	clientSecret := os.Getenv("OIDC_CLIENT_SECRET")

	if issuer == "" {
		return nil, fmt.Errorf("OIDC_ISSUER_URL is not set")
	}
	if clientID == "" {
		return nil, fmt.Errorf("OIDC_CLIENT_ID is not set")
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("OIDC_CLIENT_SECRET is not set")
	}

	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC provider discovery failed: %w", err)
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: clientID,
	})

	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  "http://192.168.80.129:8081/callback",
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	return &Provider{
		OIDCProvider: provider,
		Verifier:     verifier,
		OAuthConfig:  config,
	}, nil
}
