package auth

import (
	"errors"

	"github.com/golang-jwt/jwt/v5"
)

// Claims represents the JWT claims from tokens issued by auth-service.
// The "sub" claim is accessed via the embedded RegisteredClaims.Subject field.
type Claims struct {
	Role string `json:"role"`
	Type string `json:"type"`
	jwt.RegisteredClaims
}

// ParseAccessToken validates a JWT string and returns its claims.
// It enforces HS256 signing and rejects non-access tokens.
func ParseAccessToken(tokenString, secret string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, errors.New("token expired")
		}
		return nil, errors.New("invalid token")
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	if claims.Type != "access" {
		return nil, errors.New("invalid token type")
	}
	return claims, nil
}
