package auth

import (
	"context"
	"crypto/rand"
	"errors"

	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

const (
	sessionKeyBytes  = 32
	sessionSaltBytes = 16
)

var (
	errAdminExists    = errors.New("admin already configured")
	errAdminMissing   = errors.New("admin not configured")
	errAttemptTooSoon = errors.New("password attempt too soon")
)

// Admin holds the singleton admin credentials and session signing material.
type Admin struct {
	Username     string
	PasswordHash string
	SessionKey   []byte
	SessionSalt  []byte
}

// Store persists the singleton admin row (no per-session table).
type Store interface {
	HasAdmin(ctx context.Context) (bool, error)
	CreateAdmin(ctx context.Context, username, passwordHash string) error
	GetAdmin(ctx context.Context) (Admin, error)
	ClaimPasswordAttempt(ctx context.Context) error
	UpdatePassword(ctx context.Context, passwordHash string) error
}

type dbStore struct {
	db *harmonydb.DB
}

// NewDBStore returns a Store backed by HarmonyDB.
func NewDBStore(db *harmonydb.DB) Store {
	return &dbStore{db: db}
}

// HasAdmin reports whether the singleton gui_admin row exists.
func (s *dbStore) HasAdmin(ctx context.Context) (bool, error) {
	var exists bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM gui_admin)`).Scan(&exists)
	return exists, err
}

// newSessionSecrets returns a random signing key and salt. Caller inserts them.
func newSessionSecrets() (key, salt []byte, err error) {
	key = make([]byte, sessionKeyBytes)
	salt = make([]byte, sessionSaltBytes)
	if _, err = rand.Read(key); err != nil {
		return nil, nil, err
	}
	if _, err = rand.Read(salt); err != nil {
		return nil, nil, err
	}
	return key, salt, nil
}

// CreateAdmin inserts the singleton admin with fresh session signing secrets.
func (s *dbStore) CreateAdmin(ctx context.Context, username, passwordHash string) error {
	key, salt, err := newSessionSecrets()
	if err != nil {
		return xerrors.Errorf("session secrets: %w", err)
	}
	n, err := s.db.Exec(ctx, `
		INSERT INTO gui_admin (id, username, password_hash, session_key, session_salt)
		VALUES (TRUE, $1, $2, $3, $4)
		ON CONFLICT (id) DO NOTHING`, username, passwordHash, key, salt)
	if err != nil {
		return xerrors.Errorf("insert gui_admin: %w", err)
	}
	if n == 0 {
		return errAdminExists
	}
	return nil
}

// GetAdmin returns the configured admin row including session signing material.
func (s *dbStore) GetAdmin(ctx context.Context) (Admin, error) {
	var a Admin
	err := s.db.QueryRow(ctx, `
		SELECT username, password_hash, session_key, session_salt
		FROM gui_admin WHERE id = TRUE`).Scan(&a.Username, &a.PasswordHash, &a.SessionKey, &a.SessionSalt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Admin{}, errAdminMissing
		}
		return Admin{}, err
	}
	return a, nil
}

// ClaimPasswordAttempt takes the cluster-wide 5-second password-check slot.
// Returns errAttemptTooSoon when another attempt happened too recently.
func (s *dbStore) ClaimPasswordAttempt(ctx context.Context) error {
	n, err := s.db.Exec(ctx, `
		UPDATE gui_admin
		SET last_password_attempt_at = NOW()
		WHERE id = TRUE
		  AND (last_password_attempt_at IS NULL
		       OR last_password_attempt_at <= NOW() - INTERVAL '5 seconds')`)
	if err != nil {
		return xerrors.Errorf("claim password attempt: %w", err)
	}
	if n == 0 {
		var exists bool
		if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM gui_admin)`).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return errAdminMissing
		}
		return errAttemptTooSoon
	}
	return nil
}

// UpdatePassword replaces the bcrypt hash and rotates session key+salt (invalidates cookies).
func (s *dbStore) UpdatePassword(ctx context.Context, passwordHash string) error {
	key, salt, err := newSessionSecrets()
	if err != nil {
		return xerrors.Errorf("session secrets: %w", err)
	}
	n, err := s.db.Exec(ctx, `
		UPDATE gui_admin
		SET password_hash = $1, session_key = $2, session_salt = $3
		WHERE id = TRUE`, passwordHash, key, salt)
	if err != nil {
		return err
	}
	if n == 0 {
		return errAdminMissing
	}
	return nil
}
