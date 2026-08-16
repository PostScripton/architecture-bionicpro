package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// subjectFromAccessToken extracts the "sub" claim from a JWT without
// verifying its signature. Keycloak already authenticated the token at the
// token endpoint over a server-to-server TLS/trusted-network channel; this
// is only used for logging/telemetry, never for authorization decisions.
func subjectFromAccessToken(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var claims struct {
		Subject string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", err
	}
	return claims.Subject, nil
}
