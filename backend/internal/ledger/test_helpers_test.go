package ledger

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testOrigin = "http://walletledger.test"
const syntheticPassword = "SyntheticWalletLedgerAccess947!"
const syntheticReceiptSecret = "synthetic-tests-only-walletledger-receipt-secret-never-production-947"

type testUser struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Role     string `json:"role"`
}

type testAuth struct {
	User      testUser `json:"user"`
	Workspace struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"workspace"`
	CSRF string `json:"csrf_token"`
}

type testActor struct {
	client *http.Client
	auth   testAuth
}
type testFixture struct {
	server *Server
	http   *httptest.Server
	pool   *pgxpool.Pool
	admin  *testActor
}
type testReply struct {
	status int
	body   []byte
	header http.Header
}
type testTransaction struct {
	ID         string  `json:"id"`
	Seq        int64   `json:"seq"`
	Kind       string  `json:"kind"`
	Amount     string  `json:"amount_minor"`
	Note       string  `json:"note"`
	From       string  `json:"from_wallet_id"`
	To         string  `json:"to_wallet_id"`
	ReversalOf *string `json:"reversal_of"`
	ReversalID *string `json:"reversal_id"`
}
type testDetail struct {
	Transaction testTransaction `json:"transaction"`
	Entries     []struct {
		Wallet    string `json:"wallet_id"`
		Direction string `json:"direction"`
		Amount    string `json:"amount_minor"`
	} `json:"entries"`
	Debit  string `json:"debit_total_minor"`
	Credit string `json:"credit_total_minor"`
}
type testMutation struct {
	RequestID   string      `json:"request_id"`
	Replayed    bool        `json:"replayed"`
	Transaction *testDetail `json:"transaction"`
	User        *testUser   `json:"user"`
}

func testUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func fixture(t *testing.T) *testFixture {
	t.Helper()
	dsn := os.Getenv("WALLETLEDGER_TEST_ADMIN_URL")
	if dsn == "" {
		t.Fatal("WALLETLEDGER_TEST_ADMIN_URL is required: these checks use real PostgreSQL and never skip database verification")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("WALLETLEDGER_TEST_ADMIN_URL must be a valid PostgreSQL URL")
	}
	if cfg.ConnConfig.Database != "postgres" {
		t.Fatal("test administrator URL must target postgres; only newly owned walletledger_test_UUID databases may be created or dropped")
	}
	cfg.MaxConns = 2
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("cannot create PostgreSQL test administrator pool")
	}
	name := "walletledger_test_" + strings.ReplaceAll(testUUID(), "-", "")
	identifier := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
		admin.Close()
		t.Fatalf("cannot create isolated database: %s", safeTestError(err))
	}
	owned := cfg.Copy()
	owned.ConnConfig.Database = name
	owned.MaxConns = 16
	owned.ConnConfig.RuntimeParams["statement_timeout"] = "15000"
	owned.ConnConfig.RuntimeParams["lock_timeout"] = "10000"
	pool, err := pgxpool.NewWithConfig(ctx, owned)
	if err != nil {
		t.Fatal("cannot create isolated test pool")
	}
	f := &testFixture{pool: pool}
	t.Cleanup(func() {
		if f.http != nil {
			f.http.Close()
		}
		pool.Close()
		cleanup, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		if !strings.HasPrefix(name, "walletledger_test_") || len(name) != len("walletledger_test_")+32 {
			t.Error("refusing cleanup outside owned test database namespace")
		} else if _, err := admin.Exec(cleanup, "DROP DATABASE "+identifier); err != nil {
			t.Errorf("owned test database cleanup failed: %s", safeTestError(err))
		}
		admin.Close()
	})
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migration failed: %s", safeTestError(err))
	}
	f.server = New(pool, Config{AllowedOrigins: []string{testOrigin}, SessionHours: 12, ReceiptSecret: syntheticReceiptSecret})
	f.http = httptest.NewServer(f.server.Handler())
	f.admin = f.setup(t)
	return f
}

func safeTestError(err error) string {
	if err == nil {
		return ""
	}
	// PostgreSQL errors contain the failure code and constraint without printing a connection URL.
	if strings.Contains(err.Error(), "://") {
		return "PostgreSQL connection failed (connection URL withheld)"
	}
	return err.Error()
}

func newTestActor() *testActor {
	jar, _ := cookiejar.New(nil)
	return &testActor{client: &http.Client{Jar: jar, Timeout: 20 * time.Second}}
}

func (f *testFixture) request(a *testActor, method, path, key string, body any, change func(*http.Request)) testReply {
	var payload []byte
	if raw, ok := body.(json.RawMessage); ok {
		payload = raw
	} else if body != nil {
		payload, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, f.http.URL+path, bytes.NewReader(payload))
	if err != nil {
		return testReply{body: []byte(err.Error())}
	}
	req.Header.Set("Origin", testOrigin)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if a.auth.CSRF != "" {
		req.Header.Set("X-CSRF-Token", a.auth.CSRF)
	}
	if a.auth.User.ID != "" {
		req.Header.Set("X-Wallet-Actor", a.auth.User.ID)
		req.Header.Set("X-Wallet-Workspace", a.auth.Workspace.ID)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	if change != nil {
		change(req)
	}
	res, err := a.client.Do(req)
	if err != nil {
		return testReply{body: []byte("HTTP request failed: " + err.Error())}
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return testReply{body: []byte("HTTP body read failed")}
	}
	return testReply{status: res.StatusCode, body: data, header: res.Header.Clone()}
}

func requireStatus(t *testing.T, r testReply, status int) {
	t.Helper()
	if r.status != status {
		t.Fatalf("HTTP status %d, expected %d: %s", r.status, status, r.body)
	}
}
func decode[T any](t *testing.T, r testReply) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(r.body, &out); err != nil {
		t.Fatalf("invalid JSON response: %v; %s", err, r.body)
	}
	return out
}
func requireCode(t *testing.T, r testReply, status int, code string) {
	t.Helper()
	requireStatus(t, r, status)
	got := decode[map[string]any](t, r)
	if got["code"] != code {
		t.Fatalf("error code %v, expected %s: %s", got["code"], code, r.body)
	}
}
func (f *testFixture) csrf(t *testing.T, a *testActor) {
	t.Helper()
	r := f.request(a, "GET", "/api/auth/csrf", "", nil, nil)
	requireStatus(t, r, 200)
	a.auth.CSRF = decode[struct {
		CSRF string `json:"csrf_token"`
	}](t, r).CSRF
	if a.auth.CSRF == "" {
		t.Fatal("missing CSRF token")
	}
}
func (f *testFixture) setup(t *testing.T) *testActor {
	t.Helper()
	a := newTestActor()
	f.csrf(t, a)
	r := f.request(a, "POST", "/api/auth/setup", "", map[string]string{"workspace_name": "Synthetic wallet test workspace", "full_name": "Synthetic Administrator", "email": "admin@walletledger.test", "password": syntheticPassword}, nil)
	requireStatus(t, r, 201)
	a.auth = decode[testAuth](t, r)
	return a
}
func (f *testFixture) member(t *testing.T, name string) *testActor {
	t.Helper()
	email := strings.ToLower(strings.ReplaceAll(name, " ", "-")) + "-" + testUUID() + "@walletledger.test"
	r := f.request(f.admin, "POST", "/api/people", testUUID(), map[string]string{"full_name": name, "email": email, "password": syntheticPassword}, nil)
	requireStatus(t, r, 201)
	created := decode[testMutation](t, r)
	if created.User == nil || created.User.Role != "Member" {
		t.Fatal("administrator must create Member accounts only")
	}
	a := newTestActor()
	f.csrf(t, a)
	r = f.request(a, "POST", "/api/auth/login", "", map[string]string{"email": email, "password": syntheticPassword}, nil)
	requireStatus(t, r, 200)
	a.auth = decode[testAuth](t, r)
	return a
}
func (f *testFixture) transfer(a *testActor, recipient, amount, key string) testReply {
	return f.request(a, "POST", "/api/transfers", key, map[string]string{"recipient_id": recipient, "amount_minor": amount, "note": "Synthetic integration transfer"}, nil)
}
func (f *testFixture) fund(t *testing.T, a *testActor, amount string) testDetail {
	t.Helper()
	r := f.request(f.admin, "POST", "/api/allocations", testUUID(), map[string]string{"recipient_id": a.auth.User.ID, "amount_minor": amount, "note": "Synthetic test balance allocation"}, nil)
	requireStatus(t, r, 201)
	m := decode[testMutation](t, r)
	if m.Transaction == nil {
		t.Fatal("allocation must return journal detail")
	}
	return *m.Transaction
}
func (f *testFixture) balance(t *testing.T, a *testActor) string {
	t.Helper()
	r := f.request(a, "GET", "/api/wallets", "", nil, nil)
	requireStatus(t, r, 200)
	var out struct {
		Items []struct {
			User    *string `json:"user_id"`
			Balance string  `json:"balance_minor"`
		} `json:"items"`
	}
	out = decode[struct {
		Items []struct {
			User    *string `json:"user_id"`
			Balance string  `json:"balance_minor"`
		} `json:"items"`
	}](t, r)
	for _, w := range out.Items {
		if w.User != nil && *w.User == a.auth.User.ID {
			return w.Balance
		}
	}
	t.Fatal("own wallet missing")
	return ""
}
func (f *testFixture) walletID(t *testing.T, a *testActor) string {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(context.Background(), "SELECT id::text FROM wallets WHERE user_id=$1", a.auth.User.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func (f *testFixture) count(t *testing.T, table string) int64 {
	t.Helper()
	var n int64
	allowed := map[string]bool{"transactions": true, "journal_entries": true, "receipts": true, "audit_events": true, "users": true, "wallets": true}
	if !allowed[table] {
		t.Fatal("unapproved test table")
	}
	if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func (f *testFixture) consistent(t *testing.T) {
	t.Helper()
	r := f.request(f.admin, "GET", "/api/reconciliation", "", nil, nil)
	requireStatus(t, r, 200)
	out := decode[struct {
		Balanced bool   `json:"balanced"`
		Debit    string `json:"total_debit_minor"`
		Credit   string `json:"total_credit_minor"`
		Total    string `json:"total_wallet_balance_minor"`
		Items    []struct {
			Difference string `json:"difference_minor"`
		} `json:"items"`
	}](t, r)
	if !out.Balanced || out.Debit != out.Credit || out.Total != "0" {
		t.Fatalf("reconciliation failure: %s", r.body)
	}
	for _, item := range out.Items {
		if item.Difference != "0" {
			t.Fatalf("wallet cache differs from journal: %s", r.body)
		}
	}
}
func (f *testFixture) requireBalance(t *testing.T, a *testActor, want string) {
	t.Helper()
	if got := f.balance(t, a); got != want {
		t.Fatalf("balance=%s, want=%s", got, want)
	}
}
func requireTwoLegs(t *testing.T, d testDetail, amount string) {
	t.Helper()
	if len(d.Entries) != 2 || d.Debit != amount || d.Credit != amount || d.Entries[0].Wallet == d.Entries[1].Wallet {
		t.Fatalf("unbalanced journal detail: %+v", d)
	}
	counts := map[string]int{}
	for _, e := range d.Entries {
		counts[e.Direction]++
		if e.Amount != amount {
			t.Fatal("journal amount changed")
		}
	}
	if counts["DEBIT"] != 1 || counts["CREDIT"] != 1 {
		t.Fatal("journal must have one debit and one credit")
	}
}
func describeReply(r testReply) string { return fmt.Sprintf("status=%d body=%s", r.status, r.body) }
