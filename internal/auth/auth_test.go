package auth_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"umirhack/internal/auth"
	"umirhack/internal/auth/authtest"
)

func newSvc(now *time.Time) (*auth.Service, *authtest.Mem) {
	repo := authtest.New()
	return auth.New(repo, auth.Options{Required: true, SessionTTL: time.Hour, Cost: bcrypt.MinCost,
		Now: func() time.Time { return *now }}), repo
}

func TestDefaultBcryptCostIs12(t *testing.T) {
	// Сервис без явной стоимости использует 12.
	svc := auth.New(authtest.New(), auth.Options{})
	u, err := svc.CreateUser(context.Background(), "x", "password1", auth.Viewer)
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := bcrypt.Cost([]byte(u.PasswordHash)); c != 12 {
		t.Fatalf("service cost = %d", c)
	}
}

func TestTokenIs32BytesAndOnlyHashStored(t *testing.T) {
	tok, err := auth.NewToken()
	if err != nil || len(tok) != 43 { // 32 байта в base64url без заполнения
		t.Fatalf("token %q: %v", tok, err)
	}
	other, _ := auth.NewToken()
	if tok == other {
		t.Fatal("tokens must be random")
	}
	if len(auth.HashToken(tok)) != 32 {
		t.Fatal("SHA-256 expected")
	}

	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	svc, repo := newSvc(&now)
	ctx := context.Background()
	_, _ = svc.CreateUser(ctx, "ann", "password1", auth.Operator)
	token, _, err := svc.Login(ctx, "ann", "password1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SessionUser(ctx, []byte(token), now); err == nil {
		t.Fatal("raw token must not be a key in storage")
	}
	if _, err := repo.SessionUser(ctx, auth.HashToken(token), now); err != nil {
		t.Fatalf("hash lookup: %v", err)
	}
}

func TestSessionExpiresAfterTTL(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	svc, _ := newSvc(&now)
	ctx := context.Background()
	_, _ = svc.CreateUser(ctx, "ann", "password1", auth.Viewer)
	token, _, _ := svc.Login(ctx, "ann", "password1")

	now = now.Add(time.Hour - time.Nanosecond)
	if _, err := svc.Authenticate(ctx, token, ""); err != nil {
		t.Fatalf("just before TTL: %v", err)
	}
	now = now.Add(time.Nanosecond)
	if _, err := svc.Authenticate(ctx, token, ""); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("at TTL: %v", err)
	}
}

func TestLoginRateLimit(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	svc, _ := newSvc(&now)
	ctx := context.Background()
	_, _ = svc.CreateUser(ctx, "ann", "password1", auth.Viewer)

	for i := 1; i <= 5; i++ {
		if _, _, err := svc.Login(ctx, "ann", "wrong"); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	// Шестая попытка — 429, даже с верным паролем.
	if _, _, err := svc.Login(ctx, "ann", "password1"); !errors.Is(err, auth.ErrTooManyAttempts) {
		t.Fatalf("6th: %v", err)
	}
	// Другой логин не затронут.
	if _, _, err := svc.Login(ctx, "bob", "x"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("other login: %v", err)
	}
	// Через 15 минут после последней неудачи окно очищается.
	now = now.Add(15 * time.Minute)
	if _, _, err := svc.Login(ctx, "ann", "password1"); err != nil {
		t.Fatalf("after window: %v", err)
	}
}

func TestUnknownLoginAndWrongPasswordIndistinguishable(t *testing.T) {
	now := time.Now()
	svc, _ := newSvc(&now)
	ctx := context.Background()
	_, _ = svc.CreateUser(ctx, "ann", "password1", auth.Viewer)
	_, _, e1 := svc.Login(ctx, "ann", "bad")
	_, _, e2 := svc.Login(ctx, "ghost", "bad")
	if !errors.Is(e1, auth.ErrInvalidCredentials) || e1 != e2 {
		t.Fatalf("errors differ: %v / %v", e1, e2)
	}
}

func TestBootstrapCreatesAdminOnlyOnEmptyTable(t *testing.T) {
	now := time.Now()
	svc, repo := newSvc(&now)
	ctx := context.Background()
	if err := svc.Bootstrap(ctx, "root", "rootpass1"); err != nil {
		t.Fatal(err)
	}
	u, err := repo.UserByLogin(ctx, "root")
	if err != nil || u.Role != auth.Admin {
		t.Fatalf("admin: %v %+v", err, u)
	}
	if err := svc.Bootstrap(ctx, "second", "secondpass"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UserByLogin(ctx, "second"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatal("bootstrap must not add users to a non-empty table")
	}
}

func TestRoleAllows(t *testing.T) {
	cases := []struct {
		have, need auth.Role
		want       bool
	}{
		{auth.Viewer, auth.Viewer, true}, {auth.Viewer, auth.Operator, false},
		{auth.Operator, auth.Viewer, true}, {auth.Operator, auth.Admin, false},
		{auth.Admin, auth.Operator, true}, {"", auth.Viewer, false}, {"root", auth.Viewer, false},
	}
	for _, c := range cases {
		if got := c.have.Allows(c.need); got != c.want {
			t.Errorf("%q.Allows(%q) = %v", c.have, c.need, got)
		}
	}
}

// Параллельная пачка не обходит лимит: проверок пароля не больше пяти.
func TestLoginRateLimitHoldsUnderConcurrency(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	svc, _ := newSvc(&now)
	ctx := context.Background()
	_, _ = svc.CreateUser(ctx, "bob", "password1", auth.Viewer)

	var wg sync.WaitGroup
	var checked, blocked int32
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := svc.Login(ctx, "bob", "wrong")
			switch {
			case errors.Is(err, auth.ErrInvalidCredentials):
				atomic.AddInt32(&checked, 1)
			case errors.Is(err, auth.ErrTooManyAttempts):
				atomic.AddInt32(&blocked, 1)
			}
		}()
	}
	wg.Wait()
	if checked != 5 || blocked != 45 {
		t.Fatalf("checked=%d blocked=%d, want 5 and 45", checked, blocked)
	}
}

func TestChangePasswordKeepsCurrentSessionOnly(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	svc, repo := newSvc(&now)
	ctx := context.Background()
	u, _ := svc.CreateUser(ctx, "ann", "password1", auth.Operator)
	cur, _, _ := svc.Login(ctx, "ann", "password1")
	other, _, _ := svc.Login(ctx, "ann", "password1")

	if err := svc.ChangePassword(ctx, u.ID, cur, "wrong", "password2"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("wrong old: %v", err)
	}
	if err := svc.ChangePassword(ctx, u.ID, cur, "password1", "password2"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, cur, ""); err != nil {
		t.Errorf("current session: %v", err)
	}
	if _, err := svc.Authenticate(ctx, other, ""); err == nil {
		t.Error("other session must be gone")
	}
	if repo.SessionCount() != 1 {
		t.Errorf("sessions: %d", repo.SessionCount())
	}
	if _, _, err := svc.Login(ctx, "ann", "password1"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("old password still works: %v", err)
	}
	if _, _, err := svc.Login(ctx, "ann", "password2"); err != nil {
		t.Errorf("new password: %v", err)
	}
	h, _ := repo.UserByID(ctx, u.ID)
	if err := bcrypt.CompareHashAndPassword([]byte(h.PasswordHash), []byte("password2")); err != nil {
		t.Errorf("hash: %v", err)
	}
}

func TestChangePasswordIsRateLimitedSeparatelyFromLogin(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	svc, _ := newSvc(&now)
	ctx := context.Background()
	u, _ := svc.CreateUser(ctx, "ann", "password1", auth.Operator)
	for i := 0; i < 5; i++ {
		if err := svc.ChangePassword(ctx, u.ID, "", "wrong", "password2"); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if err := svc.ChangePassword(ctx, u.ID, "", "password1", "password2"); !errors.Is(err, auth.ErrTooManyAttempts) {
		t.Fatalf("6th: %v", err)
	}
	if _, _, err := svc.Login(ctx, "ann", "password1"); err != nil {
		t.Fatalf("login must not be blocked by password attempts: %v", err)
	}
}
