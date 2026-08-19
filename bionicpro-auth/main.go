package main

import (
	"log"
	"net/http"
)

func main() {
	cfg := loadConfig()
	srv := NewServer(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/login", srv.handleLogin)
	mux.HandleFunc("/auth/callback", srv.handleCallback)
	mux.HandleFunc("/auth/logout", srv.handleLogout)
	mux.HandleFunc("/auth/session", srv.requireSession(srv.handleSessionInfo))
	mux.HandleFunc("/reports", srv.requireSession(srv.handleReports))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := srv.corsMiddleware(mux)

	log.Printf("bionicpro-auth listening on %s (realm=%s client=%s)", cfg.ListenAddr, cfg.Realm, cfg.ClientID)
	if err := http.ListenAndServe(cfg.ListenAddr, handler); err != nil {
		log.Fatal(err)
	}
}
