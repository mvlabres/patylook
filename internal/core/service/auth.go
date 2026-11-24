package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

const (
	maxLoginFailures = 5
	loginWindow      = 15 * time.Minute
)

type AuthConfig struct {
	SessionTTL    time.Duration
	AdminCategory string
}

type AuthService struct {
	users    port.UserRepository
	sessions port.SessionStore
	hasher   port.PasswordHasher
	admin    adminRule
	ttl      time.Duration
	now      Clock
	limiter  *loginLimiter
	// dummyHash is verified when the user does not exist, so response time does not reveal valid usernames.
	dummyHash string
}

var _ port.AuthService = (*AuthService)(nil)

func NewAuthService(users port.UserRepository, sessions port.SessionStore, hasher port.PasswordHasher, cfg AuthConfig, now Clock) (*AuthService, error) {
	dummy, err := hasher.Hash("dummy-password-for-timing")
	if err != nil {
		return nil, err
	}
	return &AuthService{
		users: users, sessions: sessions, hasher: hasher,
		admin: adminRule(cfg.AdminCategory), ttl: cfg.SessionTTL, now: now,
		limiter: newLoginLimiter(maxLoginFailures, loginWindow), dummyHash: dummy,
	}, nil
}

func (s *AuthService) Login(ctx context.Context, clientKey, username, password string) (*port.LoginResult, error) {
	username = strings.TrimSpace(username)
	key := clientKey + "|" + strings.ToLower(username)
	now := s.now()
	if s.limiter.Blocked(key, now) {
		return nil, domain.ErrTooManyAttempts
	}

	user, err := s.users.FindByUsername(ctx, username)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		s.hasher.Verify(s.dummyHash, password)
		s.limiter.Fail(key, now)
		return nil, domain.ErrInvalidCredentials
	case err != nil:
		return nil, err
	}
	if !s.hasher.Verify(user.PasswordHash, password) || !user.IsActive {
		s.limiter.Fail(key, now)
		return nil, domain.ErrInvalidCredentials
	}
	s.limiter.Reset(key)

	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	csrf, err := randomToken()
	if err != nil {
		return nil, err
	}
	sess := domain.Session{
		TokenHash: hashToken(token),
		UserID:    user.ID,
		CSRFToken: csrf,
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return nil, err
	}
	return &port.LoginResult{Token: token, Session: sess, User: s.admin.mark(user)}, nil
}

func (s *AuthService) Authenticate(ctx context.Context, token string) (*domain.Session, *domain.User, error) {
	if token == "" {
		return nil, nil, domain.ErrUnauthenticated
	}
	hash := hashToken(token)
	sess, err := s.sessions.Find(ctx, hash)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil, domain.ErrUnauthenticated
		}
		return nil, nil, err
	}
	now := s.now()
	if !sess.ExpiresAt.After(now) {
		_ = s.sessions.Delete(ctx, hash)
		return nil, nil, domain.ErrUnauthenticated
	}
	user, err := s.users.FindByID(ctx, sess.UserID)
	if err != nil || !user.IsActive {
		_ = s.sessions.Delete(ctx, hash)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return nil, nil, err
		}
		return nil, nil, domain.ErrUnauthenticated
	}
	// Sliding expiration, refreshed only once half the TTL has elapsed to limit writes.
	if sess.ExpiresAt.Sub(now) < s.ttl/2 {
		sess.ExpiresAt = now.Add(s.ttl)
		if err := s.sessions.Update(ctx, *sess); err != nil {
			return nil, nil, err
		}
	}
	return sess, s.admin.mark(user), nil
}

func (s *AuthService) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.sessions.Delete(ctx, hashToken(token))
}

func (s *AuthService) SetFlash(ctx context.Context, sess *domain.Session, kind domain.FlashKind, msg string) error {
	sess.Flash = &domain.Flash{Kind: kind, Message: msg}
	return s.sessions.Update(ctx, *sess)
}

func (s *AuthService) PopFlash(ctx context.Context, sess *domain.Session) *domain.Flash {
	f := sess.Flash
	if f == nil {
		return nil
	}
	sess.Flash = nil
	_ = s.sessions.Update(ctx, *sess)
	return f
}

// CleanupExpired removes expired sessions; meant to be called periodically.
func (s *AuthService) CleanupExpired(ctx context.Context) (int, error) {
	return s.sessions.DeleteExpired(ctx, s.now())
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
