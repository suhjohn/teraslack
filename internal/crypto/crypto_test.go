package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func generateTestKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return hex.EncodeToString(key)
}

func TestEncryptDecrypt(t *testing.T) {
	keyHex := generateTestKey(t)
	provider, err := NewEnvKeyProvider(keyHex, nil)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	enc := NewEncryptor(provider)
	plaintext := "super-secret-webhook-key-12345"

	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Ciphertext should be prefixed.
	if !strings.HasPrefix(ciphertext, ciphertextPrefix) {
		t.Errorf("ciphertext missing prefix, got %q", ciphertext[:20])
	}

	// Ciphertext should NOT contain the plaintext.
	if strings.Contains(ciphertext, plaintext) {
		t.Error("ciphertext contains plaintext")
	}

	// Decrypt should recover the plaintext.
	decrypted, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptDecrypt_DifferentCiphertexts(t *testing.T) {
	keyHex := generateTestKey(t)
	provider, err := NewEnvKeyProvider(keyHex, nil)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	enc := NewEncryptor(provider)
	plaintext := "same-input"

	ct1, _ := enc.Encrypt(plaintext)
	ct2, _ := enc.Encrypt(plaintext)

	// Two encryptions of the same plaintext should produce different ciphertexts
	// (because of random nonces).
	if ct1 == ct2 {
		t.Error("two encryptions produced identical ciphertext — nonce reuse!")
	}

	// Both should decrypt to the same plaintext.
	d1, _ := enc.Decrypt(ct1)
	d2, _ := enc.Decrypt(ct2)
	if d1 != plaintext || d2 != plaintext {
		t.Error("decryption mismatch")
	}
}

func TestDecrypt_RejectsPlaintext(t *testing.T) {
	keyHex := generateTestKey(t)
	provider, err := NewEnvKeyProvider(keyHex, nil)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	enc := NewEncryptor(provider)
	plaintext := "not-encrypted-value"

	_, err = enc.Decrypt(plaintext)
	if err == nil {
		t.Fatal("expected plaintext decrypt to fail")
	}
}

func TestMissingKeyProvider(t *testing.T) {
	enc := NewEncryptor(nil)
	_, err := enc.Encrypt("hello")
	if err == nil {
		t.Fatal("expected encrypt to fail")
	}
	if !errors.Is(err, ErrKeyNotConfigured) {
		t.Fatalf("encrypt: %v", err)
	}

	_, err = enc.Decrypt("hello")
	if err == nil {
		t.Fatal("expected decrypt to fail")
	}
	if !errors.Is(err, ErrKeyNotConfigured) {
		t.Fatalf("decrypt: %v", err)
	}
}

func TestKeyRotation(t *testing.T) {
	oldKeyHex := generateTestKey(t)
	newKeyHex := generateTestKey(t)

	// Create provider with old key.
	oldProvider, err := NewEnvKeyProvider(oldKeyHex, nil)
	if err != nil {
		t.Fatalf("create old provider: %v", err)
	}
	oldEnc := NewEncryptor(oldProvider)

	// Encrypt with old key.
	ciphertext, err := oldEnc.Encrypt("rotate-me")
	if err != nil {
		t.Fatalf("encrypt with old key: %v", err)
	}

	// Create new provider that knows about the old key.
	oldKeyID := oldProvider.CurrentKeyID()
	newProvider, err := NewEnvKeyProvider(newKeyHex, map[string]string{
		oldKeyID: oldKeyHex,
	})
	if err != nil {
		t.Fatalf("create new provider: %v", err)
	}
	newEnc := NewEncryptor(newProvider)

	// Decrypt old ciphertext with new provider (using old key via rotation).
	decrypted, err := newEnc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt with rotated key: %v", err)
	}
	if decrypted != "rotate-me" {
		t.Errorf("decrypted = %q, want %q", decrypted, "rotate-me")
	}

	// New encryptions use the new key.
	newCt, err := newEnc.Encrypt("new-data")
	if err != nil {
		t.Fatalf("encrypt with new key: %v", err)
	}
	newDecrypted, err := newEnc.Decrypt(newCt)
	if err != nil {
		t.Fatalf("decrypt new data: %v", err)
	}
	if newDecrypted != "new-data" {
		t.Errorf("new decrypted = %q, want %q", newDecrypted, "new-data")
	}
}

func TestHashToken(t *testing.T) {
	token := "xoxb-abc123deadbeef"
	hash := HashToken(token)

	// SHA-256 hex digest is always 64 chars.
	if len(hash) != 64 {
		t.Errorf("hash length = %d, want 64", len(hash))
	}

	// Same input produces same hash (deterministic).
	if HashToken(token) != hash {
		t.Error("hash is not deterministic")
	}

	// Different input produces different hash.
	if HashToken("xoxb-different") == hash {
		t.Error("different tokens produced same hash")
	}
}

func TestRedactToken(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"xoxb-abc123deadbeef", "xoxb-****beef"},
		{"xoxp-hello-world-test", "xoxp-****test"},
		{"short", "****"},
		{"12345678", "****"},
		{"123456789", "1234****6789"},
	}

	for _, tt := range tests {
		got := RedactToken(tt.input)
		if got != tt.want {
			t.Errorf("RedactToken(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsEncrypted(t *testing.T) {
	if IsEncrypted("plaintext") {
		t.Error("plaintext detected as encrypted")
	}
	if !IsEncrypted("enc:v1:abc:data") {
		t.Error("encrypted value not detected")
	}
}

func TestEnvKeyProvider_InvalidKey(t *testing.T) {
	// Empty key.
	_, err := NewEnvKeyProvider("", nil)
	if err != ErrKeyNotConfigured {
		t.Errorf("empty key: got %v, want ErrKeyNotConfigured", err)
	}

	// Too short.
	_, err = NewEnvKeyProvider("abcd", nil)
	if err == nil {
		t.Error("short key: expected error")
	}

	// Invalid hex.
	_, err = NewEnvKeyProvider("not-hex-not-hex-not-hex-not-hex-not-hex-not-hex-not-hex-not-hex-", nil)
	if err == nil {
		t.Error("invalid hex: expected error")
	}
}
