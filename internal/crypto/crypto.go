// Package crypto provides enterprise-ready encryption primitives for sensitive data.
//
// It implements:
//   - AES-256-GCM authenticated encryption with envelope pattern
//   - SHA-256 token hashing for one-way credential storage
//   - A KeyProvider interface for pluggable key management (env, AWS KMS, Vault)
//   - Field-level redaction helpers for event log sanitization
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Sentinel errors.
var (
	ErrInvalidCiphertext = errors.New("crypto: invalid ciphertext")
	ErrKeyNotConfigured  = errors.New("crypto: encryption key not configured")
	ErrKeyTooShort       = errors.New("crypto: key must be 32 bytes for AES-256")
)

// ciphertextPrefix identifies encrypted values.
const ciphertextPrefix = "enc:v1:"

// KeyProvider abstracts the source of encryption keys.
// Implementations can pull keys from environment variables, AWS KMS,
// HashiCorp Vault, GCP KMS, etc.
type KeyProvider interface {
	// GetKey returns the current data encryption key (DEK).
	// The key MUST be exactly 32 bytes (AES-256).
	GetKey() ([]byte, error)

	// GetKeyByID returns a DEK by its identifier (for key rotation).
	// If id is empty, behaves like GetKey (returns current key).
	GetKeyByID(id string) ([]byte, error)

	// CurrentKeyID returns the identifier of the current active key.
	CurrentKeyID() string
}

// Encryptor provides encrypt/decrypt operations using AES-256-GCM.
type Encryptor struct {
	provider KeyProvider
}

// NewEncryptor creates an Encryptor with the given key provider.
func NewEncryptor(provider KeyProvider) *Encryptor {
	return &Encryptor{provider: provider}
}

// Encrypt encrypts plaintext using AES-256-GCM and returns a prefixed,
// base64-encoded ciphertext string: "enc:v1:<keyID>:<base64(nonce+ciphertext)>"
func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	if e == nil || e.provider == nil {
		return "", ErrKeyNotConfigured
	}

	key, err := e.provider.GetKey()
	if err != nil {
		return "", fmt.Errorf("get encryption key: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	encoded := base64.StdEncoding.EncodeToString(ciphertext)

	keyID := e.provider.CurrentKeyID()
	return fmt.Sprintf("%s%s:%s", ciphertextPrefix, keyID, encoded), nil
}

// Decrypt decrypts a value produced by Encrypt.
func (e *Encryptor) Decrypt(value string) (string, error) {
	if e == nil || e.provider == nil {
		return "", ErrKeyNotConfigured
	}

	if !strings.HasPrefix(value, ciphertextPrefix) {
		return "", ErrInvalidCiphertext
	}

	// Parse: "enc:v1:<keyID>:<base64>"
	rest := strings.TrimPrefix(value, ciphertextPrefix)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return "", ErrInvalidCiphertext
	}
	keyID := parts[0]
	encoded := parts[1]

	key, err := e.provider.GetKeyByID(keyID)
	if err != nil {
		return "", fmt.Errorf("get key %q: %w", keyID, err)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", ErrInvalidCiphertext
	}

	nonce, ciphertextBody := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBody, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}

// IsEncrypted returns true if the value appears to be encrypted.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, ciphertextPrefix)
}

// HashToken produces a SHA-256 hex digest of a token string.
// This is a one-way operation — the original token cannot be recovered.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// RedactToken replaces a token with a redacted version that preserves the
// prefix (xoxb-/xoxp-) and the last 4 characters for debugging.
// Example: "xoxb-abc123deadbeef" → "xoxb-****beef"
func RedactToken(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	// Find the prefix (e.g., "xoxb-", "xoxp-")
	prefixEnd := strings.Index(token, "-")
	if prefixEnd == -1 || prefixEnd >= len(token)-4 {
		return token[:4] + "****" + token[len(token)-4:]
	}
	prefix := token[:prefixEnd+1]
	suffix := token[len(token)-4:]
	return prefix + "****" + suffix
}

// EnvKeyProvider loads encryption keys from environment-style configuration.
// It supports a primary key and optionally previous keys for rotation.
type EnvKeyProvider struct {
	currentKey   []byte
	currentKeyID string
	previousKeys map[string][]byte // keyID → key (for decrypting old data)
}

// NewEnvKeyProvider creates a key provider from a hex-encoded 32-byte key.
// previousKeys is a map of keyID → hex-encoded key for key rotation support.
func NewEnvKeyProvider(currentKeyHex string, previousKeys map[string]string) (*EnvKeyProvider, error) {
	if currentKeyHex == "" {
		return nil, ErrKeyNotConfigured
	}

	currentKey, err := hex.DecodeString(currentKeyHex)
	if err != nil {
		return nil, fmt.Errorf("decode current key: %w", err)
	}
	if len(currentKey) != 32 {
		return nil, ErrKeyTooShort
	}

	// Derive a stable key ID from the key itself (first 8 bytes of SHA-256).
	keyIDHash := sha256.Sum256(currentKey)
	currentKeyID := hex.EncodeToString(keyIDHash[:4])

	prevKeys := make(map[string][]byte)
	prevKeys[currentKeyID] = currentKey // current key is also accessible by ID

	for id, hexKey := range previousKeys {
		k, err := hex.DecodeString(hexKey)
		if err != nil {
			return nil, fmt.Errorf("decode previous key %q: %w", id, err)
		}
		if len(k) != 32 {
			return nil, fmt.Errorf("previous key %q: %w", id, ErrKeyTooShort)
		}
		prevKeys[id] = k
	}

	return &EnvKeyProvider{
		currentKey:   currentKey,
		currentKeyID: currentKeyID,
		previousKeys: prevKeys,
	}, nil
}

func (p *EnvKeyProvider) GetKey() ([]byte, error) {
	return p.currentKey, nil
}

func (p *EnvKeyProvider) GetKeyByID(id string) ([]byte, error) {
	if id == "" {
		return p.currentKey, nil
	}
	k, ok := p.previousKeys[id]
	if !ok {
		return nil, fmt.Errorf("unknown key ID %q", id)
	}
	return k, nil
}

func (p *EnvKeyProvider) CurrentKeyID() string {
	return p.currentKeyID
}
