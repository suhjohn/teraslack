package crypto

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/kms"
)

type fakeKMSClient struct{}

func (fakeKMSClient) Encrypt(_ context.Context, params *kms.EncryptInput, _ ...func(*kms.Options)) (*kms.EncryptOutput, error) {
	return &kms.EncryptOutput{
		CiphertextBlob: append([]byte("kms:"), params.Plaintext...),
	}, nil
}

func (fakeKMSClient) Decrypt(_ context.Context, params *kms.DecryptInput, _ ...func(*kms.Options)) (*kms.DecryptOutput, error) {
	return &kms.DecryptOutput{
		Plaintext: []byte(strings.TrimPrefix(string(params.CiphertextBlob), "kms:")),
	}, nil
}

func TestEncryptStringUsesEnvPrefixAndDecryptsLegacyCiphertext(t *testing.T) {
	key := strings.Repeat("11", 32)

	encrypted, err := EncryptString(key, "shared-secret")
	if err != nil {
		t.Fatalf("encrypt string: %v", err)
	}
	if !strings.HasPrefix(encrypted, envCipherPrefix) {
		t.Fatalf("encrypted value %q missing %s prefix", encrypted, envCipherPrefix)
	}

	decrypted, err := DecryptString(key, encrypted)
	if err != nil {
		t.Fatalf("decrypt prefixed ciphertext: %v", err)
	}
	if decrypted != "shared-secret" {
		t.Fatalf("decrypt prefixed ciphertext = %q, want shared-secret", decrypted)
	}

	legacy := strings.TrimPrefix(encrypted, envCipherPrefix)
	decrypted, err = DecryptString(key, legacy)
	if err != nil {
		t.Fatalf("decrypt legacy ciphertext: %v", err)
	}
	if decrypted != "shared-secret" {
		t.Fatalf("decrypt legacy ciphertext = %q, want shared-secret", decrypted)
	}
}

func TestNewStringProtectorRequiresProvider(t *testing.T) {
	if _, err := NewStringProtector(context.Background(), Options{}); err == nil {
		t.Fatal("NewStringProtector unexpectedly succeeded without a provider")
	}
}

func TestStringProtectorPrefersKMSAndStillDecryptsEnvCiphertext(t *testing.T) {
	key := strings.Repeat("11", 32)
	protector, err := newStringProtector(Options{
		EnvKey:       key,
		AWSKMSKeyID:  "alias/teraslack",
		AWSKMSRegion: "us-west-2",
	}, fakeKMSClient{})
	if err != nil {
		t.Fatalf("build protector: %v", err)
	}

	encrypted, err := protector.EncryptString(context.Background(), "kms-secret")
	if err != nil {
		t.Fatalf("encrypt with protector: %v", err)
	}
	if !strings.HasPrefix(encrypted, kmsCipherPrefix) {
		t.Fatalf("kms encrypted value %q missing %s prefix", encrypted, kmsCipherPrefix)
	}

	decrypted, err := protector.DecryptString(context.Background(), encrypted)
	if err != nil {
		t.Fatalf("decrypt kms ciphertext: %v", err)
	}
	if decrypted != "kms-secret" {
		t.Fatalf("decrypt kms ciphertext = %q, want kms-secret", decrypted)
	}

	envEncrypted, err := EncryptString(key, "env-secret")
	if err != nil {
		t.Fatalf("encrypt env ciphertext: %v", err)
	}
	decrypted, err = protector.DecryptString(context.Background(), envEncrypted)
	if err != nil {
		t.Fatalf("decrypt env ciphertext through protector: %v", err)
	}
	if decrypted != "env-secret" {
		t.Fatalf("decrypt env ciphertext through protector = %q, want env-secret", decrypted)
	}

	legacy := strings.TrimPrefix(envEncrypted, envCipherPrefix)
	decrypted, err = protector.DecryptString(context.Background(), legacy)
	if err != nil {
		t.Fatalf("decrypt legacy env ciphertext through protector: %v", err)
	}
	if decrypted != "env-secret" {
		t.Fatalf("decrypt legacy env ciphertext through protector = %q, want env-secret", decrypted)
	}
}
