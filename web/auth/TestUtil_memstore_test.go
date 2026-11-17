package auth

import (
	"context"
	"sync"
	"time"
)

// MemStore is an in-memory Store for tests.
type MemStore struct {
	mu                    sync.Mutex
	admin                 Admin
	hasAdmin              bool
	lastPasswordAttemptAt time.Time
	now                   func() time.Time
}

// NewMemStore returns an empty in-memory Store.
func NewMemStore() *MemStore {
	return &MemStore{now: time.Now}
}

func (s *MemStore) HasAdmin(context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hasAdmin, nil
}

func (s *MemStore) CreateAdmin(_ context.Context, username, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasAdmin {
		return errAdminExists
	}
	key, salt, err := newSessionSecrets()
	if err != nil {
		return err
	}
	s.hasAdmin = true
	s.admin = Admin{
		Username:     username,
		PasswordHash: passwordHash,
		SessionKey:   key,
		SessionSalt:  salt,
	}
	return nil
}

func (s *MemStore) GetAdmin(context.Context) (Admin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasAdmin {
		return Admin{}, errAdminMissing
	}
	out := s.admin
	out.SessionKey = append([]byte(nil), s.admin.SessionKey...)
	out.SessionSalt = append([]byte(nil), s.admin.SessionSalt...)
	return out, nil
}

func (s *MemStore) ClaimPasswordAttempt(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasAdmin {
		return errAdminMissing
	}
	now := s.now()
	if !s.lastPasswordAttemptAt.IsZero() && now.Sub(s.lastPasswordAttemptAt) < 5*time.Second {
		return errAttemptTooSoon
	}
	s.lastPasswordAttemptAt = now
	return nil
}

func (s *MemStore) UpdatePassword(_ context.Context, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasAdmin {
		return errAdminMissing
	}
	key, salt, err := newSessionSecrets()
	if err != nil {
		return err
	}
	s.admin.PasswordHash = passwordHash
	s.admin.SessionKey = key
	s.admin.SessionSalt = salt
	return nil
}
