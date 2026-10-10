// Package authtest — хранилище auth.Repo в памяти для тестов.
package authtest

import (
	"context"
	"sort"
	"sync"
	"time"

	"umirhack/internal/auth"
)

type session struct {
	userID  int64
	expires time.Time
}

// Mem — потокобезопасная реализация auth.Repo.
type Mem struct {
	mu       sync.Mutex
	seq      int64
	users    map[int64]auth.User
	sessions map[string]session
	keys     map[int64]auth.APIKey
	keyHash  map[string]int64
}

// New создаёт пустое хранилище.
func New() *Mem {
	return &Mem{users: map[int64]auth.User{}, sessions: map[string]session{}, keys: map[int64]auth.APIKey{}, keyHash: map[string]int64{}}
}

var _ auth.Repo = (*Mem)(nil)

// SessionCount — число сессий в хранилище (для проверок в тестах).
func (m *Mem) SessionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

func (m *Mem) CountUsers(context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.users), nil
}

func (m *Mem) CreateUser(_ context.Context, login, hash string, role auth.Role) (auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Login == login {
			return auth.User{}, auth.ErrExists
		}
	}
	m.seq++
	u := auth.User{ID: m.seq, Login: login, Role: role, PasswordHash: hash, CreatedAt: time.Now()}
	m.users[u.ID] = u
	return u, nil
}

func (m *Mem) UserByLogin(_ context.Context, login string) (auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Login == login {
			return u, nil
		}
	}
	return auth.User{}, auth.ErrNotFound
}

func (m *Mem) ListUsers(context.Context) ([]auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]auth.User, 0, len(m.users))
	for _, u := range m.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Mem) DeleteUser(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return auth.ErrNotFound
	}
	if u.Role == auth.Admin {
		n := 0
		for _, x := range m.users {
			if x.Role == auth.Admin {
				n++
			}
		}
		if n <= 1 {
			return auth.ErrLastAdmin
		}
	}
	delete(m.users, id)
	for h, s := range m.sessions {
		if s.userID == id {
			delete(m.sessions, h)
		}
	}
	return nil
}

func (m *Mem) CreateSession(_ context.Context, h []byte, userID int64, exp time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[string(h)] = session{userID, exp}
	return nil
}

func (m *Mem) SessionUser(_ context.Context, h []byte, now time.Time) (auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[string(h)]
	if !ok || !now.Before(s.expires) {
		return auth.User{}, auth.ErrNotFound
	}
	u, ok := m.users[s.userID]
	if !ok {
		return auth.User{}, auth.ErrNotFound
	}
	return u, nil
}

func (m *Mem) DeleteSession(_ context.Context, h []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, string(h))
	return nil
}

func (m *Mem) DeleteExpiredSessions(_ context.Context, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for h, s := range m.sessions {
		if !now.Before(s.expires) {
			delete(m.sessions, h)
		}
	}
	return nil
}

func (m *Mem) CreateAPIKey(_ context.Context, name string, h []byte, role auth.Role) (auth.APIKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, dup := m.keyHash[string(h)]; dup {
		return auth.APIKey{}, auth.ErrExists
	}
	m.seq++
	k := auth.APIKey{ID: m.seq, Name: name, Role: role, CreatedAt: time.Now()}
	m.keys[k.ID] = k
	m.keyHash[string(h)] = k.ID
	return k, nil
}

func (m *Mem) APIKeyByHash(_ context.Context, h []byte) (auth.APIKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.keyHash[string(h)]
	if !ok {
		return auth.APIKey{}, auth.ErrNotFound
	}
	return m.keys[id], nil
}

func (m *Mem) ListAPIKeys(context.Context) ([]auth.APIKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]auth.APIKey, 0, len(m.keys))
	for _, k := range m.keys {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Mem) DeleteAPIKey(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.keys[id]; !ok {
		return auth.ErrNotFound
	}
	delete(m.keys, id)
	for h, kid := range m.keyHash {
		if kid == id {
			delete(m.keyHash, h)
		}
	}
	return nil
}
