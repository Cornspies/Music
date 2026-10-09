package deezer

import (
	"bytes"
	"crypto/cipher"
	"strings"
	"testing"

	"golang.org/x/crypto/blowfish"
)

func TestGetBlowfishKey(t *testing.T) {
	trackID := "3135556"
	key := GetBlowfishKey(trackID)
	if len(key) != 16 {
		t.Fatalf("expected key length 16, got %d", len(key))
	}

	// Key should be deterministic
	key2 := GetBlowfishKey(trackID)
	if !bytes.Equal(key, key2) {
		t.Fatalf("expected deterministic key, got %v vs %v", key, key2)
	}
}

func TestEncryptDownloadURL(t *testing.T) {
	md5Origin := "98127349817234981723498172349812"
	trackID := "3135556"
	qualityCode := 3
	mediaVersion := "1"

	url, err := EncryptDownloadURL(md5Origin, qualityCode, trackID, mediaVersion)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedPrefix := "https://e-cdns-proxy-9.dzcdn.net/mobile/1/"
	if !strings.HasPrefix(url, expectedPrefix) {
		t.Fatalf("expected URL to start with %q, got %q", expectedPrefix, url)
	}

	hexPart := strings.TrimPrefix(url, expectedPrefix)
	if len(hexPart)%32 != 0 {
		t.Fatalf("hex part length %d should be a multiple of 32 (16 AES bytes in hex)", len(hexPart))
	}
}

func TestDecryptBlowfishChunk(t *testing.T) {
	key := GetBlowfishKey("123456789")
	cipherBlock, err := blowfish.NewCipher(key)
	if err != nil {
		t.Fatalf("blowfish cipher init: %v", err)
	}

	// Prepare 2048 bytes of test data
	plaintext := make([]byte, 2048)
	for i := range plaintext {
		plaintext[i] = byte(i % 256)
	}

	// Encrypt using CBC with blowfishIV
	mode := cipher.NewCBCEncrypter(cipherBlock, blowfishIV)
	ciphertext := make([]byte, len(plaintext))
	mode.CryptBlocks(ciphertext, plaintext)

	// Decrypt using DecryptBlowfishChunk
	decrypted, err := DecryptBlowfishChunk(ciphertext, key)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Fatalf("decrypted data did not match plaintext")
	}
}

func TestDecryptDeezerStream(t *testing.T) {
	key := GetBlowfishKey("987654321")
	cipherBlock, err := blowfish.NewCipher(key)
	if err != nil {
		t.Fatalf("blowfish cipher init: %v", err)
	}

	// Create 3 full chunks (2048 bytes each) + 500 trailing bytes
	fullLen := 2048*3 + 500
	originalData := make([]byte, fullLen)
	for i := range originalData {
		originalData[i] = byte((i * 17) % 256)
	}

	// Encrypt only every 3rd chunk (chunk 0, chunk 3, etc.)
	encryptedStream := make([]byte, fullLen)
	copy(encryptedStream, originalData)

	// Chunk 0 (0..2048) should be encrypted
	mode := cipher.NewCBCEncrypter(cipherBlock, blowfishIV)
	mode.CryptBlocks(encryptedStream[0:2048], originalData[0:2048])

	// Chunks 1 and 2 are untouched
	// Trailing 500 bytes are untouched

	var out bytes.Buffer
	written, err := DecryptDeezerStream(bytes.NewReader(encryptedStream), &out, key, 0)
	if err != nil {
		t.Fatalf("stream decryption error: %v", err)
	}

	if written != int64(fullLen) {
		t.Fatalf("expected %d bytes written, got %d", fullLen, written)
	}

	if !bytes.Equal(originalData, out.Bytes()) {
		t.Fatalf("stream decryption output did not match original data")
	}
}

func TestDecryptDeezerStreamLimit(t *testing.T) {
	key := GetBlowfishKey("11223344")

	// 2048 * 2 = 4096 bytes
	fullLen := 4096
	data := make([]byte, fullLen)

	var out bytes.Buffer
	limit := int64(3000)
	written, err := DecryptDeezerStream(bytes.NewReader(data), &out, key, limit)
	if err != ErrFileTooLarge {
		t.Fatalf("expected ErrFileTooLarge, got %v", err)
	}
	if written > limit {
		t.Fatalf("expected written <= limit (%d), got %d", limit, written)
	}
}
