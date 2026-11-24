package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"patylook/internal/adapter/memory"
	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

// ---- fakes (embedding the port makes any unexpected call panic loudly) ----

type fakeHasher struct{}

func (fakeHasher) Hash(p string) (string, error) { return "hash:" + p, nil }
func (fakeHasher) Verify(h, p string) bool       { return h == "hash:"+p }

type fakeUsers struct {
	port.UserRepository
	byName map[string]*domain.User
	byID   map[int]*domain.User
}

func newFakeUsers(users ...*domain.User) *fakeUsers {
	f := &fakeUsers{byName: map[string]*domain.User{}, byID: map[int]*domain.User{}}
	for _, u := range users {
		f.byName[u.Username] = u
		f.byID[u.ID] = u
	}
	return f
}

func (f *fakeUsers) FindByUsername(_ context.Context, name string) (*domain.User, error) {
	if u, ok := f.byName[name]; ok {
		c := *u
		return &c, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeUsers) FindByID(_ context.Context, id int) (*domain.User, error) {
	if u, ok := f.byID[id]; ok {
		c := *u
		return &c, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeUsers) UpdatePassword(_ context.Context, id int, hash string) error {
	f.byID[id].PasswordHash = hash
	return nil
}

func (f *fakeUsers) SetActive(_ context.Context, id int, active bool) error {
	f.byID[id].IsActive = active
	return nil
}

type fixedClock struct{ t time.Time }

func (c *fixedClock) now() time.Time { return c.t }

var testLoc = time.FixedZone("BRT", -3*3600)

func newClock() *fixedClock {
	return &fixedClock{t: time.Date(2026, 11, 24, 10, 0, 0, 0, testLoc)}
}

// ---- auth ----

func newAuth(t *testing.T, clock *fixedClock, users ...*domain.User) (*AuthService, *memory.SessionStore) {
	t.Helper()
	sessions := memory.NewSessionStore()
	auth, err := NewAuthService(newFakeUsers(users...), sessions, fakeHasher{},
		AuthConfig{SessionTTL: time.Hour, AdminCategory: "admin"}, clock.now)
	if err != nil {
		t.Fatal(err)
	}
	return auth, sessions
}

func activeUser() *domain.User {
	return &domain.User{ID: 1, Username: "ana", PasswordHash: "hash:secret123", IsActive: true, CategoryName: "admin", CategoryID: 1}
}

func TestLoginAndAuthenticate(t *testing.T) {
	clock := newClock()
	auth, _ := newAuth(t, clock, activeUser())
	ctx := context.Background()

	res, err := auth.Login(ctx, "1.2.3.4", "ana", "secret123")
	if err != nil {
		t.Fatal(err)
	}
	if res.Token == "" || res.Session.CSRFToken == "" || !res.User.Admin {
		t.Fatalf("unexpected login result: %+v", res)
	}
	if res.Session.TokenHash == res.Token {
		t.Error("session store must hold a hash, not the raw token")
	}

	sess, user, err := auth.Authenticate(ctx, res.Token)
	if err != nil || user.Username != "ana" || sess.CSRFToken != res.Session.CSRFToken {
		t.Fatalf("Authenticate: %v %v %v", sess, user, err)
	}

	if _, _, err := auth.Authenticate(ctx, "forged"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("forged token: %v", err)
	}
}

func TestLoginFailures(t *testing.T) {
	clock := newClock()
	inactive := &domain.User{ID: 2, Username: "off", PasswordHash: "hash:secret123", IsActive: false}
	auth, _ := newAuth(t, clock, activeUser(), inactive)
	ctx := context.Background()

	for _, c := range []struct{ user, pass string }{
		{"ana", "wrong"}, {"nobody", "secret123"}, {"off", "secret123"},
	} {
		if _, err := auth.Login(ctx, "ip", c.user, c.pass); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Errorf("Login(%s): %v", c.user, err)
		}
	}
}

func TestLoginThrottling(t *testing.T) {
	clock := newClock()
	auth, _ := newAuth(t, clock, activeUser())
	ctx := context.Background()

	for i := 0; i < maxLoginFailures; i++ {
		_, _ = auth.Login(ctx, "ip", "ana", "wrong")
	}
	// Even the right password is refused while blocked.
	if _, err := auth.Login(ctx, "ip", "ana", "secret123"); !errors.Is(err, domain.ErrTooManyAttempts) {
		t.Fatalf("expected throttling, got %v", err)
	}
	// A different client is unaffected.
	if _, err := auth.Login(ctx, "other-ip", "ana", "secret123"); err != nil {
		t.Errorf("other client blocked: %v", err)
	}
	// The block lapses after the window.
	clock.t = clock.t.Add(loginWindow + time.Second)
	if _, err := auth.Login(ctx, "ip", "ana", "secret123"); err != nil {
		t.Errorf("still blocked after window: %v", err)
	}
}

func TestSessionExpiryAndDeactivation(t *testing.T) {
	clock := newClock()
	users := newFakeUsers(activeUser())
	sessions := memory.NewSessionStore()
	auth, _ := NewAuthService(users, sessions, fakeHasher{}, AuthConfig{SessionTTL: time.Hour, AdminCategory: "admin"}, clock.now)
	ctx := context.Background()

	res, _ := auth.Login(ctx, "ip", "ana", "secret123")

	// Deactivated users lose access immediately.
	users.byID[1].IsActive = false
	if _, _, err := auth.Authenticate(ctx, res.Token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("deactivated user kept access: %v", err)
	}

	users.byID[1].IsActive = true
	res, _ = auth.Login(ctx, "ip", "ana", "secret123")
	clock.t = clock.t.Add(2 * time.Hour)
	if _, _, err := auth.Authenticate(ctx, res.Token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("expired session accepted: %v", err)
	}
}

func TestSessionSlidingExpiration(t *testing.T) {
	clock := newClock()
	auth, sessions := newAuth(t, clock, activeUser())
	ctx := context.Background()
	res, _ := auth.Login(ctx, "ip", "ana", "secret123")

	clock.t = clock.t.Add(40 * time.Minute) // past half the TTL
	if _, _, err := auth.Authenticate(ctx, res.Token); err != nil {
		t.Fatal(err)
	}
	stored, _ := sessions.Find(ctx, res.Session.TokenHash)
	if want := clock.t.Add(time.Hour); !stored.ExpiresAt.Equal(want) {
		t.Errorf("expiry not extended: %v, want %v", stored.ExpiresAt, want)
	}
}

func TestLogoutAndFlash(t *testing.T) {
	clock := newClock()
	auth, _ := newAuth(t, clock, activeUser())
	ctx := context.Background()
	res, _ := auth.Login(ctx, "ip", "ana", "secret123")

	sess, _, _ := auth.Authenticate(ctx, res.Token)
	if err := auth.SetFlash(ctx, sess, domain.FlashSuccess, "ok"); err != nil {
		t.Fatal(err)
	}
	sess, _, _ = auth.Authenticate(ctx, res.Token)
	if f := auth.PopFlash(ctx, sess); f == nil || f.Message != "ok" {
		t.Fatalf("flash = %+v", f)
	}
	sess, _, _ = auth.Authenticate(ctx, res.Token)
	if f := auth.PopFlash(ctx, sess); f != nil {
		t.Errorf("flash should be consumed, got %+v", f)
	}

	if err := auth.Logout(ctx, res.Token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := auth.Authenticate(ctx, res.Token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("session survived logout: %v", err)
	}
}

// ---- user service ----

func TestUserServicePermissions(t *testing.T) {
	admin := &domain.User{ID: 1, Admin: true}
	operator := &domain.User{ID: 2}
	svc := NewUserService(newFakeUsers(&domain.User{ID: 1, IsActive: true}, &domain.User{ID: 2, IsActive: true}),
		memory.NewSessionStore(), fakeHasher{}, "admin")
	ctx := context.Background()

	if _, err := svc.List(ctx, operator, 1, ""); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("operator listed users: %v", err)
	}
	if err := svc.SetActive(ctx, operator, 1, false); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("operator deactivated user: %v", err)
	}
	if err := svc.ResetPassword(ctx, operator, 1, "newpassword1"); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("operator reset password: %v", err)
	}
	// An admin cannot deactivate themselves.
	err := svc.SetActive(ctx, admin, 1, false)
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("self-deactivation: %v", err)
	}
}

func TestChangeOwnPassword(t *testing.T) {
	users := newFakeUsers(&domain.User{ID: 1, Username: "ana", PasswordHash: "hash:oldpassword", IsActive: true})
	sessions := memory.NewSessionStore()
	svc := NewUserService(users, sessions, fakeHasher{}, "admin")
	ctx := context.Background()
	actor := &domain.User{ID: 1}

	if err := svc.ChangeOwnPassword(ctx, actor, "wrong", "newpassword1"); err == nil {
		t.Error("wrong current password accepted")
	}
	if err := svc.ChangeOwnPassword(ctx, actor, "oldpassword", "short"); err == nil {
		t.Error("weak new password accepted")
	}
	_ = sessions.Create(ctx, domain.Session{TokenHash: "t", UserID: 1, ExpiresAt: time.Now().Add(time.Hour)})
	if err := svc.ChangeOwnPassword(ctx, actor, "oldpassword", "newpassword1"); err != nil {
		t.Fatal(err)
	}
	if users.byID[1].PasswordHash != "hash:newpassword1" {
		t.Error("password not updated")
	}
	if _, err := sessions.Find(ctx, "t"); !errors.Is(err, domain.ErrNotFound) {
		t.Error("sessions should be revoked after a password change")
	}
}
