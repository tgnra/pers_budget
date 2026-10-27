package main

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// accountHashes and dummyHash are generated from accounts.txt into accounts_gen.go
// by `go generate` (see tools/genaccounts).

// verifyPassword checks password against "pbkdf2-sha256$<iter>$<salt b64>$<key b64>".
func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	salt, err2 := base64.RawStdEncoding.DecodeString(parts[2])
	want, err3 := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || err2 != nil || err3 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

// checkLogin verifies credentials, spending the same effort whether or not the user exists.
func checkLogin(user, password string) bool {
	h, ok := accountHashes[user]
	if !ok {
		h = dummyHash
	}
	return verifyPassword(h, password) && ok
}

const (
	sessionCookie = "budget_session"
	sessionTTL    = 90 * 24 * time.Hour
)

// sessions issues HMAC-signed cookies: base64(user|expiry).base64(mac).
type sessions struct{ key []byte }

// loadSessionKey reads <dir>/.session_key, creating it on first run so sessions survive restarts.
func loadSessionKey(dir string) ([]byte, error) {
	p := filepath.Join(dir, ".session_key")
	data, err := os.ReadFile(p)
	if err == nil {
		return hex.DecodeString(strings.TrimSpace(string(data)))
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	rand.Read(key)
	return key, os.WriteFile(p, []byte(hex.EncodeToString(key)), 0o600)
}

func (s *sessions) mac(payload string) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(payload))
	return m.Sum(nil)
}

func secure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func (s *sessions) set(w http.ResponseWriter, r *http.Request, user string) {
	exp := time.Now().Add(sessionTTL)
	payload := user + "|" + strconv.FormatInt(exp.Unix(), 10)
	enc := base64.RawURLEncoding
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString(s.mac(payload)),
		Path:     "/",
		Expires:  exp,
		HttpOnly: true,
		Secure:   secure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *sessions) clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
}

// user returns the signed-in user, or "" if the cookie is missing, invalid or expired.
func (s *sessions) user(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	p64, m64, ok := strings.Cut(c.Value, ".")
	enc := base64.RawURLEncoding
	payload, err := enc.DecodeString(p64)
	mac, err2 := enc.DecodeString(m64)
	if !ok || err != nil || err2 != nil || !hmac.Equal(mac, s.mac(string(payload))) {
		return ""
	}
	user, expStr, _ := strings.Cut(string(payload), "|")
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return ""
	}
	if _, ok := accountHashes[user]; !ok {
		return "" // account removed in a later build
	}
	return user
}
