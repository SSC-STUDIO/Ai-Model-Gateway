package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"crypto/pbkdf2"
)

const (
	// Encryption constants
	saltSize   = 32
	keySize    = 32
	iterations = 100000

	// Prefix for encrypted values
	encryptedPrefix = "ENC:"
)

// SecureStorage provides encrypted storage for sensitive data
type SecureStorage struct {
	masterKey []byte
}

// NewSecureStorage creates a new secure storage instance
// It derives the encryption key from the provided master key or environment variable
func NewSecureStorage(masterKey string) (*SecureStorage, error) {
	// Try to get master key from environment if not provided
	if masterKey == "" {
		masterKey = os.Getenv("AI_GATEWAY_MASTER_KEY")
	}

	if masterKey == "" {
		return nil, fmt.Errorf("master key not provided and AI_GATEWAY_MASTER_KEY not set")
	}

	// Derive a fixed-size key from the master key
	key, err := deriveKey(masterKey, []byte("ai-gateway-fixed-salt"))
	if err != nil {
		return nil, fmt.Errorf("derive master key: %w", err)
	}

	return &SecureStorage{
		masterKey: key,
	}, nil
}

// deriveKey derives a fixed-size key from a password using PBKDF2
func deriveKey(password string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, salt, iterations, keySize)
}

// Encrypt encrypts a plaintext string using AES-GCM
func (s *SecureStorage) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	// Don't double-encrypt
	if strings.HasPrefix(plaintext, encryptedPrefix) {
		return plaintext, nil
	}

	block, err := aes.NewCipher(s.masterKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and authenticate
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Return as base64-encoded string with prefix
	return encryptedPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a ciphertext string using AES-GCM
func (s *SecureStorage) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	// Check if it's encrypted
	if !strings.HasPrefix(ciphertext, encryptedPrefix) {
		// Not encrypted, return as-is
		return ciphertext, nil
	}

	// Remove prefix
	encoded := strings.TrimPrefix(ciphertext, encryptedPrefix)

	// Decode base64
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(s.masterKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// IsEncrypted checks if a value is encrypted
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, encryptedPrefix)
}

// MaskKey masks an API key for logging/display purposes
func MaskKey(key string) string {
	if key == "" {
		return ""
	}

	// If encrypted, show a masked version
	if IsEncrypted(key) {
		return "ENC:****"
	}

	// For plaintext keys, show first 4 and last 4 characters
	if len(key) <= 8 {
		return "****"
	}

	return key[:4] + "****" + key[len(key)-4:]
}

// HashKey creates a one-way hash of a key for comparison/validation
func HashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

// SecureConfig wraps Config with encryption capabilities
type SecureConfig struct {
	*Config
	storage *SecureStorage
}

// NewSecureConfig creates a new secure configuration wrapper
func NewSecureConfig(cfg *Config, masterKey string) (*SecureConfig, error) {
	storage, err := NewSecureStorage(masterKey)
	if err != nil {
		return nil, err
	}

	return &SecureConfig{
		Config:  cfg,
		storage: storage,
	}, nil
}

// EncryptUpstreamAPIKeys encrypts all API keys in upstreams
func (sc *SecureConfig) EncryptUpstreamAPIKeys() error {
	for i := range sc.Upstreams {
		if sc.Upstreams[i].APIKey != "" && !IsEncrypted(sc.Upstreams[i].APIKey) {
			encrypted, err := sc.storage.Encrypt(sc.Upstreams[i].APIKey)
			if err != nil {
				return fmt.Errorf("failed to encrypt API key for upstream %s: %w", sc.Upstreams[i].Name, err)
			}
			sc.Upstreams[i].APIKey = encrypted
		}
	}
	return nil
}

// DecryptUpstreamAPIKey decrypts the API key for a specific upstream
func (sc *SecureConfig) DecryptUpstreamAPIKey(upstreamIndex int) (string, error) {
	if upstreamIndex < 0 || upstreamIndex >= len(sc.Upstreams) {
		return "", fmt.Errorf("invalid upstream index: %d", upstreamIndex)
	}

	apiKey := sc.Upstreams[upstreamIndex].APIKey
	if apiKey == "" {
		return "", nil
	}

	return sc.storage.Decrypt(apiKey)
}

// GetUpstreamAPIKey returns the decrypted API key for an upstream (for use in requests)
func (sc *SecureConfig) GetUpstreamAPIKey(upstreamName string) (string, error) {
	for i, u := range sc.Upstreams {
		if u.Name == upstreamName {
			return sc.DecryptUpstreamAPIKey(i)
		}
	}
	return "", fmt.Errorf("upstream not found: %s", upstreamName)
}

// SanitizedConfig returns a copy of the config with sensitive values masked
func (sc *SecureConfig) SanitizedConfig() *Config {
	// Deep copy the config
	sanitized := &Config{
		Listen:    sc.Config.Listen,
		Reload:    sc.Config.Reload,
		Router:    sc.Config.Router,
		Health:    sc.Config.Health,
		Admin:     sc.Config.Admin,
		Telemetry: sc.Config.Telemetry,
		Pricing:   sc.Config.Pricing,
		Bridge:    sc.Config.Bridge,
		Proxy:     sc.Config.Proxy,
		Upstreams: make([]Upstream, len(sc.Config.Upstreams)),
	}

	// Copy upstreams with masked API keys
	for i, u := range sc.Config.Upstreams {
		sanitized.Upstreams[i] = Upstream{
			Name:                u.Name,
			BaseURL:             u.BaseURL,
			APIKey:              MaskKey(u.APIKey),
			ProviderClass:       u.ProviderClass,
			Models:              append([]string(nil), u.Models...),
			Weight:              u.Weight,
			TimeoutMs:           u.TimeoutMs,
			SameUpstreamRetries: u.SameUpstreamRetries,
			Enabled:             u.Enabled,
			Headers:             copyHeaders(u.Headers),
		}
	}

	// Mask admin auth token if present
	if sanitized.Admin.AuthToken != "" {
		sanitized.Admin.AuthToken = MaskKey(sanitized.Admin.AuthToken)
	}

	return sanitized
}

func copyHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}
	copied := make(map[string]string, len(headers))
	for k, v := range headers {
		copied[k] = v
	}
	return copied
}

// GenerateMasterKey generates a new random master key
func GenerateMasterKey() (string, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key), nil
}
