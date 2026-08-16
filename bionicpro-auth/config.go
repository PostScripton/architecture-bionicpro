package main

import "os"

// Config holds all runtime settings for bionicpro-auth. Everything is
// injected through the environment so the same binary can run against any
// Keycloak realm / IdP without a rebuild.
type Config struct {
	ListenAddr string

	KeycloakInternalURL string // used server-to-server (docker network)
	KeycloakPublicURL   string // used for browser redirects
	Realm               string
	ClientID            string

	BackendPublicURL string // this service's own externally reachable URL
	FrontendURL      string
	ReportsAPIURL    string

	CookieName   string
	CookieSecure bool

	SessionTTLSeconds int // must be > access token TTL
}

func loadConfig() Config {
	return Config{
		ListenAddr:          getEnv("LISTEN_ADDR", ":8000"),
		KeycloakInternalURL: getEnv("KEYCLOAK_INTERNAL_URL", "http://keycloak:8080"),
		KeycloakPublicURL:   getEnv("KEYCLOAK_PUBLIC_URL", "http://localhost:8080"),
		Realm:               getEnv("KEYCLOAK_REALM", "reports-realm"),
		ClientID:            getEnv("KEYCLOAK_CLIENT_ID", "reports-frontend"),
		BackendPublicURL:    getEnv("BACKEND_PUBLIC_URL", "http://localhost:8000"),
		FrontendURL:         getEnv("FRONTEND_URL", "http://localhost:3000"),
		ReportsAPIURL:       getEnv("REPORTS_API_URL", ""),
		CookieName:          getEnv("COOKIE_NAME", "bionicpro_session"),
		CookieSecure:        getEnv("COOKIE_SECURE", "true") == "true",
		SessionTTLSeconds:   getEnvInt("SESSION_TTL_SECONDS", 900), // 15 min > 2 min access token
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n := 0
	for _, c := range v {
		if c < '0' || c > '9' {
			return fallback
		}
		n = n*10 + int(c-'0')
	}
	return n
}
