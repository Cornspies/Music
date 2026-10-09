package deezer

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"

	"golang.org/x/crypto/blowfish"
)

func newBlowfishCipher(key []byte) (cipher.Block, error) {
	return blowfish.NewCipher(key)
}

var (
	blowfishSecret = []byte("g4el58wc0zvf9na1")
	aesKey         = []byte("jo6aey6haid2Teih")
	blowfishIV     = []byte{0, 1, 2, 3, 4, 5, 6, 7}

	// ErrFileTooLarge is returned when a stream exceeds the maximum allowed byte limit.
	ErrFileTooLarge = errors.New("file exceeds maximum allowed size")
)

// GetBlowfishKey derives the 16-byte Blowfish key for decrypting a Deezer track.
func GetBlowfishKey(trackID string) []byte {
	h := md5.Sum([]byte(trackID))
	hexStr := fmt.Sprintf("%x", h)
	key := make([]byte, 16)
	for i := 0; i < 16; i++ {
		key[i] = hexStr[i] ^ hexStr[i+16] ^ blowfishSecret[i]
	}
	return key
}

// EncryptDownloadURL constructs Deezer's mobile CDN streaming URL for a track using AES-128-ECB.
func EncryptDownloadURL(md5Origin string, qualityCode int, trackID, mediaVersion string) (string, error) {
	if len(md5Origin) == 0 {
		return "", errors.New("track unavailable (invalid MD5_ORIGIN)")
	}

	var sep byte = 0xa4
	var step1Buf bytes.Buffer
	step1Buf.WriteString(md5Origin)
	step1Buf.WriteByte(sep)
	step1Buf.WriteString(strconv.Itoa(qualityCode))
	step1Buf.WriteByte(sep)
	step1Buf.WriteString(trackID)
	step1Buf.WriteByte(sep)
	step1Buf.WriteString(mediaVersion)
	step1Bytes := step1Buf.Bytes()

	step1Hash := md5.Sum(step1Bytes)
	step1MD5 := fmt.Sprintf("%x", step1Hash)

	var step2Buf bytes.Buffer
	step2Buf.WriteString(step1MD5)
	step2Buf.WriteByte(sep)
	step2Buf.Write(step1Bytes)
	step2Buf.WriteByte(sep)
	step2Bytes := step2Buf.Bytes()

	if rem := len(step2Bytes) % 16; rem != 0 {
		padLen := 16 - rem
		step2Bytes = append(step2Bytes, bytes.Repeat([]byte(" "), padLen)...)
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", fmt.Errorf("aes init: %w", err)
	}

	encrypted := make([]byte, len(step2Bytes))
	for i := 0; i < len(step2Bytes); i += 16 {
		block.Encrypt(encrypted[i:i+16], step2Bytes[i:i+16])
	}

	hexResult := hex.EncodeToString(encrypted)
	cdn := md5Origin[0:1]
	return fmt.Sprintf("https://e-cdns-proxy-%s.dzcdn.net/mobile/1/%s", cdn, hexResult), nil
}

// DecryptBlowfishChunk decrypts a single 2048-byte chunk with Blowfish in CBC mode.
func DecryptBlowfishChunk(chunk, key []byte) ([]byte, error) {
	if len(chunk)%8 != 0 {
		return nil, errors.New("chunk size must be a multiple of 8")
	}
	cipherBlock, err := newBlowfishCipher(key)
	if err != nil {
		return nil, err
	}
	mode := cipher.NewCBCDecrypter(cipherBlock, blowfishIV)
	out := make([]byte, len(chunk))
	mode.CryptBlocks(out, chunk)
	return out, nil
}

// DecryptDeezerStream reads an encrypted stream from r, decrypts every 3rd 2048-byte chunk
// (chunks 0, 3, 6, ...) using Blowfish CBC, and writes the decrypted stream to w.
// If maxBytes > 0 and writing the next chunk would exceed maxBytes, reading and writing
// stop immediately and ErrFileTooLarge is returned.
// Any trailing partial chunk (< 2048 bytes) is left unencrypted.
func DecryptDeezerStream(r io.Reader, w io.Writer, key []byte, maxBytes int64) (int64, error) {
	cipherBlock, err := newBlowfishCipher(key)
	if err != nil {
		return 0, err
	}

	const chunkSize = 2048
	buf := make([]byte, chunkSize)
	dec := make([]byte, chunkSize)
	var totalWritten int64
	chunkIndex := 0

	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			if maxBytes > 0 && totalWritten+int64(n) > maxBytes {
				return totalWritten, ErrFileTooLarge
			}

			if n == chunkSize {
				if chunkIndex%3 == 0 {
					mode := cipher.NewCBCDecrypter(cipherBlock, blowfishIV)
					mode.CryptBlocks(dec, buf)
					wn, werr := w.Write(dec)
					totalWritten += int64(wn)
					if werr != nil {
						return totalWritten, werr
					}
				} else {
					wn, werr := w.Write(buf)
					totalWritten += int64(wn)
					if werr != nil {
						return totalWritten, werr
					}
				}
				chunkIndex++
			} else {
				// Trailing partial chunk (< 2048 bytes), written as-is
				wn, werr := w.Write(buf[:n])
				totalWritten += int64(wn)
				if werr != nil {
					return totalWritten, werr
				}
			}
		}

		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return totalWritten, err
		}
	}

	return totalWritten, nil
}
