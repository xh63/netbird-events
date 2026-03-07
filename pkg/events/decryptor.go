package events

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
)

// NetbirdDecryptor decrypts AES-256-GCM encrypted fields from NetBird's database.
// NetBird encrypts sensitive columns (email, name) before writing to PostgreSQL or SQLite.
//
// Verified against NetBird management server as of early 2026.
// Wire format: base64( nonce[12 bytes] | ciphertext | GCM auth tag[16 bytes] )
//
// If NetBird changes its encryption scheme in a future release, this file will need
// to be updated. The symptom is base64-looking strings appearing in initiator_email /
// target_email output despite decryption being enabled. Check NetBird release notes
// for any changes to field encryption and update the wire format parsing accordingly.
type NetbirdDecryptor struct {
	gcm cipher.AEAD
}

// NewNetbirdDecryptor creates a decryptor from a raw 32-byte AES-256 key.
func NewNetbirdDecryptor(key []byte) (*NetbirdDecryptor, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}
	return &NetbirdDecryptor{gcm: gcm}, nil
}

// Decrypt attempts to decrypt a base64-encoded AES-GCM ciphertext.
// If the value is not valid base64, is too short, or decryption fails (e.g. the
// field is not encrypted), the original value is returned unchanged — callers
// are always safe to call this without knowing whether a value is encrypted.
func (d *NetbirdDecryptor) Decrypt(value string) string {
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return value // not base64 — plaintext field, return as-is
	}
	if len(data) < d.gcm.NonceSize() {
		return value // too short to contain a nonce
	}
	nonce, ciphertext := data[:d.gcm.NonceSize()], data[d.gcm.NonceSize():]
	plaintext, err := d.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return value // decryption failed — value may not be encrypted
	}
	return string(plaintext)
}
