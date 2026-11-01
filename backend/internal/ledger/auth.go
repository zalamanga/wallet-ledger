package ledger

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sort"
	"sync"

	"github.com/jackc/pgx/v5"
)

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
type accountInput struct {
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}
type setupInput struct {
	WorkspaceName string `json:"workspace_name"`
	FullName      string `json:"full_name"`
	Email         string `json:"email"`
	Password      string `json:"password"`
}

func normalizeAccount(input *accountInput) error {
	var err error
	if input.FullName, err = cleanText(input.FullName, 1, 120, "Nama"); err != nil {
		return err
	}
	if input.Email, err = cleanEmail(input.Email); err != nil {
		return err
	}
	return validatePassword(input.Password)
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	var name *string
	err := s.pool.QueryRow(r.Context(), "SELECT name FROM workspaces LIMIT 1").Scan(&name)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"setup_required": name == nil, "workspace_name": name})
}

func (s *Server) anonymousCSRF(w http.ResponseWriter, r *http.Request) {
	token := randomToken()
	http.SetCookie(w, &http.Cookie{Name: "walletledger_csrf", Value: token, Path: "/api/auth", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: 3600})
	writeJSON(w, 200, map[string]string{"csrf_token": token})
}

func (s *Server) preAuthCSRF(r *http.Request) error {
	if err := s.origin(r); err != nil {
		return err
	}
	if i, err := s.authenticate(r.Context(), s.pool, r, false); err == nil && equalToken(r.Header.Get("X-CSRF-Token"), i.CSRFToken) {
		return nil
	}
	c, err := r.Cookie("walletledger_csrf")
	if err != nil || len(c.Value) != 43 || !equalToken(c.Value, r.Header.Get("X-CSRF-Token")) {
		return problem(403, "CSRF_INVALID", "Token formulir perlu diperbarui.")
	}
	return nil
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if err := s.preAuthCSRF(r); err != nil {
		writeError(w, err)
		return
	}
	var input setupInput
	if err := decodeBody(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	account := accountInput{FullName: input.FullName, Email: input.Email, Password: input.Password}
	if err := normalizeAccount(&account); err != nil {
		writeError(w, err)
		return
	}
	name, err := cleanText(input.WorkspaceName, 1, 100, "Nama workspace")
	if err != nil {
		writeError(w, err)
		return
	}
	hash, err := hashPassword(account.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtextextended('walletledger:setup',0))"); err != nil {
		writeError(w, err)
		return
	}
	var count int
	if err = tx.QueryRow(r.Context(), "SELECT count(*) FROM workspaces").Scan(&count); err != nil {
		writeError(w, err)
		return
	}
	if count != 0 {
		writeError(w, problem(409, "SETUP_COMPLETE", "Workspace sudah dibuat. Silakan masuk."))
		return
	}
	view := AuthView{User: AuthUser{ID: newID(), Email: account.Email, FullName: account.FullName, Role: "Admin"}, Workspace: Workspace{ID: newID(), Name: name}}
	if _, err = tx.Exec(r.Context(), "INSERT INTO workspaces(id,name) VALUES($1,$2)", view.Workspace.ID, name); err == nil {
		_, err = tx.Exec(r.Context(), "INSERT INTO users(id,workspace_id,email,full_name,role,password_hash) VALUES($1,$2,$3,$4,'Admin',$5)", view.User.ID, view.Workspace.ID, view.User.Email, view.User.FullName, hash)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), "INSERT INTO wallets(id,workspace_id,user_id,is_system) VALUES($1,$2,$3,false),($4,$2,NULL,true)", newID(), view.Workspace.ID, view.User.ID, newID())
	}
	var token string
	if err == nil {
		token, view.CSRFToken, err = s.insertSession(r.Context(), tx, view.User.ID)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	s.sessionCookie(w, token)
	writeJSON(w, 201, view)
}

func (s *Server) insertSession(ctx context.Context, tx pgx.Tx, userID string) (string, string, error) {
	token, csrf := randomToken(), randomToken()
	_, err := tx.Exec(ctx, "INSERT INTO sessions(id,user_id,token_hash,csrf_token,expires_at) VALUES($1,$2,$3,$4,clock_timestamp()+make_interval(hours=>$5::integer))", newID(), userID, digest(token), csrf, s.cfg.SessionHours)
	return token, csrf, err
}

func (s *Server) sessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: "walletledger_session", Value: token, Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: s.cfg.SessionHours * 3600})
}

func peerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

var dummyHashOnce sync.Once
var dummyHash string

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if err := s.preAuthCSRF(r); err != nil {
		writeError(w, err)
		return
	}
	var input credentials
	if err := decodeBody(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	var err error
	input.Email, err = cleanEmail(input.Email)
	if err != nil {
		writeError(w, err)
		return
	}
	if err = validatePassword(input.Password); err != nil {
		writeError(w, err)
		return
	}
	if err = s.recordLoginAttempt(r.Context(), input.Email, peerIP(r)); err != nil {
		writeError(w, err)
		return
	}
	var hash string
	view := AuthView{}
	err = s.pool.QueryRow(r.Context(), `SELECT u.id,u.email,u.full_name,u.role,u.password_hash,w.id,w.name FROM users u JOIN workspaces w ON w.id=u.workspace_id WHERE u.email=$1`, input.Email).Scan(&view.User.ID, &view.User.Email, &view.User.FullName, &view.User.Role, &hash, &view.Workspace.ID, &view.Workspace.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		dummyHashOnce.Do(func() { dummyHash, _ = hashPassword("SyntheticUnknownUserTimingOnly947!") })
		_ = checkPassword(dummyHash, input.Password)
		writeError(w, problem(401, "INVALID_CREDENTIALS", "Email atau password salah."))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	if !checkPassword(hash, input.Password) {
		writeError(w, problem(401, "INVALID_CREDENTIALS", "Email atau password salah."))
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	token, csrf, err := s.insertSession(r.Context(), tx, view.User.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	if old, err := r.Cookie("walletledger_session"); err == nil {
		if _, err = tx.Exec(r.Context(), "DELETE FROM sessions WHERE token_hash=$1", digest(old.Value)); err != nil {
			writeError(w, err)
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	view.CSRFToken = csrf
	s.sessionCookie(w, token)
	writeJSON(w, 200, view)
}

func (s *Server) recordLoginAttempt(ctx context.Context, email, peer string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	locks := []string{"login:email:" + email, "login:peer:" + peer}
	sort.Strings(locks)
	for _, key := range locks {
		if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", key); err != nil {
			return err
		}
	}
	var emails, peers int
	err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE normalized_email=$1),count(*) FILTER(WHERE peer_ip=$2)
	  FROM login_attempts WHERE created_at>clock_timestamp()-interval '600 seconds' AND (normalized_email=$1 OR peer_ip=$2)`, email, peer).Scan(&emails, &peers)
	if err != nil {
		return err
	}
	if emails >= 10 || peers >= 60 {
		return problem(429, "RATE_LIMITED", "Terlalu banyak percobaan masuk. Coba kembali setelah jeda.")
	}
	_, err = tx.Exec(ctx, "INSERT INTO login_attempts(normalized_email,peer_ip) VALUES($1,$2)", email, peer)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	i, err := s.authenticate(r.Context(), s.pool, r, false)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, i.AuthView)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	i, err := s.authenticate(r.Context(), tx, r, true)
	if err == nil {
		err = s.authenticatedWrite(r, i)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	var body map[string]any
	if err = decodeBody(w, r, &body); err != nil {
		writeError(w, err)
		return
	}
	if len(body) != 0 {
		writeError(w, problem(422, "VALIDATION_ERROR", "Logout memakai objek JSON kosong."))
		return
	}
	if _, err = tx.Exec(r.Context(), "DELETE FROM sessions WHERE id=$1", i.SessionID); err != nil {
		writeError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "walletledger_session", Value: "", Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	writeJSON(w, 204, nil)
}

func (s *Server) listPeople(w http.ResponseWriter, r *http.Request) {
	s.withRead(w, r, false, func(tx pgx.Tx, i *identity) (any, error) {
		rows, err := tx.Query(r.Context(), "SELECT u.id,u.full_name,w.id FROM users u JOIN wallets w ON w.user_id=u.id WHERE u.workspace_id=$1 ORDER BY lower(u.full_name),u.id", i.Workspace.ID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		type person struct {
			ID       string `json:"id"`
			FullName string `json:"full_name"`
			WalletID string `json:"wallet_id"`
		}
		items := []person{}
		for rows.Next() {
			var item person
			if err = rows.Scan(&item.ID, &item.FullName, &item.WalletID); err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return map[string]any{"items": items}, rows.Err()
	})
}

func (s *Server) listAdminPeople(w http.ResponseWriter, r *http.Request) {
	s.withRead(w, r, true, func(tx pgx.Tx, i *identity) (any, error) {
		rows, err := tx.Query(r.Context(), "SELECT id,email,full_name,role FROM users WHERE workspace_id=$1 ORDER BY created_at,id", i.Workspace.ID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := []AuthUser{}
		for rows.Next() {
			var item AuthUser
			if err = rows.Scan(&item.ID, &item.Email, &item.FullName, &item.Role); err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return map[string]any{"items": items}, rows.Err()
	})
}
