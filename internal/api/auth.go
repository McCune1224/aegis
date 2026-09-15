package api

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie = "aegis_session"
	csrfHeader    = "X-CSRF-Token"

	sessionTTL = 12 * time.Hour
	csrfBytes  = 32

	// The iteration count is the only knob that makes an offline guess expensive,
	// so it is high enough to cost a login a few milliseconds. Tests lower it
	// through TestMain; the encoded hash carries whatever count it was made with.
	pbkdf2KeyLength  = 32
	pbkdf2SaltLength = 16
)

var passwordIterations = 210_000

// Auth holds the one admin credential and the sessions that prove a login.
// Sessions live in memory, so a restart signs every browser out.
type Auth struct {
	hash   string
	secure bool

	mu       sync.Mutex
	sessions map[string]session
}

type session struct {
	csrf    string
	expires time.Time
}

// NewAuth returns an Auth for the encoded password hash. secure marks the
// session cookie Secure, which it must be when the server speaks TLS.
func NewAuth(hash string, secure bool) *Auth {
	return &Auth{hash: hash, secure: secure, sessions: make(map[string]session)}
}

// HashPassword returns an encoded hash that VerifyPassword reads back. The
// format is pbkdf2_sha256$iterations$salt$key, all base64.
func HashPassword(password string) (string, error) {
	salt := make([]byte, pbkdf2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("api: password salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, pbkdf2KeyLength)
	if err != nil {
		return "", fmt.Errorf("api: password hash: %w", err)
	}
	return strings.Join([]string{
		"pbkdf2_sha256",
		strconv.Itoa(passwordIterations),
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	}, "$"), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// CheckPassword reports whether password matches the stored credential.
func (a *Auth) CheckPassword(password string) bool {
	return verifyPassword(a.hash, password)
}

// Begin opens a session and writes its cookie. It returns the CSRF token the
// client must echo on every state-changing request.
func (a *Auth) Begin(w http.ResponseWriter) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	csrf, err := randomToken()
	if err != nil {
		return "", err
	}

	a.mu.Lock()
	now := time.Now()
	for existing, record := range a.sessions {
		if now.After(record.expires) {
			delete(a.sessions, existing)
		}
	}
	a.sessions[token] = session{csrf: csrf, expires: now.Add(sessionTTL)}
	a.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	return csrf, nil
}

// End closes the session the request carries.
func (a *Auth) End(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		a.mu.Lock()
		delete(a.sessions, cookie.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func (a *Auth) session(r *http.Request) (session, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return session{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	found, ok := a.sessions[cookie.Value]
	if !ok {
		return session{}, false
	}
	if time.Now().After(found.expires) {
		delete(a.sessions, cookie.Value)
		return session{}, false
	}
	return found, true
}

// Authenticated reports whether the request carries a live session.
func (a *Auth) Authenticated(r *http.Request) bool {
	_, ok := a.session(r)
	return ok
}

// ValidCSRF reports whether the request echoes the session's CSRF token. It is
// the second half of the check, after Authenticated.
func (a *Auth) ValidCSRF(r *http.Request) bool {
	found, ok := a.session(r)
	if !ok {
		return false
	}
	header := r.Header.Get(csrfHeader)
	if header == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(header), []byte(found.csrf)) == 1
}

func randomToken() (string, error) {
	buffer := make([]byte, csrfBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("api: random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

// GeneratePassword returns a random password to seed a first boot with.
func GeneratePassword() (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	return token, nil
}
