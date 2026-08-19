package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"
)

// session holds tokens for one authenticated user. AccessToken/RefreshToken
// are stored AES-GCM encrypted with a key that only ever lives in this
// process' memory and is never persisted, so a memory dump of a swapped-out
// page or a heap snapshot still doesn't yield plaintext tokens.
type session struct {
	Subject          string
	EncAccessToken   []byte
	EncRefreshToken  []byte
	AccessExpiresAt  time.Time
	SessionExpiresAt time.Time
}

var errSessionNotFound = errors.New("session not found")
var errSessionExpired = errors.New("session expired")

// SessionStore is an in-memory, mutex-protected store keyed by session id.
// In production this is swapped for a distributed cache (e.g. Redis) behind
// the same interface - the HTTP handlers never touch the map directly.
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]*session
	gcm      cipher.AEAD
}

func NewSessionStore() *SessionStore {
	key := make([]byte, 32) // AES-256
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		panic("cannot generate session encryption key: " + err.Error())
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return &SessionStore{
		sessions: make(map[string]*session),
		gcm:      gcm,
	}
}

func (s *SessionStore) encrypt(plaintext string) []byte {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		panic(err)
	}
	return s.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
}

func (s *SessionStore) decrypt(ciphertext []byte) (string, error) {
	nonceSize := s.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	nonce, data := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := s.gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func newSessionID() string {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// Create stores a brand new session bound to freshly minted tokens and
// returns its id.
func (s *SessionStore) Create(subject, accessToken, refreshToken string, accessExpiresAt time.Time, sessionTTL time.Duration) string {
	id := newSessionID()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = &session{
		Subject:          subject,
		EncAccessToken:   s.encrypt(accessToken),
		EncRefreshToken:  s.encrypt(refreshToken),
		AccessExpiresAt:  accessExpiresAt,
		SessionExpiresAt: time.Now().Add(sessionTTL),
	}
	return id
}

// Get returns the decrypted tokens for a session id.
func (s *SessionStore) Get(id string) (subject, accessToken, refreshToken string, accessExpiresAt time.Time, err error) {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return "", "", "", time.Time{}, errSessionNotFound
	}
	if time.Now().After(sess.SessionExpiresAt) {
		s.Delete(id)
		return "", "", "", time.Time{}, errSessionExpired
	}
	at, err := s.decrypt(sess.EncAccessToken)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	rt, err := s.decrypt(sess.EncRefreshToken)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	return sess.Subject, at, rt, sess.AccessExpiresAt, nil
}

// UpdateTokens rewrites the tokens of an existing session in place (used
// after a refresh_token grant), keeping the same session id.
func (s *SessionStore) UpdateTokens(id, accessToken, refreshToken string, accessExpiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return errSessionNotFound
	}
	sess.EncAccessToken = s.encrypt(accessToken)
	sess.EncRefreshToken = s.encrypt(refreshToken)
	sess.AccessExpiresAt = accessExpiresAt
	return nil
}

// Rotate creates a new session id carrying over the same tokens/subject and
// remaining session lifetime, then deletes the old id. This defeats session
// fixation: an id learned before/around login stops being valid on the very
// next authenticated request.
func (s *SessionStore) Rotate(oldID string) (newID string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[oldID]
	if !ok {
		return "", errSessionNotFound
	}
	newID = newSessionID()
	s.sessions[newID] = sess
	delete(s.sessions, oldID)
	return newID, nil
}

func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// SessionTTLFor returns the session's remaining lifetime, used to keep the
// cookie's Max-Age in sync after a rotation.
func (s *SessionStore) TTLFor(id string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return 0
	}
	return time.Until(sess.SessionExpiresAt)
}
