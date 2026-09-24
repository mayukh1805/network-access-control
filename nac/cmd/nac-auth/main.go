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
	"html"
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

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		cookie, err := r.Cookie("nac_session")
		if err != nil {
			http.Error(w, "Not authenticated. Please login with Keycloak.", http.StatusUnauthorized)
			return
		}

		nacSession, err := session.GetSessionByToken(r.Context(), conn, cookie.Value)
		if err != nil {
			http.Error(w, "Session expired or invalid. Please login again.", http.StatusUnauthorized)
			return
		}

		user, err := database.GetUserByID(r.Context(), conn, nacSession.UserID)
		if err != nil {
			http.Error(w, "User lookup failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		device, err := database.GetDevice(r.Context(), conn, nacSession.DeviceID)
		if err != nil {
			http.Error(w, "Device lookup failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		_, bindingErr := database.GetActiveBinding(
			r.Context(),
			conn,
			device.ID,
			nacSession.IP,
		)

		postureStatus := "UNKNOWN"
		if device.Status == "active" {
			postureStatus = "HEALTHY"
		} else {
			postureStatus = "UNHEALTHY"
		}

		policyDecision := policy.Evaluate(
			user.Role,
			"printer",
			"print",
		)

		bindingStatus := "INVALID"
		if bindingErr == nil {
			bindingStatus = "ACTIVE"
		}

		safeUsername := html.EscapeString(user.Username)
		safeRole := html.EscapeString(user.Role)
		safeHostname := html.EscapeString(device.Hostname)
		safeMAC := html.EscapeString(device.MAC)
		safeIP := html.EscapeString(nacSession.IP.String())
		safeDeviceStatus := html.EscapeString(device.Status)
		safeBindingStatus := html.EscapeString(bindingStatus)
		safeSessionStatus := html.EscapeString(nacSession.Status)
		safePolicyDecision := html.EscapeString(string(policyDecision))
		safePostureStatus := html.EscapeString(postureStatus)

		fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head>
	<title>NAC Dashboard</title>
	<meta charset="UTF-8">
	<style>
		body {
			font-family: Arial, sans-serif;
			max-width: 900px;
			margin: 40px auto;
			padding: 20px;
		}
		table {
			width: 100%%;
			border-collapse: collapse;
			margin-bottom: 25px;
		}
		th, td {
			border: 1px solid #ccc;
			padding: 10px;
			text-align: left;
		}
		th {
			background: #f2f2f2;
		}
		.status {
			font-weight: bold;
		}
	</style>
</head>

<body>

<h1>Network Access Control Dashboard</h1>

<p>🔒 <strong>HTTPS connection established</strong></p>

<h2>Identity</h2>

<table>
<tr><th>User</th><td>%s</td></tr>
<tr><th>Role</th><td>%s</td></tr>
</table>

<h2>Device</h2>

<table>
<tr><th>Hostname</th><td>%s</td></tr>
<tr><th>MAC Address</th><td>%s</td></tr>
<tr><th>IP Address</th><td>%s</td></tr>
<tr><th>Device Status</th><td class="status">%s</td></tr>
<tr><th>Binding</th><td class="status">%s</td></tr>
</table>

<h2>Session</h2>

<table>
<tr><th>Session ID</th><td>%d</td></tr>
<tr><th>Status</th><td class="status">%s</td></tr>
<tr><th>Expires</th><td>%s</td></tr>
</table>

<h2>Authorization</h2>

<table>
<tr><th>Resource</th><td>Printer</td></tr>
<tr><th>Action</th><td>Print</td></tr>
<tr><th>Policy Decision</th><td class="status">%s</td></tr>
<tr><th>Device Posture</th><td class="status">%s</td></tr>
</table>

<h2>System</h2>

<table>
<tr><th>NAC Controller</th><td>ONLINE</td></tr>
<tr><th>PostgreSQL</th><td>CONNECTED</td></tr>
<tr><th>Keycloak OIDC</th><td>CONNECTED</td></tr>
<tr><th>Transport</th><td>HTTPS / TLS</td></tr>
<tr><th>Enforcement</th><td>nftables</td></tr>
</table>

</body>
</html>
`,
			safeUsername,
			safeRole,
			safeHostname,
			safeMAC,
			safeIP,
			safeDeviceStatus,
			safeBindingStatus,
			nacSession.ID,
			safeSessionStatus,
			nacSession.ExpiresAt.Format("2006-01-02 15:04:05"),
			safePolicyDecision,
			safePostureStatus,
		)
	})

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
			Secure:   true,
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

		http.SetCookie(w, &http.Cookie{
			Name:     "nac_session",
			Value:    sessionToken,
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})

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
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})

		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	})

	fmt.Println("NAC HTTPS dashboard listening on :8443")

	if err := http.ListenAndServeTLS(
		":8443",
		"certs/nac-dashboard.crt",
		"certs/nac-dashboard.key",
		nil,
	); err != nil {
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
