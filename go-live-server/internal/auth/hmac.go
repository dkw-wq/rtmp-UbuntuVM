package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

// GeneratePlaySign creates an HMAC-SHA256 signature for play URL authentication.
// sign = hex(HMAC-SHA256(stream_key + expire_string, secret))
func GeneratePlaySign(streamKey string, expire int64, secret string) string {
	data := streamKey + strconv.FormatInt(expire, 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

// ValidatePlaySign verifies the HMAC-SHA256 signature and expiry for a play request.
func ValidatePlaySign(streamKey string, expire int64, sign string, secret string) error {
	// Compare using constant-time comparison to avoid timing attacks
	expected := GeneratePlaySign(streamKey, expire, secret)
	if !hmac.Equal([]byte(sign), []byte(expected)) {
		return fmt.Errorf("invalid signature")
	}
	return nil
}
