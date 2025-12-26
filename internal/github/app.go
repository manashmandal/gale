package github

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v68/github"
)

// AppClient handles GitHub App authentication
type AppClient struct {
	appID      int64
	privateKey *rsa.PrivateKey

	// Token cache (installation_id -> token)
	tokenCache map[int64]*InstallationToken
	cacheMu    sync.RWMutex

	// HTTP client for API calls
	httpClient *http.Client
}

// InstallationToken represents a cached installation access token
type InstallationToken struct {
	Token     string
	ExpiresAt time.Time
}

// NewAppClient creates a new GitHub App client
func NewAppClient(appID int64, privateKeyPEM []byte) (*AppClient, error) {
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8 format
		keyInterface, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		var ok bool
		key, ok = keyInterface.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not RSA")
		}
	}

	return &AppClient{
		appID:      appID,
		privateKey: key,
		tokenCache: make(map[int64]*InstallationToken),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// NewAppClientFromFile creates a new GitHub App client from a private key file
func NewAppClientFromFile(appID int64, privateKeyPath string) (*AppClient, error) {
	keyData, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("reading private key file: %w", err)
	}
	return NewAppClient(appID, keyData)
}

// NewAppClientFromEnv creates a new GitHub App client from environment variable
func NewAppClientFromEnv(appID int64, envVar string) (*AppClient, error) {
	keyData := os.Getenv(envVar)
	if keyData == "" {
		return nil, fmt.Errorf("environment variable %s is not set", envVar)
	}
	return NewAppClient(appID, []byte(keyData))
}

// GenerateJWT creates a JWT for GitHub App authentication
// JWTs are valid for up to 10 minutes
func (a *AppClient) GenerateJWT() (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iat": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
		"iss": a.appID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(a.privateKey)
}

// GetInstallationToken returns a cached or new installation access token
func (a *AppClient) GetInstallationToken(ctx context.Context, installationID int64) (string, error) {
	// Check cache first
	a.cacheMu.RLock()
	if token, ok := a.tokenCache[installationID]; ok {
		// Return cached token if it's valid for at least 5 more minutes
		if time.Now().Add(5 * time.Minute).Before(token.ExpiresAt) {
			a.cacheMu.RUnlock()
			return token.Token, nil
		}
	}
	a.cacheMu.RUnlock()

	// Generate new token
	return a.refreshInstallationToken(ctx, installationID)
}

// refreshInstallationToken fetches a new installation access token from GitHub
func (a *AppClient) refreshInstallationToken(ctx context.Context, installationID int64) (string, error) {
	jwt, err := a.GenerateJWT()
	if err != nil {
		return "", fmt.Errorf("generating JWT: %w", err)
	}

	// Create GitHub client with JWT auth
	client := github.NewClient(nil).WithAuthToken(jwt)

	// Get installation token
	token, _, err := client.Apps.CreateInstallationToken(ctx, installationID, nil)
	if err != nil {
		return "", fmt.Errorf("creating installation token: %w", err)
	}

	// Cache the token
	a.cacheMu.Lock()
	a.tokenCache[installationID] = &InstallationToken{
		Token:     token.GetToken(),
		ExpiresAt: token.GetExpiresAt().Time,
	}
	a.cacheMu.Unlock()

	return token.GetToken(), nil
}

// GetInstallations returns all installations for this app
func (a *AppClient) GetInstallations(ctx context.Context) ([]*github.Installation, error) {
	jwt, err := a.GenerateJWT()
	if err != nil {
		return nil, fmt.Errorf("generating JWT: %w", err)
	}

	client := github.NewClient(nil).WithAuthToken(jwt)

	installations, _, err := client.Apps.ListInstallations(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("listing installations: %w", err)
	}

	return installations, nil
}

// GetInstallationForRepo finds the installation ID for a specific repository
func (a *AppClient) GetInstallationForRepo(ctx context.Context, owner, repo string) (int64, error) {
	jwt, err := a.GenerateJWT()
	if err != nil {
		return 0, fmt.Errorf("generating JWT: %w", err)
	}

	client := github.NewClient(nil).WithAuthToken(jwt)

	installation, _, err := client.Apps.FindRepositoryInstallation(ctx, owner, repo)
	if err != nil {
		return 0, fmt.Errorf("finding installation for %s/%s: %w", owner, repo, err)
	}

	return installation.GetID(), nil
}

// ValidateCredentials tests that the app credentials are valid
func (a *AppClient) ValidateCredentials(ctx context.Context) error {
	jwt, err := a.GenerateJWT()
	if err != nil {
		return fmt.Errorf("generating JWT: %w", err)
	}

	client := github.NewClient(nil).WithAuthToken(jwt)

	// Try to get app info - this validates the JWT
	app, _, err := client.Apps.Get(ctx, "")
	if err != nil {
		return fmt.Errorf("invalid app credentials: %w", err)
	}

	if app.GetID() != a.appID {
		return fmt.Errorf("app ID mismatch: expected %d, got %d", a.appID, app.GetID())
	}

	return nil
}

// GetAppInfo returns information about the GitHub App
func (a *AppClient) GetAppInfo(ctx context.Context) (*github.App, error) {
	jwt, err := a.GenerateJWT()
	if err != nil {
		return nil, fmt.Errorf("generating JWT: %w", err)
	}

	client := github.NewClient(nil).WithAuthToken(jwt)

	app, _, err := client.Apps.Get(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("getting app info: %w", err)
	}

	return app, nil
}

// ClearTokenCache clears all cached installation tokens
func (a *AppClient) ClearTokenCache() {
	a.cacheMu.Lock()
	a.tokenCache = make(map[int64]*InstallationToken)
	a.cacheMu.Unlock()
}

// GetCachedTokenCount returns the number of cached tokens
func (a *AppClient) GetCachedTokenCount() int {
	a.cacheMu.RLock()
	defer a.cacheMu.RUnlock()
	return len(a.tokenCache)
}
