package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"rosboard/internal/store"
)

var (
	ErrAdminExists        = errors.New("administrator already exists")
	ErrAdminNotFound      = errors.New("administrator not found")
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrInvalidSession     = errors.New("invalid session")
	ErrRateLimited        = errors.New("too many login attempts")
)

const (
	SessionCookieName = "rosboard_session"
	SessionLifetime   = 7 * 24 * time.Hour
	sessionRenewAfter = 24 * time.Hour
	administratorID   = int64(1)
)

type PasswordParams struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

var DefaultPasswordParams = PasswordParams{
	Memory:      64 * 1024,
	Iterations:  3,
	Parallelism: 2,
	SaltLength:  16,
	KeyLength:   32,
}

type Options struct {
	Now            func() time.Time
	Random         io.Reader
	PasswordParams PasswordParams
}

type Service struct {
	store       *store.Store
	now         func() time.Time
	random      io.Reader
	params      PasswordParams
	limiter     *loginLimiter
	verifySlots chan struct{}
}

type Session struct {
	Token     string
	ExpiresAt time.Time
	Username  string
	Renewed   bool
}

type LoginError struct {
	Err        error
	RetryAfter time.Duration
}

func (e *LoginError) Error() string { return e.Err.Error() }
func (e *LoginError) Unwrap() error { return e.Err }

func New(storage *store.Store) *Service {
	return NewWithOptions(storage, Options{})
}

func NewWithOptions(storage *store.Store, options Options) *Service {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	random := options.Random
	if random == nil {
		random = rand.Reader
	}
	params := options.PasswordParams
	if params.Memory == 0 {
		params = DefaultPasswordParams
	}
	return &Service{
		store: storage, now: now, random: random, params: params,
		limiter: newLoginLimiter(now), verifySlots: make(chan struct{}, 2),
	}
}

func ValidateUsername(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) {
		return "", errors.New("username must be valid UTF-8")
	}
	count := utf8.RuneCountInString(value)
	if count < 1 || count > 64 {
		return "", errors.New("username must contain 1 to 64 characters")
	}
	return value, nil
}

func ValidatePassword(password, confirmation string) error {
	if password != confirmation {
		return errors.New("password confirmation does not match")
	}
	if !utf8.ValidString(password) {
		return errors.New("password must be valid UTF-8")
	}
	count := utf8.RuneCountInString(password)
	if count < 4 || count > 128 {
		return errors.New("password must contain 4 to 128 characters")
	}
	return nil
}

func (s *Service) Admin(ctx context.Context) (store.AdminAccount, error) {
	account, err := s.store.Admin(ctx)
	if errors.Is(err, store.ErrAdminNotFound) {
		return store.AdminAccount{}, ErrAdminNotFound
	}
	return account, err
}

func (s *Service) CreateAdmin(ctx context.Context, username, password, confirmation string) (Session, error) {
	username, err := ValidateUsername(username)
	if err != nil {
		return Session{}, err
	}
	if err := ValidatePassword(password, confirmation); err != nil {
		return Session{}, err
	}
	passwordHash, err := s.hashPassword(password)
	if err != nil {
		return Session{}, err
	}
	session, persisted, err := s.newSession(administratorID)
	if err != nil {
		return Session{}, err
	}
	if _, err := s.store.CreateAdminWithSession(ctx, username, passwordHash, persisted, s.now().UTC()); err != nil {
		if errors.Is(err, store.ErrAdminExists) {
			return Session{}, ErrAdminExists
		}
		return Session{}, err
	}
	session.Username = username
	return session, nil
}

func (s *Service) Login(ctx context.Context, remoteIP, username, password string) (Session, error) {
	username = strings.TrimSpace(username)
	key := remoteIP + "\x00" + username
	if retry := s.limiter.retryAfter(key); retry > 0 {
		return Session{}, &LoginError{Err: ErrRateLimited, RetryAfter: retry}
	}
	select {
	case s.verifySlots <- struct{}{}:
		defer func() { <-s.verifySlots }()
	default:
		return Session{}, &LoginError{Err: ErrRateLimited, RetryAfter: time.Second}
	}

	account, err := s.store.Admin(ctx)
	passwordMatches := err == nil && s.verifyPassword(password, account.PasswordHash)
	valid := err == nil && account.Username == username && passwordMatches
	if !valid {
		retry := s.limiter.failed(key)
		if retry > 0 {
			return Session{}, &LoginError{Err: ErrRateLimited, RetryAfter: retry}
		}
		return Session{}, ErrInvalidCredentials
	}
	s.limiter.succeeded(key)
	session, persisted, err := s.newSession(account.ID)
	if err != nil {
		return Session{}, err
	}
	if err := s.store.CreateAuthSession(ctx, persisted); err != nil {
		return Session{}, err
	}
	session.Username = account.Username
	return session, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	tokenHash, err := decodeAndHashToken(token)
	if err != nil {
		return Session{}, ErrInvalidSession
	}
	persisted, err := s.store.AuthSession(ctx, tokenHash)
	if errors.Is(err, store.ErrSessionNotFound) {
		return Session{}, ErrInvalidSession
	}
	if err != nil {
		return Session{}, err
	}
	now := s.now().UTC()
	if !persisted.ExpiresAt.After(now) {
		_ = s.store.DeleteAuthSession(ctx, tokenHash)
		return Session{}, ErrInvalidSession
	}
	account, err := s.store.Admin(ctx)
	if err != nil || account.ID != persisted.AdminID {
		return Session{}, ErrInvalidSession
	}
	session := Session{Token: token, ExpiresAt: persisted.ExpiresAt, Username: account.Username}
	if now.Sub(persisted.LastSeen) >= sessionRenewAfter {
		session.ExpiresAt = now.Add(SessionLifetime)
		if err := s.store.RenewAuthSession(ctx, tokenHash, now, session.ExpiresAt); err != nil {
			return Session{}, err
		}
		session.Renewed = true
	}
	return session, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	tokenHash, err := decodeAndHashToken(token)
	if err != nil {
		return nil
	}
	return s.store.DeleteAuthSession(ctx, tokenHash)
}

func (s *Service) UpdateCredentials(ctx context.Context, username, password, confirmation string) (string, error) {
	username, err := ValidateUsername(username)
	if err != nil {
		return "", err
	}
	if err := ValidatePassword(password, confirmation); err != nil {
		return "", err
	}
	passwordHash, err := s.hashPassword(password)
	if err != nil {
		return "", err
	}
	if err := s.store.UpdateAdminCredentials(ctx, username, passwordHash, s.now().UTC()); err != nil {
		if errors.Is(err, store.ErrAdminNotFound) {
			return "", ErrAdminNotFound
		}
		return "", err
	}
	return username, nil
}

func (s *Service) ResetPassword(ctx context.Context, password, confirmation string) error {
	if _, err := s.store.Admin(ctx); err != nil {
		return ErrAdminNotFound
	}
	return s.setPassword(ctx, password, confirmation)
}

func (s *Service) setPassword(ctx context.Context, password, confirmation string) error {
	if err := ValidatePassword(password, confirmation); err != nil {
		return err
	}
	passwordHash, err := s.hashPassword(password)
	if err != nil {
		return err
	}
	return s.store.UpdateAdminPassword(ctx, passwordHash, s.now().UTC())
}

func (s *Service) OnboardingComplete(ctx context.Context) (bool, error) {
	return s.store.OnboardingComplete(ctx)
}

func (s *Service) CompleteOnboarding(ctx context.Context) error {
	return s.store.SetOnboardingComplete(ctx, s.now().UTC())
}

func (s *Service) newSession(adminID int64) (Session, store.AuthSession, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return Session{}, store.AuthSession{}, fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	tokenHash := sha256.Sum256(raw)
	now := s.now().UTC()
	expiresAt := now.Add(SessionLifetime)
	return Session{Token: token, ExpiresAt: expiresAt}, store.AuthSession{
		TokenHash: tokenHash[:], AdminID: adminID, CreatedAt: now, LastSeen: now, ExpiresAt: expiresAt,
	}, nil
}

func decodeAndHashToken(token string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return nil, ErrInvalidSession
	}
	hash := sha256.Sum256(raw)
	return hash[:], nil
}

func (s *Service) hashPassword(password string) (string, error) {
	salt := make([]byte, s.params.SaltLength)
	if _, err := io.ReadFull(s.random, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, s.params.Iterations, s.params.Memory, s.params.Parallelism, s.params.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, s.params.Memory, s.params.Iterations, s.params.Parallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func (s *Service) verifyPasswordLimited(password, encoded string) bool {
	select {
	case s.verifySlots <- struct{}{}:
		defer func() { <-s.verifySlots }()
		return s.verifyPassword(password, encoded)
	default:
		return false
	}
}

func (s *Service) verifyPassword(password, encoded string) bool {
	params, salt, expected, err := parsePasswordHash(encoded)
	if err != nil {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func parsePasswordHash(encoded string) (PasswordParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return PasswordParams{}, nil, nil, errors.New("invalid password hash")
	}
	var params PasswordParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.Memory, &params.Iterations, &params.Parallelism); err != nil {
		return PasswordParams{}, nil, nil, errors.New("invalid password hash parameters")
	}
	if params.Memory < 8*1024 || params.Memory > 256*1024 || params.Iterations < 1 || params.Iterations > 10 || params.Parallelism < 1 || params.Parallelism > 8 {
		return PasswordParams{}, nil, nil, errors.New("unsafe password hash parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return PasswordParams{}, nil, nil, errors.New("invalid password salt")
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(hash) < 16 {
		return PasswordParams{}, nil, nil, errors.New("invalid password hash")
	}
	return params, salt, hash, nil
}
