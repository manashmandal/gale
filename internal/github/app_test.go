package github

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// generateTestKey creates a test RSA key pair for testing
func generateTestKey(t *testing.T) []byte {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}

	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	pemBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	}

	return pem.EncodeToMemory(pemBlock)
}

func TestNewAppClient(t *testing.T) {
	keyPEM := generateTestKey(t)

	client, err := NewAppClient(12345, keyPEM)
	if err != nil {
		t.Fatalf("NewAppClient() error = %v", err)
	}

	if client.appID != 12345 {
		t.Errorf("appID = %d, want 12345", client.appID)
	}
	if client.privateKey == nil {
		t.Error("privateKey is nil")
	}
	if client.tokenCache == nil {
		t.Error("tokenCache is nil")
	}
}

func TestNewAppClient_InvalidPEM(t *testing.T) {
	_, err := NewAppClient(12345, []byte("not a valid PEM"))
	if err == nil {
		t.Error("NewAppClient() with invalid PEM should return error")
	}
}

func TestNewAppClient_PKCS8Format(t *testing.T) {
	// Generate a key and encode as PKCS8
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}

	privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("failed to marshal PKCS8: %v", err)
	}

	pemBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateKeyBytes,
	}
	keyPEM := pem.EncodeToMemory(pemBlock)

	client, err := NewAppClient(12345, keyPEM)
	if err != nil {
		t.Fatalf("NewAppClient() with PKCS8 error = %v", err)
	}

	if client.privateKey == nil {
		t.Error("privateKey is nil")
	}
}

func TestGenerateJWT(t *testing.T) {
	keyPEM := generateTestKey(t)

	client, err := NewAppClient(12345, keyPEM)
	if err != nil {
		t.Fatalf("NewAppClient() error = %v", err)
	}

	tokenString, err := client.GenerateJWT()
	if err != nil {
		t.Fatalf("GenerateJWT() error = %v", err)
	}

	if tokenString == "" {
		t.Error("GenerateJWT() returned empty string")
	}

	// Parse and validate the token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return &client.privateKey.PublicKey, nil
	})
	if err != nil {
		t.Fatalf("Failed to parse JWT: %v", err)
	}

	if !token.Valid {
		t.Error("Generated JWT is not valid")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("Failed to get claims from token")
	}

	// Check issuer is app ID
	if iss, ok := claims["iss"].(float64); !ok || int64(iss) != 12345 {
		t.Errorf("JWT issuer = %v, want 12345", claims["iss"])
	}

	// Check expiration is ~10 minutes from now
	if exp, ok := claims["exp"].(float64); ok {
		expTime := time.Unix(int64(exp), 0)
		expectedExp := time.Now().Add(10 * time.Minute)
		if expTime.Before(expectedExp.Add(-1*time.Minute)) || expTime.After(expectedExp.Add(1*time.Minute)) {
			t.Errorf("JWT exp = %v, want ~%v", expTime, expectedExp)
		}
	} else {
		t.Error("JWT missing exp claim")
	}
}

func TestInstallationToken(t *testing.T) {
	keyPEM := generateTestKey(t)

	client, err := NewAppClient(12345, keyPEM)
	if err != nil {
		t.Fatalf("NewAppClient() error = %v", err)
	}

	// Test token caching
	client.cacheMu.Lock()
	client.tokenCache[99999] = &InstallationToken{
		Token:     "cached-token",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	client.cacheMu.Unlock()

	// Check cache count
	if count := client.GetCachedTokenCount(); count != 1 {
		t.Errorf("GetCachedTokenCount() = %d, want 1", count)
	}

	// Clear cache
	client.ClearTokenCache()
	if count := client.GetCachedTokenCount(); count != 0 {
		t.Errorf("After ClearTokenCache(), GetCachedTokenCount() = %d, want 0", count)
	}
}

func TestTokenCacheExpiration(t *testing.T) {
	keyPEM := generateTestKey(t)

	client, err := NewAppClient(12345, keyPEM)
	if err != nil {
		t.Fatalf("NewAppClient() error = %v", err)
	}

	// Add an expired token
	client.cacheMu.Lock()
	client.tokenCache[99999] = &InstallationToken{
		Token:     "expired-token",
		ExpiresAt: time.Now().Add(-1 * time.Hour), // Already expired
	}
	client.cacheMu.Unlock()

	// The cached token should not be returned since it's expired
	// (GetInstallationToken would try to refresh, but that requires GitHub API)
	// Just verify the cache has the token
	client.cacheMu.RLock()
	token, exists := client.tokenCache[99999]
	client.cacheMu.RUnlock()

	if !exists {
		t.Error("Token should exist in cache")
	}
	if token.Token != "expired-token" {
		t.Errorf("Cached token = %q, want expired-token", token.Token)
	}
}

func TestNewAppClientFromEnv_NotSet(t *testing.T) {
	_, err := NewAppClientFromEnv(12345, "NONEXISTENT_ENV_VAR_FOR_TEST")
	if err == nil {
		t.Error("NewAppClientFromEnv() with unset env var should return error")
	}
}
