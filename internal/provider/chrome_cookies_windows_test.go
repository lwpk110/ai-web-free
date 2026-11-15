//go:build windows

package provider

import (
	"crypto/aes"
	"crypto/cipher"
	"testing"
)

func TestDecryptChromeAESGCMAndCleanValue(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef") // 32 bytes AES-256
	nonce := []byte("123456789012")                  // 12 bytes GCM nonce

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher failed: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("NewGCM failed: %v", err)
	}

	// 1. Standard v10 cookie payload
	plain := []byte("session_val_abc123")
	sealed := gcm.Seal(nil, nonce, plain, nil)
	enc := append([]byte("v10"), nonce...)
	enc = append(enc, sealed...)

	dec, err := decryptChromeAESGCM(key, enc)
	if err != nil {
		t.Fatalf("decryptChromeAESGCM failed: %v", err)
	}
	if got := cleanDecryptedCookieValue(dec); got != "session_val_abc123" {
		t.Fatalf("expected session_val_abc123, got %q", got)
	}

	// 2. Chrome 130+ payload with 32-byte binary domain hash prefix
	hashPrefix := make([]byte, 32)
	for i := range hashPrefix {
		hashPrefix[i] = byte(i) // contains non-printable bytes < 0x20
	}
	prefixedPlain := append(hashPrefix, []byte("xs_token_xyz")...)
	sealed2 := gcm.Seal(nil, nonce, prefixedPlain, nil)
	enc2 := append([]byte("v11"), nonce...)
	enc2 = append(enc2, sealed2...)

	dec2, err := decryptChromeAESGCM(key, enc2)
	if err != nil {
		t.Fatalf("decryptChromeAESGCM (prefixed) failed: %v", err)
	}
	if got := cleanDecryptedCookieValue(dec2); got != "xs_token_xyz" {
		t.Fatalf("expected xs_token_xyz, got %q", got)
	}
}
