package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"sync"
	"time"
)

const pkceEntryTTL = 5 * time.Minute

type pkceEntry struct {
	CodeVerifier string
	CreatedAt    time.Time
}

// PKCEStore holds in-flight authorization requests keyed by the OAuth
// `state` parameter, so the callback can retrieve the code_verifier that
// only this backend (never the browser) ever saw.
type PKCEStore struct {
	mu      sync.Mutex
	entries map[string]pkceEntry
}

func NewPKCEStore() *PKCEStore {
	return &PKCEStore{entries: make(map[string]pkceEntry)}
}

func randomURLSafeString(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// NewChallenge generates a fresh state + PKCE (S256) pair and stores the
// verifier under the state for later retrieval in the callback.
func (p *PKCEStore) NewChallenge() (state, codeChallenge string) {
	verifier := randomURLSafeString(32)
	state = randomURLSafeString(16)

	sum := sha256.Sum256([]byte(verifier))
	codeChallenge = base64.RawURLEncoding.EncodeToString(sum[:])

	p.mu.Lock()
	p.entries[state] = pkceEntry{CodeVerifier: verifier, CreatedAt: time.Now()}
	p.mu.Unlock()

	return state, codeChallenge
}

// Consume returns and removes the code_verifier for a state (single use).
func (p *PKCEStore) Consume(state string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.entries[state]
	delete(p.entries, state)
	if !ok {
		return "", errors.New("unknown or already used state")
	}
	if time.Since(entry.CreatedAt) > pkceEntryTTL {
		return "", errors.New("state expired")
	}
	return entry.CodeVerifier, nil
}
