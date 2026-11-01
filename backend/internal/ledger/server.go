package ledger

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	pool      *pgxpool.Pool
	cfg       Config
	mux       *http.ServeMux
	semaphore chan struct{}
	failpoint func(stage string) error
}

func New(pool *pgxpool.Pool, cfg Config) *Server {
	if len(cfg.ReceiptSecret) < 32 {
		panic("ReceiptSecret must contain at least 32 bytes")
	}
	if cfg.SessionHours <= 0 {
		cfg.SessionHours = 12
	}
	if len(cfg.AllowedOrigins) == 0 {
		cfg.AllowedOrigins = []string{"http://127.0.0.1:8170", "http://localhost:8170"}
	}
	s := &Server{pool: pool, cfg: cfg, mux: http.NewServeMux(), semaphore: make(chan struct{}, 32)}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		select {
		case s.semaphore <- struct{}{}:
			defer func() { <-s.semaphore }()
		default:
			writeError(w, problem(503, "SERVICE_BUSY", "Layanan sedang sibuk. Coba request yang sama kembali."))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		s.mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		if err := s.pool.Ping(r.Context()); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ready"})
	})
	s.mux.HandleFunc("GET /api/openapi.json", s.openapi)
	s.mux.HandleFunc("GET /api/auth/status", s.authStatus)
	s.mux.HandleFunc("GET /api/auth/csrf", s.anonymousCSRF)
	s.mux.HandleFunc("POST /api/auth/setup", s.setup)
	s.mux.HandleFunc("POST /api/auth/login", s.login)
	s.mux.HandleFunc("GET /api/auth/me", s.me)
	s.mux.HandleFunc("POST /api/auth/logout", s.logout)
	s.mux.HandleFunc("GET /api/people", s.listPeople)
	s.mux.HandleFunc("POST /api/people", s.createPerson)
	s.mux.HandleFunc("GET /api/admin/people", s.listAdminPeople)
	s.mux.HandleFunc("GET /api/wallets", s.listWallets)
	s.mux.HandleFunc("GET /api/wallets/{id}", s.getWallet)
	s.mux.HandleFunc("POST /api/transfers", s.transfer)
	s.mux.HandleFunc("POST /api/allocations", s.allocate)
	s.mux.HandleFunc("POST /api/transactions/{id}/reverse", s.reverse)
	s.mux.HandleFunc("GET /api/transactions", s.listTransactions)
	s.mux.HandleFunc("GET /api/transactions/{id}", s.getTransaction)
	s.mux.HandleFunc("GET /api/reconciliation", s.reconciliation)
	s.mux.HandleFunc("GET /api/audit", s.audit)
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, problem(404, "NOT_FOUND", "Endpoint tidak ditemukan."))
	})
	if dist := os.Getenv("WALLETLEDGER_FRONTEND_DIR"); dist != "" {
		absolute, err := filepath.Abs(dist)
		if err == nil {
			files := http.FileServer(http.Dir(absolute))
			s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet && r.Method != http.MethodHead {
					w.Header().Set("Allow", "GET, HEAD")
					writeError(w, problem(405, "METHOD_NOT_ALLOWED", "Berkas frontend hanya dapat dibaca melalui GET atau HEAD."))
					return
				}
				clean := filepath.Join(absolute, filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))
				rel, err := filepath.Rel(absolute, clean)
				if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					http.NotFound(w, r)
					return
				}
				if r.URL.Path == "/" {
					http.ServeFile(w, r, filepath.Join(absolute, "index.html"))
					return
				}
				for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
					if strings.HasPrefix(part, ".") {
						http.NotFound(w, r)
						return
					}
				}
				if info, err := os.Stat(clean); err == nil && !info.IsDir() {
					files.ServeHTTP(w, r)
					return
				}
				http.NotFound(w, r)
			})
		}
	}
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Server) authenticate(ctx context.Context, q queryer, r *http.Request, lock bool) (*identity, error) {
	cookie, err := r.Cookie("walletledger_session")
	if err != nil || len(cookie.Value) != 43 {
		return nil, problem(401, "UNAUTHENTICATED", "Silakan masuk terlebih dahulu.")
	}
	stmt := `SELECT s.id,u.id,u.email,u.full_name,u.role,w.id,w.name,s.csrf_token
	    FROM sessions s JOIN users u ON u.id=s.user_id JOIN workspaces w ON w.id=u.workspace_id
	    WHERE s.token_hash=$1 AND s.expires_at>clock_timestamp()`
	if lock {
		stmt += " FOR SHARE OF s,u"
	}
	i := &identity{}
	err = q.QueryRow(ctx, stmt, digest(cookie.Value)).Scan(&i.SessionID, &i.User.ID, &i.User.Email, &i.User.FullName, &i.User.Role, &i.Workspace.ID, &i.Workspace.Name, &i.CSRFToken)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, problem(401, "UNAUTHENTICATED", "Sesi sudah berakhir. Silakan masuk kembali.")
	}
	if err != nil {
		return nil, err
	}
	return i, nil
}

func (s *Server) origin(r *http.Request) error {
	value := r.Header.Get("Origin")
	for _, allowed := range s.cfg.AllowedOrigins {
		if value != "" && value == allowed {
			return nil
		}
	}
	return problem(403, "ORIGIN_INVALID", "Origin permintaan tidak diizinkan.")
}

func (s *Server) identityHeaders(r *http.Request, i *identity) error {
	actor, workspace := r.Header.Get("X-Wallet-Actor"), r.Header.Get("X-Wallet-Workspace")
	if !validID(actor) || !validID(workspace) || lowerID(actor) != i.User.ID || lowerID(workspace) != i.Workspace.ID {
		return problem(403, "IDENTITY_CHANGED", "Akun aktif berubah. Muat ulang dan periksa akun sebelum mengirim.")
	}
	return nil
}

func (s *Server) authenticatedWrite(r *http.Request, i *identity) error {
	if err := s.origin(r); err != nil {
		return err
	}
	if err := s.identityHeaders(r, i); err != nil {
		return err
	}
	if !equalToken(r.Header.Get("X-CSRF-Token"), i.CSRFToken) {
		return problem(403, "CSRF_INVALID", "Token formulir perlu diperbarui.")
	}
	return nil
}

func requireAdmin(i *identity) error {
	if i.User.Role != "Admin" {
		return problem(403, "FORBIDDEN", "Tindakan ini hanya untuk Admin.")
	}
	return nil
}

func (s *Server) withRead(w http.ResponseWriter, r *http.Request, admin bool, fn func(pgx.Tx, *identity) (any, error)) {
	tx, err := s.pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		writeError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	i, err := s.authenticate(r.Context(), tx, r, false)
	if err == nil && admin {
		err = requireAdmin(i)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	result, err := fn(tx, i)
	if err != nil {
		writeError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
