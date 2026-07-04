package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

// GeneratePlaySign creates an HMAC-SHA256 signature for play URL authentication.
// sign = hex(HMAC-SHA256(stream_key + expire_string, secret))
func GeneratePlaySign(streamKey string, expire int64, secret string) string {
	return generateSign(streamKey, expire, secret)
}

// ValidatePlaySign verifies the HMAC-SHA256 signature for a play request.
func ValidatePlaySign(streamKey string, expire int64, sign string, secret string) error {
	return validateSign(streamKey, expire, sign, secret)
}

// GeneratePushSign creates a legacy HMAC-SHA256 signature for RTMP push URL authentication.
// sign = hex(HMAC-SHA256(stream_key + expire_string, push_secret))
func GeneratePushSign(streamKey string, expire int64, secret string) string {
	return generateSign(streamKey, expire, secret)
}

// ValidatePushSign verifies a legacy HMAC-SHA256 signature for a push request.
func ValidatePushSign(streamKey string, expire int64, sign string, secret string) error {
	return validateSign(streamKey, expire, sign, secret)
}

// GeneratePublishToken creates a long-lived random token for RTMP publishers.
func GeneratePublishToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate publish token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// ValidatePublishToken verifies a provided publish token against the stored token.
func ValidatePublishToken(expected string, provided string) error {
	if expected == "" {
		return fmt.Errorf("publish token is not configured")
	}
	if provided == "" {
		return fmt.Errorf("publish token is required")
	}
	if !hmac.Equal([]byte(expected), []byte(provided)) {
		return fmt.Errorf("invalid publish token")
	}
	return nil
}

func generateSign(streamKey string, expire int64, secret string) string {
	data := streamKey + strconv.FormatInt(expire, 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

func validateSign(streamKey string, expire int64, sign string, secret string) error {
	expected := generateSign(streamKey, expire, secret)
	if !hmac.Equal([]byte(sign), []byte(expected)) {
		return fmt.Errorf("invalid signature")
	}
	return nil
}
