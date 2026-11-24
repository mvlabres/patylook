// Package memory holds in-process adapters. Sessions live here so the existing
// database does not need extra tables.
package memory

import (
	"context"
	"sync"
	"time"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

// SessionStore keeps sessions in memory. They are lost when the process restarts,
// which simply forces users to sign in again.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]domain.Session
}

var _ port.SessionStore = (*SessionStore)(nil)

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]domain.Session)}
}

func (s *SessionStore) Create(_ context.Context, sess domain.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.TokenHash] = cloneSession(sess)
	return nil
}

func (s *SessionStore) Find(_ context.Context, tokenHash string) (*domain.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[tokenHash]
	if !ok {
		return nil, domain.ErrNotFound
	}
	c := cloneSession(sess)
	return &c, nil
}

func (s *SessionStore) Update(_ context.Context, sess domain.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[sess.TokenHash]; !ok {
		return domain.ErrNotFound
	}
	s.sessions[sess.TokenHash] = cloneSession(sess)
	return nil
}

func (s *SessionStore) Delete(_ context.Context, tokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, tokenHash)
	return nil
}

func (s *SessionStore) DeleteByUser(_ context.Context, userID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.sessions {
		if v.UserID == userID {
			delete(s.sessions, k)
		}
	}
	return nil
}

func (s *SessionStore) DeleteExpired(_ context.Context, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for k, v := range s.sessions {
		if !v.ExpiresAt.After(now) {
			delete(s.sessions, k)
			n++
		}
	}
	return n, nil
}

func cloneSession(s domain.Session) domain.Session {
	if s.Flash != nil {
		f := *s.Flash
		s.Flash = &f
	}
	return s
}
