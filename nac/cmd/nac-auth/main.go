package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"
	"nac/internal/database"
	"nac/internal/oidc"
	"nac/internal/policy"
	"nac/internal/session"
)

func main() {
	ctx := context.Background()
	dbURL := os.Getenv("NAC_DATABASE_URL")
	if dbURL == "" {
		panic("NAC_DATABASE_URL is not set")
	}

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		panic(err)
	}
	defer conn.Close(ctx)
	provider, err := oidc.NewProvider(ctx)
	if err != nil {
		panic(err)
	}

	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		state, err := randomString(32)
		if err != nil {
			http.Error(w, "failed to generate state", http.StatusInternalServerError)
			return
		}

		verifier, err := randomString(32)
		if err != nil {
			http.Error(w, "failed to generate PKCE verifier", http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "oidc_state",
			Value:    state + "|" + verifier,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})

		url := provider.OAuthConfig.AuthCodeURL(
			state,
			oauth2.S256ChallengeOption(verifier),
		)

		http.Redirect(w, r, url, http.StatusFound)
	})

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("oidc_state")
		if err != nil {
			http.Error(w, "OIDC state cookie missing", http.StatusBadRequest)
			return
		}

		parts := strings.SplitN(cookie.Value, "|", 2)
		if len(parts) != 2 {
			http.Error(w, "invalid OIDC state cookie", http.StatusBadRequest)
			return
		}

		expectedState := parts[0]
		verifier := parts[1]

		if r.URL.Query().Get("state") != expectedState {
			http.Error(w, "OIDC state validation failed", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "authorization code missing", http.StatusBadRequest)
			return
		}

		token, err := provider.OAuthConfig.Exchange(
			r.Context(),
			code,
			oauth2.SetAuthURLParam("code_verifier", verifier),
		)
		if err != nil {
			http.Error(w, "token exchange failed: "+err.Error(), http.StatusUnauthorized)
			return
		}

		rawIDToken, ok := token.Extra("id_token").(string)
		if !ok {
			http.Error(w, "ID token missing", http.StatusUnauthorized)
			return
		}

		idToken, err := provider.Verifier.Verify(r.Context(), rawIDToken)
		if err != nil {
			http.Error(w, "ID token verification failed: "+err.Error(), http.StatusUnauthorized)
			return
		}

		var claims oidc.Claims

		if err := idToken.Claims(&claims); err != nil {
			http.Error(w, "failed to read ID token claims", http.StatusInternalServerError)
			return
		}
		user, err := database.GetUser(r.Context(), conn, claims.PreferredUsername)
		if err != nil {
			http.Error(w, "database user lookup failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		sessionToken, err := randomString(32)
		if err != nil {
			http.Error(w, "failed to generate session token", http.StatusInternalServerError)
			return
		}

		activeSession, err := session.CreateSession(
			r.Context(),
			conn,
			user.ID,
			1,
			netip.MustParseAddr("10.77.10.100"),
			sessionToken,
			time.Now().Add(1*time.Hour),
		)
		if err != nil {
			http.Error(w, "session creation failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		decision := policy.Evaluate(user.Role, "printer", "print")
		fmt.Printf(
			"OIDC user=%s PostgreSQL role=%s Policy decision=%s Session ID=%d IP=%s Expires=%s\n",
			claims.PreferredUsername,
			user.Role,
			decision,
			activeSession.ID,
			activeSession.IP,
			activeSession.ExpiresAt.Format("2006-01-02 15:04:05"),
		)

		http.SetCookie(w, &http.Cookie{
			Name:     "oidc_state",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})

		fmt.Fprintf(
			w,
			"<html><body><h1>OIDC Authentication Successful</h1>"+
				"<p><strong>User:</strong> %s</p>"+
				"<p><strong>Email:</strong> %s</p>"+
				"<p><strong>Subject:</strong> %s</p>"+
				"<p><strong>Role:</strong> %s</p>"+
				"</body></html>",
			claims.PreferredUsername,
			claims.Email,
			claims.Subject,
			user.Role,
		)
	})

	fmt.Println("NAC OIDC server listening on :8081")

	if err := http.ListenAndServe(":8081", nil); err != nil {
		panic(err)
	}
}

func randomString(length int) (string, error) {
	b := make([]byte, length)

	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}
