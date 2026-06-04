package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// GeneratePushToken creates a JWT for RTMP push authentication.
// Claims: stream_key, exp (now + expiry).
func GeneratePushToken(streamKey string, secret string, expiry time.Duration) (string, error) {
	return generate(jwt.MapClaims{
		"stream_key": streamKey,
		"exp":        time.Now().Add(expiry).Unix(),
	}, secret)
}

// GenerateAdminToken creates a JWT for admin API access.
// Claims: username, role, exp.
func GenerateAdminToken(username string, secret string, expiry time.Duration) (string, error) {
	return generate(jwt.MapClaims{
		"username": username,
		"role":     "admin",
		"exp":      time.Now().Add(expiry).Unix(),
	}, secret)
}

func generate(claims jwt.Claims, secret string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

// ValidateToken parses and validates a JWT token string.
// Returns the claims on success, or an error if the token is invalid or expired.
func ValidateToken(tokenString string, secret string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, fmt.Errorf("invalid token")
}
