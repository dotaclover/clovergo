package service

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"clovergo/internal/domain"
)

const (
	lockPBKDF2Iterations = 210000
	lockMinRunes         = 4
	lockMaxRunes         = 64
	lockMaxFailures      = 5
	lockCooldown         = 30 * time.Second
	lockTokenTTL         = 30 * 24 * time.Hour
)

// lockGuard throttles password guesses: after lockMaxFailures consecutive
// wrong passwords every attempt is refused for lockCooldown. It lives in
// memory, so a restart clears it; that is acceptable for a local app.
// verify serialises check→compare→count so concurrent guesses cannot exceed
// the limit. keyOnce/key is the per-process random token-signing key: tokens
// stop working after a restart, and the key never touches the database.
type lockGuard struct {
	verify   sync.Mutex
	mu       sync.Mutex
	failures int
	until    time.Time

	keyOnce sync.Once
	key     []byte
}

func (g *lockGuard) signingKey() []byte {
	g.keyOnce.Do(func() {
		g.key = make([]byte, 32)
		if _, err := rand.Read(g.key); err != nil {
			panic("clovergo: crypto/rand unavailable: " + err.Error())
		}
	})
	return g.key
}

func (g *lockGuard) check() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if time.Now().Before(g.until) {
		return appError(CodeTooManyAttempts, "too many wrong passwords, try again in a moment", nil)
	}
	return nil
}

func (g *lockGuard) fail() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.failures++
	if g.failures >= lockMaxFailures {
		g.failures = 0
		g.until = time.Now().Add(lockCooldown)
	}
}

func (g *lockGuard) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.failures = 0
}

// LockStatus reports whether a global password is set.
func (s *Service) LockStatus(ctx context.Context) (bool, error) {
	lock, err := s.repo.AppLock(ctx)
	if err != nil {
		return false, appError(CodeInternal, "read lock failed", err)
	}
	return lock != nil, nil
}

// VerifyToken is the check behind every protected request. With no password
// set everything is open; otherwise the token must carry an unexpired time
// and a valid signature made with the per-process key over that time and the
// stored hash. Changing or removing the password, or restarting, therefore
// invalidates every token issued before.
func (s *Service) VerifyToken(ctx context.Context, token string) error {
	lock, err := s.repo.AppLock(ctx)
	if err != nil {
		return appError(CodeInternal, "read lock failed", err)
	}
	if lock == nil {
		return nil
	}
	if !s.validToken(token, lock.PasswordHash) {
		return appError(CodeAuthRequired, "the app is locked: unlock it with the password", nil)
	}
	return nil
}

func (s *Service) validToken(token, hashHex string) bool {
	expStr, sig, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	want := s.signToken(exp, hashHex)
	return subtle.ConstantTimeCompare([]byte(sig), []byte(want)) == 1
}

func (s *Service) newToken(hashHex string) string {
	exp := time.Now().Add(lockTokenTTL).Unix()
	return strconv.FormatInt(exp, 10) + "." + s.signToken(exp, hashHex)
}

func (s *Service) signToken(exp int64, hashHex string) string {
	mac := hmac.New(sha256.New, s.lockGuard.signingKey())
	mac.Write([]byte("clovergo-session|" + strconv.FormatInt(exp, 10) + "|" + hashHex))
	return hex.EncodeToString(mac.Sum(nil))
}

// Unlock checks the password and returns the token the client stores.
func (s *Service) Unlock(ctx context.Context, password string) (string, error) {
	lock, err := s.repo.AppLock(ctx)
	if err != nil {
		return "", appError(CodeInternal, "read lock failed", err)
	}
	if lock == nil {
		return "", appError(CodeInvalidState, "no password is set", nil)
	}
	if err := s.checkPassword(lock.PasswordHash, lock.Salt, lock.Iterations, password); err != nil {
		return "", err
	}
	return s.newToken(lock.PasswordHash), nil
}

// SetPassword sets or changes the global password. When one is already set,
// the current password must be supplied. It returns a fresh token because the
// old one stops working.
func (s *Service) SetPassword(ctx context.Context, current, password string) (string, error) {
	if n := utf8.RuneCountInString(password); n < lockMinRunes || n > lockMaxRunes {
		return "", appError(CodeInvalidInput, "the password must be 4 to 64 characters", nil)
	}
	existing, err := s.repo.AppLock(ctx)
	if err != nil {
		return "", appError(CodeInternal, "read lock failed", err)
	}
	if existing != nil {
		if err := s.checkPassword(existing.PasswordHash, existing.Salt, existing.Iterations, current); err != nil {
			return "", err
		}
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", appError(CodeInternal, "generate salt failed", err)
	}
	hash, err := pbkdf2.Key(sha256.New, password, salt, lockPBKDF2Iterations, 32)
	if err != nil {
		return "", appError(CodeInternal, "hash password failed", err)
	}
	lock := domain.AppLock{PasswordHash: hex.EncodeToString(hash), Salt: hex.EncodeToString(salt), Iterations: lockPBKDF2Iterations}
	if err := s.repo.SaveAppLock(ctx, lock); err != nil {
		return "", appError(CodeInternal, "save lock failed", err)
	}
	s.lockGuard.reset()
	s.log.InfoContext(ctx, "global password set")
	return s.newToken(lock.PasswordHash), nil
}

// RemovePassword turns the lock off after confirming the current password.
func (s *Service) RemovePassword(ctx context.Context, current string) error {
	existing, err := s.repo.AppLock(ctx)
	if err != nil {
		return appError(CodeInternal, "read lock failed", err)
	}
	if existing == nil {
		return appError(CodeInvalidState, "no password is set", nil)
	}
	if err := s.checkPassword(existing.PasswordHash, existing.Salt, existing.Iterations, current); err != nil {
		return err
	}
	if err := s.repo.DeleteAppLock(ctx); err != nil {
		return appError(CodeInternal, "remove lock failed", err)
	}
	s.log.InfoContext(ctx, "global password removed")
	return nil
}

func (s *Service) checkPassword(hashHex, saltHex string, iterations int, password string) error {
	s.lockGuard.verify.Lock()
	defer s.lockGuard.verify.Unlock()
	if err := s.lockGuard.check(); err != nil {
		return err
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return appError(CodeInternal, "stored salt is corrupt", err)
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		return appError(CodeInternal, "hash password failed", err)
	}
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(got)), []byte(hashHex)) != 1 {
		s.lockGuard.fail()
		return appError(CodePasswordIncorrect, "the password is incorrect", nil)
	}
	s.lockGuard.reset()
	return nil
}
