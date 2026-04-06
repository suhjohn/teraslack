package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
)

const (
	envCipherPrefix = "env:v1:"
	kmsCipherPrefix = "kms:v1:"
)

type Options struct {
	EnvKey         string
	AWSKMSKeyID    string
	AWSKMSRegion   string
	AWSKMSEndpoint string
}

type StringProtector struct {
	envKey   string
	kms      kmsClient
	kmsKeyID string
	primary  string
}

type kmsClient interface {
	Encrypt(ctx context.Context, params *kms.EncryptInput, optFns ...func(*kms.Options)) (*kms.EncryptOutput, error)
	Decrypt(ctx context.Context, params *kms.DecryptInput, optFns ...func(*kms.Options)) (*kms.DecryptOutput, error)
}

func NewStringProtector(ctx context.Context, options Options) (*StringProtector, error) {
	kmsKeyID := strings.TrimSpace(options.AWSKMSKeyID)
	var client kmsClient
	if kmsKeyID != "" {
		region := strings.TrimSpace(options.AWSKMSRegion)
		if region == "" {
			return nil, fmt.Errorf("AWS_KMS_REGION or AWS_REGION is required when AWS_KMS_KEY_ID is set")
		}
		cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
		if err != nil {
			return nil, fmt.Errorf("load aws config: %w", err)
		}
		endpoint := strings.TrimSpace(options.AWSKMSEndpoint)
		client = kms.NewFromConfig(cfg, func(o *kms.Options) {
			if endpoint != "" {
				o.BaseEndpoint = aws.String(endpoint)
			}
		})
	}
	return newStringProtector(options, client)
}

func newStringProtector(options Options, client kmsClient) (*StringProtector, error) {
	protector := &StringProtector{
		envKey:   strings.TrimSpace(options.EnvKey),
		kms:      client,
		kmsKeyID: strings.TrimSpace(options.AWSKMSKeyID),
	}
	if protector.kmsKeyID != "" && protector.kms == nil {
		return nil, fmt.Errorf("AWS_KMS_KEY_ID is configured but AWS KMS client initialization failed")
	}
	switch {
	case protector.kms != nil:
		protector.primary = kmsCipherPrefix
	case protector.envKey != "":
		protector.primary = envCipherPrefix
	default:
		return nil, fmt.Errorf("encryption requires AWS_KMS_KEY_ID or ENCRYPTION_KEY")
	}
	return protector, nil
}

func (p *StringProtector) EncryptString(ctx context.Context, plaintext string) (string, error) {
	switch p.primary {
	case kmsCipherPrefix:
		output, err := p.kms.Encrypt(ctx, &kms.EncryptInput{
			KeyId:     aws.String(p.kmsKeyID),
			Plaintext: []byte(plaintext),
		})
		if err != nil {
			return "", err
		}
		return kmsCipherPrefix + base64.StdEncoding.EncodeToString(output.CiphertextBlob), nil
	case envCipherPrefix:
		return EncryptString(p.envKey, plaintext)
	default:
		return "", fmt.Errorf("encryption provider is not configured")
	}
}

func (p *StringProtector) DecryptString(ctx context.Context, encoded string) (string, error) {
	switch {
	case strings.HasPrefix(encoded, kmsCipherPrefix):
		if p.kms == nil {
			return "", fmt.Errorf("kms ciphertext cannot be decrypted without AWS KMS configuration")
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, kmsCipherPrefix))
		if err != nil {
			return "", err
		}
		output, err := p.kms.Decrypt(ctx, &kms.DecryptInput{CiphertextBlob: raw})
		if err != nil {
			return "", err
		}
		return string(output.Plaintext), nil
	case strings.HasPrefix(encoded, envCipherPrefix):
		if p.envKey == "" {
			return "", fmt.Errorf("env ciphertext cannot be decrypted without ENCRYPTION_KEY")
		}
		return DecryptString(p.envKey, encoded)
	case strings.Contains(encoded, ":"):
		return "", fmt.Errorf("unsupported ciphertext format")
	default:
		if p.envKey == "" {
			return "", fmt.Errorf("legacy env ciphertext cannot be decrypted without ENCRYPTION_KEY")
		}
		return DecryptString(p.envKey, encoded)
	}
}

func RandomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func SHA256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func EncryptString(hexKey string, plaintext string) (string, error) {
	block, err := aes.NewCipher(parseKey(hexKey))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return envCipherPrefix + base64.StdEncoding.EncodeToString(append(nonce, ciphertext...)), nil
}

func DecryptString(hexKey string, encoded string) (string, error) {
	block, err := aes.NewCipher(parseKey(hexKey))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, envCipherPrefix))
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func HMACSHA256Hex(secret string, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func parseKey(hexKey string) []byte {
	raw, err := hex.DecodeString(hexKey)
	if err != nil || len(raw) != 32 {
		sum := sha256.Sum256([]byte(hexKey))
		return sum[:]
	}
	return raw
}
