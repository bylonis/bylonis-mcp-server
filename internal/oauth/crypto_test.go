package oauth

import (
	"errors"
	"testing"
	"time"
)

func TestEncryptDecryptTokenRoundTrip(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)

	token, err := EncryptToken("api-key", "https://tenant.example.com", "client-1", expiresAt, secret)
	if err != nil {
		t.Fatalf("EncryptToken() error = %v", err)
	}

	apiKey, bylonisURL, clientID, gotExpiresAt, err := DecryptToken(token, secret)
	if err != nil {
		t.Fatalf("DecryptToken() error = %v", err)
	}

	if apiKey != "api-key" || bylonisURL != "https://tenant.example.com" || clientID != "client-1" {
		t.Fatalf("unexpected payload: apiKey=%q bylonisURL=%q clientID=%q", apiKey, bylonisURL, clientID)
	}
	if !gotExpiresAt.Equal(expiresAt) {
		t.Fatalf("DecryptToken() expiresAt = %v, want %v", gotExpiresAt, expiresAt)
	}
}

func TestDecryptTokenExpired(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")

	token, err := EncryptToken("api-key", "https://tenant.example.com", "client-1", time.Now().UTC().Add(-time.Minute), secret)
	if err != nil {
		t.Fatalf("EncryptToken() error = %v", err)
	}

	_, _, _, _, err = DecryptToken(token, secret)
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("DecryptToken() error = %v, want %v", err, ErrExpiredToken)
	}
}

func TestDecryptTokenRejectsWrongKey(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	wrongSecret := []byte("fedcba9876543210fedcba9876543210")

	token, err := EncryptToken("api-key", "https://tenant.example.com", "client-1", time.Now().UTC().Add(time.Hour), secret)
	if err != nil {
		t.Fatalf("EncryptToken() error = %v", err)
	}

	_, _, _, _, err = DecryptToken(token, wrongSecret)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("DecryptToken() error = %v, want %v", err, ErrInvalidToken)
	}
}

func TestDecryptTokenRejectsMalformedInput(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")

	_, _, _, _, err := DecryptToken("not-a-valid-token", secret)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("DecryptToken() error = %v, want %v", err, ErrInvalidToken)
	}
}

func TestEncryptDecryptClientIDRoundTrip(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	createdAt := time.Now().UTC().Truncate(time.Second)

	clientID, err := EncryptClientID([]string{"http://127.0.0.1:4567/callback"}, "Claude", createdAt, secret)
	if err != nil {
		t.Fatalf("EncryptClientID() error = %v", err)
	}

	redirectURIs, clientName, gotCreatedAt, err := DecryptClientID(clientID, secret)
	if err != nil {
		t.Fatalf("DecryptClientID() error = %v", err)
	}
	if clientName != "Claude" {
		t.Fatalf("client name = %q, want %q", clientName, "Claude")
	}
	if len(redirectURIs) != 1 || redirectURIs[0] != "http://127.0.0.1:4567/callback" {
		t.Fatalf("redirect URIs = %v", redirectURIs)
	}
	if !gotCreatedAt.Equal(createdAt) {
		t.Fatalf("createdAt = %v, want %v", gotCreatedAt, createdAt)
	}
}

func TestDecryptAuthorizationCodeExpired(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")

	code, err := EncryptAuthorizationCode(
		"api-key",
		"https://tenant.example.com",
		"client-1",
		"http://127.0.0.1:4567/callback",
		"challenge",
		"S256",
		time.Now().UTC().Add(-time.Minute),
		secret,
	)
	if err != nil {
		t.Fatalf("EncryptAuthorizationCode() error = %v", err)
	}

	_, _, _, _, _, _, _, err = DecryptAuthorizationCode(code, secret)
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("DecryptAuthorizationCode() error = %v, want %v", err, ErrExpiredToken)
	}
}

func TestDecryptTokenRejectsRefreshTokenBlob(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")

	refreshToken, err := EncryptRefreshToken("api-key", "https://tenant.example.com", "client-1", time.Now().UTC().Add(time.Hour), secret)
	if err != nil {
		t.Fatalf("EncryptRefreshToken() error = %v", err)
	}

	_, _, _, _, err = DecryptToken(refreshToken, secret)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("DecryptToken() error = %v, want %v", err, ErrInvalidToken)
	}
}

func TestDecryptTokenRejectsAuthorizationCodeBlob(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")

	code, err := EncryptAuthorizationCode(
		"api-key",
		"https://tenant.example.com",
		"client-1",
		"http://127.0.0.1:4567/callback",
		"challenge",
		"S256",
		time.Now().UTC().Add(time.Hour),
		secret,
	)
	if err != nil {
		t.Fatalf("EncryptAuthorizationCode() error = %v", err)
	}

	_, _, _, _, err = DecryptToken(code, secret)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("DecryptToken() error = %v, want %v", err, ErrInvalidToken)
	}
}

// Tokens issued before v1.0 carried the instance URL as signoz_url; they must
// be rejected so the client authorizes again.
func TestDecryptRejectsTokensWithoutBylonisURL(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	legacy := map[string]any{
		"api_key":    "api-key",
		"signoz_url": "https://tenant.example.com",
		"client_id":  "client-1",
		"expires_at": time.Now().UTC().Add(time.Hour),
	}

	access, err := encryptTypedBlob(blobTypeAccessToken, legacy, secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := DecryptToken(access, secret); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("DecryptToken(legacy) error = %v, want ErrInvalidToken", err)
	}

	refresh, err := encryptTypedBlob(blobTypeRefreshToken, legacy, secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := DecryptRefreshToken(refresh, secret); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("DecryptRefreshToken(legacy) error = %v, want ErrInvalidToken", err)
	}

	code, err := encryptTypedBlob(blobTypeAuthorizationCode, legacy, secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, _, _, _, err := DecryptAuthorizationCode(code, secret); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("DecryptAuthorizationCode(legacy) error = %v, want ErrInvalidToken", err)
	}
}
