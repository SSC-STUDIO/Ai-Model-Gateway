package config

import "testing"

func TestSecureStorageEncryptDecryptRoundTrip(t *testing.T) {
	storage, err := NewSecureStorage("unit-test-master-key")
	if err != nil {
		t.Fatalf("NewSecureStorage failed: %v", err)
	}

	ciphertext, err := storage.Encrypt("sk-secret-value")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	if !IsEncrypted(ciphertext) {
		t.Fatalf("expected encrypted value, got %q", ciphertext)
	}

	plaintext, err := storage.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if plaintext != "sk-secret-value" {
		t.Fatalf("expected decrypted plaintext to round-trip, got %q", plaintext)
	}
}

func TestMaskKeyHandlesEncryptedValues(t *testing.T) {
	if got := MaskKey("ENC:abcdef"); got != "ENC:****" {
		t.Fatalf("expected encrypted key to be masked, got %q", got)
	}
}
