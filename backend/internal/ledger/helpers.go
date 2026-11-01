package ledger

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
var amountPattern = regexp.MustCompile(`^[1-9][0-9]{0,9}$`)

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("system random source unavailable")
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("system random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func digest(value string) string { h := sha256.Sum256([]byte(value)); return hex.EncodeToString(h[:]) }
func equalToken(a, b string) bool {
	return len(a) > 0 && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func validID(value string) bool   { return uuidPattern.MatchString(value) }
func lowerID(value string) string { return strings.ToLower(value) }

func passwordInput(password string) []byte {
	// Prehashing preserves the documented 128-byte input limit without bcrypt truncation.
	h := sha256.Sum256([]byte(password))
	return []byte(base64.StdEncoding.EncodeToString(h[:]))
}
func hashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword(passwordInput(password), 12)
	return "sha256$" + string(h), err
}
func checkPassword(hash, password string) bool {
	if !strings.HasPrefix(hash, "sha256$") {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(strings.TrimPrefix(hash, "sha256$")), passwordInput(password)) == nil
}
func validatePassword(value string) error {
	if len(value) < 12 || len(value) > 128 || !utf8.ValidString(value) {
		return problem(422, "VALIDATION_ERROR", "Password harus terdiri dari 12 sampai 128 byte UTF-8.")
	}
	return nil
}
func cleanText(value string, min, max int, label string) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) || utf8.RuneCountInString(value) < min || utf8.RuneCountInString(value) > max {
		return "", problem(422, "VALIDATION_ERROR", label+" tidak memenuhi panjang yang diizinkan.")
	}
	return value, nil
}
func cleanEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || len(value) > 254 || !strings.Contains(value, "@") {
		return "", problem(422, "VALIDATION_ERROR", "Alamat email tidak valid.")
	}
	return value, nil
}
func parseAmount(value string) (int64, error) {
	if !amountPattern.MatchString(value) {
		return 0, problem(422, "VALIDATION_ERROR", "Jumlah harus berupa string bilangan bulat positif tanpa nol di depan.")
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n > 1_000_000_000 {
		return 0, problem(422, "VALIDATION_ERROR", "Jumlah per transaksi paling banyak 1000000000 rupiah simulasi.")
	}
	return n, nil
}

func decodeBody(w http.ResponseWriter, r *http.Request, target any) error {
	if contentType := strings.Split(r.Header.Get("Content-Type"), ";")[0]; contentType != "application/json" {
		return problem(422, "VALIDATION_ERROR", "Gunakan Content-Type application/json.")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	raw, readErr := io.ReadAll(r.Body)
	if readErr != nil {
		var size *http.MaxBytesError
		if errors.As(readErr, &size) {
			return problem(413, "BODY_TOO_LARGE", "Badan permintaan melebihi 64 KiB.")
		}
		return problem(422, "VALIDATION_ERROR", "Badan JSON tidak dapat dibaca.")
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return problem(422, "VALIDATION_ERROR", "Badan permintaan harus berupa objek JSON.")
	}
	d := json.NewDecoder(bytes.NewReader(trimmed))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			return problem(413, "BODY_TOO_LARGE", "Badan permintaan melebihi 64 KiB.")
		}
		return problem(422, "VALIDATION_ERROR", "Badan JSON tidak valid atau memiliki kolom yang tidak dikenal.")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return problem(422, "VALIDATION_ERROR", "Badan permintaan harus berisi satu objek JSON.")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if status != http.StatusNoContent {
		_ = json.NewEncoder(w).Encode(value)
	}
}
func writeError(w http.ResponseWriter, err error) {
	var e *apiError
	if !errors.As(err, &e) {
		e = problem(503, "SERVICE_UNAVAILABLE", "Layanan belum dapat memastikan hasil. Coba lagi dengan request yang sama.")
	}
	if e.Status == 429 {
		w.Header().Set("Retry-After", "600")
	}
	writeJSON(w, e.Status, e)
}

func pageParams(r *http.Request) (int64, int, error) {
	before := int64(0)
	limit := 50
	var err error
	if value := r.URL.Query().Get("before"); value != "" {
		before, err = strconv.ParseInt(value, 10, 64)
		if err != nil || before <= 0 {
			return 0, 0, problem(422, "VALIDATION_ERROR", "Cursor before harus bilangan bulat positif.")
		}
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			return 0, 0, problem(422, "VALIDATION_ERROR", "Limit harus 1 sampai 100.")
		}
	}
	return before, limit, nil
}
