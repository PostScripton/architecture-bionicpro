package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
	TokenType        string `json:"token_type"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type KeycloakClient struct {
	InternalBaseURL string
	Realm           string
	ClientID        string
	httpClient      *http.Client
}

func NewKeycloakClient(internalBaseURL, realm, clientID string) *KeycloakClient {
	return &KeycloakClient{
		InternalBaseURL: strings.TrimRight(internalBaseURL, "/"),
		Realm:           realm,
		ClientID:        clientID,
		httpClient:      &http.Client{Timeout: 10 * time.Second},
	}
}

func (k *KeycloakClient) tokenEndpoint() string {
	return fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", k.InternalBaseURL, k.Realm)
}

func (k *KeycloakClient) postForm(values url.Values) (*tokenResponse, error) {
	resp, err := k.httpClient.PostForm(k.tokenEndpoint(), values)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		desc := tr.ErrorDescription
		if desc == "" {
			desc = tr.Error
		}
		return nil, fmt.Errorf("keycloak token endpoint returned %d: %s", resp.StatusCode, desc)
	}
	return &tr, nil
}

// ExchangeCode performs the authorization_code grant with PKCE. No client
// secret is sent - the code_verifier proves this request came from the
// party that originated the /auth/login redirect (this backend).
func (k *KeycloakClient) ExchangeCode(code, codeVerifier, redirectURI string) (*tokenResponse, error) {
	values := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {k.ClientID},
		"code_verifier": {codeVerifier},
	}
	return k.postForm(values)
}

// RefreshTokens performs the refresh_token grant to mint a new access token
// (and, with refresh token rotation enabled in Keycloak, a new refresh
// token) once the current access token has expired.
func (k *KeycloakClient) RefreshTokens(refreshToken string) (*tokenResponse, error) {
	values := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {k.ClientID},
	}
	return k.postForm(values)
}

// Logout revokes the refresh token at Keycloak so it can no longer be used
// to mint new access tokens after the user logs out.
func (k *KeycloakClient) Logout(refreshToken string) error {
	values := url.Values{
		"client_id":     {k.ClientID},
		"refresh_token": {refreshToken},
	}
	endpoint := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/logout", k.InternalBaseURL, k.Realm)
	resp, err := k.httpClient.PostForm(endpoint, values)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// AuthorizeURL builds the browser-facing authorization endpoint URL. It
// uses the public (browser-reachable) Keycloak base URL, which can differ
// from the internal docker-network URL used for server-to-server calls.
func AuthorizeURL(publicBaseURL, realm, clientID, redirectURI, state, codeChallenge string) string {
	base := strings.TrimRight(publicBaseURL, "/")
	q := url.Values{
		"client_id":             {clientID},
		"response_type":         {"code"},
		"scope":                 {"openid"},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}
	return fmt.Sprintf("%s/realms/%s/protocol/openid-connect/auth?%s", base, realm, q.Encode())
}
