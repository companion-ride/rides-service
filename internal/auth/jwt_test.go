package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret-key-for-unit-tests"

// helper to create a signed token string
func makeToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return s
}

func TestParseAccessToken_Valid(t *testing.T) {
	tokenStr := makeToken(t, jwt.MapClaims{
		"sub":  "550e8400-e29b-41d4-a716-446655440000",
		"role": "driver",
		"type": "access",
		"exp":  jwt.NewNumericDate(time.Now().Add(30 * time.Minute)),
		"iat":  jwt.NewNumericDate(time.Now()),
	})

	claims, err := ParseAccessToken(tokenStr, testSecret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.Subject != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("got subject %q, want UUID", claims.Subject)
	}
	if claims.Role != "driver" {
		t.Errorf("got role %q, want driver", claims.Role)
	}
}

func TestParseAccessToken_Expired(t *testing.T) {
	tokenStr := makeToken(t, jwt.MapClaims{
		"sub":  "550e8400-e29b-41d4-a716-446655440000",
		"role": "passenger",
		"type": "access",
		"exp":  jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
		"iat":  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
	})

	_, err := ParseAccessToken(tokenStr, testSecret)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
	if err.Error() != "token expired" {
		t.Errorf("got error %q, want %q", err.Error(), "token expired")
	}
}

func TestParseAccessToken_WrongType(t *testing.T) {
	tokenStr := makeToken(t, jwt.MapClaims{
		"sub":  "550e8400-e29b-41d4-a716-446655440000",
		"role": "passenger",
		"type": "refresh",
		"exp":  jwt.NewNumericDate(time.Now().Add(30 * time.Minute)),
		"iat":  jwt.NewNumericDate(time.Now()),
	})

	_, err := ParseAccessToken(tokenStr, testSecret)
	if err == nil {
		t.Fatal("expected error for refresh token")
	}
	if err.Error() != "invalid token type" {
		t.Errorf("got error %q, want %q", err.Error(), "invalid token type")
	}
}

func TestParseAccessToken_WrongSecret(t *testing.T) {
	tokenStr := makeToken(t, jwt.MapClaims{
		"sub":  "550e8400-e29b-41d4-a716-446655440000",
		"role": "driver",
		"type": "access",
		"exp":  jwt.NewNumericDate(time.Now().Add(30 * time.Minute)),
		"iat":  jwt.NewNumericDate(time.Now()),
	})

	_, err := ParseAccessToken(tokenStr, "wrong-secret")
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
	if err.Error() != "invalid token" {
		t.Errorf("got error %q, want %q", err.Error(), "invalid token")
	}
}

func TestParseAccessToken_Malformed(t *testing.T) {
	_, err := ParseAccessToken("not.a.jwt", testSecret)
	if err == nil {
		t.Fatal("expected error for malformed token")
	}
	if err.Error() != "invalid token" {
		t.Errorf("got error %q, want %q", err.Error(), "invalid token")
	}
}
