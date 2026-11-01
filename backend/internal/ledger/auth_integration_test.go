package ledger

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestSetupIsOnceAndCreatesNoOpeningMoney(t *testing.T) {
	f := fixture(t)
	if f.count(t, "users") != 1 || f.count(t, "wallets") != 2 || f.count(t, "transactions") != 0 {
		t.Fatal("setup must create one administrator plus user/system wallets with no money")
	}
	r := f.request(f.admin, "GET", "/api/auth/status", "", nil, nil)
	requireStatus(t, r, 200)
	out := decode[struct {
		Required bool   `json:"setup_required"`
		Name     string `json:"workspace_name"`
	}](t, r)
	if out.Required || out.Name != "Synthetic wallet test workspace" {
		t.Fatal("setup status does not describe existing workspace")
	}
	a := newTestActor()
	f.csrf(t, a)
	r = f.request(a, "POST", "/api/auth/setup", "", map[string]string{"workspace_name": "Replacement", "full_name": "Replacement Admin", "email": "replacement@walletledger.test", "password": syntheticPassword}, nil)
	requireStatus(t, r, 409)
	if f.count(t, "users") != 1 {
		t.Fatal("second setup created account")
	}
	f.consistent(t)
}

func TestSessionCookieAndDatabaseStoreOnlyHashedToken(t *testing.T) {
	f := fixture(t)
	r := f.request(f.admin, "GET", "/api/auth/me", "", nil, nil)
	requireStatus(t, r, 200)
	if decode[testAuth](t, r).User.Role != "Admin" {
		t.Fatal("setup did not return administrator identity")
	}
	u, _ := http.NewRequest("GET", f.http.URL, nil)
	var token string
	for _, c := range f.admin.client.Jar.Cookies(u.URL) {
		if c.Name == "walletledger_session" {
			token = c.Value
		}
	}
	if len(token) < 32 {
		t.Fatal("missing opaque authentication cookie")
	}
	var hash string
	if err := f.pool.QueryRow(context.Background(), "SELECT token_hash FROM sessions WHERE user_id=$1", f.admin.auth.User.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == token || strings.Contains(hash, token) {
		t.Fatal("raw session token stored in PostgreSQL")
	}
	a := newTestActor()
	f.csrf(t, a)
	r = f.request(a, "POST", "/api/auth/login", "", map[string]string{"email": f.admin.auth.User.Email, "password": syntheticPassword}, nil)
	requireStatus(t, r, 200)
	found := false
	for _, raw := range r.header.Values("Set-Cookie") {
		if strings.HasPrefix(raw, "walletledger_session=") {
			found = true
			if !strings.Contains(raw, "HttpOnly") || !strings.Contains(raw, "SameSite=Lax") {
				t.Fatal("session cookie lacks HttpOnly/SameSite=Lax")
			}
		}
	}
	if !found {
		t.Fatal("login did not set session cookie")
	}
}

func TestLoginNormalizationAndPasswordBytes(t *testing.T) {
	f := fixture(t)
	password := strings.Repeat("a", 90) + "Synthetic9!"
	email := "long-password@walletledger.test"
	r := f.request(f.admin, "POST", "/api/people", testUUID(), map[string]string{"full_name": "Long password account", "email": email, "password": password}, nil)
	requireStatus(t, r, 201)
	a := newTestActor()
	f.csrf(t, a)
	r = f.request(a, "POST", "/api/auth/login", "", map[string]string{"email": "  LONG-PASSWORD@WALLETLEDGER.TEST  ", "password": password}, nil)
	requireStatus(t, r, 200)
	if decode[testAuth](t, r).User.Email != email {
		t.Fatal("email not normalized")
	}
	b := newTestActor()
	f.csrf(t, b)
	r = f.request(b, "POST", "/api/auth/login", "", map[string]string{"email": email, "password": password[:75] + "different-ending"}, nil)
	requireStatus(t, r, 401)
	for _, bad := range []string{"short", strings.Repeat("a", 129), strings.Repeat("\u9577", 43)} {
		r = f.request(f.admin, "POST", "/api/people", testUUID(), map[string]string{"full_name": "Invalid password", "email": testUUID() + "@walletledger.test", "password": bad}, nil)
		requireStatus(t, r, 422)
	}
}

func TestLoginAttemptLimitIsEnabled(t *testing.T) {
	f := fixture(t)
	a := newTestActor()
	f.csrf(t, a)
	for range 10 {
		r := f.request(a, "POST", "/api/auth/login", "", map[string]string{"email": "missing@walletledger.test", "password": "WrongSyntheticPassword947!"}, nil)
		requireStatus(t, r, 401)
	}
	r := f.request(a, "POST", "/api/auth/login", "", map[string]string{"email": "missing@walletledger.test", "password": "WrongSyntheticPassword947!"}, nil)
	requireStatus(t, r, 429)
	if r.header.Get("Retry-After") == "" {
		t.Fatal("rate limit omitted Retry-After")
	}
	r = f.request(a, "POST", "/api/auth/login", "", map[string]string{"email": f.admin.auth.User.Email, "password": syntheticPassword}, nil)
	requireStatus(t, r, 200)
}

func TestCSRFAndOriginRequiredForAnonymousAndAuthenticatedWrites(t *testing.T) {
	f := fixture(t)
	a := newTestActor()
	f.csrf(t, a)
	login := map[string]string{"email": f.admin.auth.User.Email, "password": syntheticPassword}
	for name, change := range map[string]func(*http.Request){
		"missing_csrf":   func(r *http.Request) { r.Header.Del("X-CSRF-Token") },
		"invalid_csrf":   func(r *http.Request) { r.Header.Set("X-CSRF-Token", "synthetic-invalid-token") },
		"foreign_origin": func(r *http.Request) { r.Header.Set("Origin", "https://foreign.example") },
	} {
		t.Run(name, func(t *testing.T) {
			r := f.request(a, "POST", "/api/auth/login", "", login, change)
			requireStatus(t, r, 403)
		})
	}
	b := f.member(t, "Receiver")
	f.fund(t, f.admin, "10")
	for name, change := range map[string]func(*http.Request){
		"session_missing_csrf":   func(r *http.Request) { r.Header.Del("X-CSRF-Token") },
		"session_invalid_csrf":   func(r *http.Request) { r.Header.Set("X-CSRF-Token", "synthetic-invalid-token") },
		"session_foreign_origin": func(r *http.Request) { r.Header.Set("Origin", "https://foreign.example") },
	} {
		t.Run(name, func(t *testing.T) {
			r := f.request(f.admin, "POST", "/api/transfers", testUUID(), map[string]string{"recipient_id": b.auth.User.ID, "amount_minor": "1", "note": "CSRF boundary"}, change)
			requireStatus(t, r, 403)
		})
	}
	f.requireBalance(t, f.admin, "10")
	f.requireBalance(t, b, "0")
	f.consistent(t)
}

func TestExpectedActorAndWorkspaceAreCheckedBeforeCSRFAndReplay(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Sender")
	b := f.member(t, "Receiver")
	f.fund(t, a, "10")
	for name, change := range map[string]func(*http.Request){
		"wrong_actor":       func(r *http.Request) { r.Header.Set("X-Wallet-Actor", b.auth.User.ID) },
		"wrong_workspace":   func(r *http.Request) { r.Header.Set("X-Wallet-Workspace", testUUID()) },
		"missing_actor":     func(r *http.Request) { r.Header.Del("X-Wallet-Actor") },
		"missing_workspace": func(r *http.Request) { r.Header.Del("X-Wallet-Workspace") },
	} {
		t.Run(name, func(t *testing.T) {
			r := f.request(a, "POST", "/api/transfers", testUUID(), map[string]string{"recipient_id": b.auth.User.ID, "amount_minor": "1", "note": "Identity boundary"}, func(r *http.Request) { r.Header.Set("X-CSRF-Token", "stale-client-token"); change(r) })
			requireCode(t, r, 403, "IDENTITY_CHANGED")
		})
	}
	f.requireBalance(t, a, "10")
	f.consistent(t)
}

func TestLogoutRevokesSessionAndExpiredSessionsCannotRead(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Session owner")
	r := f.request(a, "POST", "/api/auth/logout", "", map[string]any{}, nil)
	requireStatus(t, r, 204)
	r = f.request(a, "GET", "/api/auth/me", "", nil, nil)
	requireStatus(t, r, 401)
	a = newTestActor()
	f.csrf(t, a)
	r = f.request(a, "POST", "/api/auth/login", "", map[string]string{"email": f.admin.auth.User.Email, "password": syntheticPassword}, nil)
	requireStatus(t, r, 200)
	a.auth = decode[testAuth](t, r)
	if _, err := f.pool.Exec(context.Background(), "UPDATE sessions SET expires_at=now()-interval '1 second' WHERE user_id=$1", a.auth.User.ID); err != nil {
		t.Fatal(err)
	}
	r = f.request(a, "GET", "/api/wallets", "", nil, nil)
	requireStatus(t, r, 401)
}

func TestMemberCannotUseAdminRoutesOrReadForeignMoney(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Member")
	b := f.member(t, "Private owner")
	allocation := f.fund(t, b, "100")
	for _, path := range []string{"/api/admin/people", "/api/reconciliation", "/api/audit"} {
		requireStatus(t, f.request(a, "GET", path, "", nil, nil), 403)
	}
	requireStatus(t, f.request(a, "GET", "/api/wallets/"+f.walletID(t, b), "", nil, nil), 404)
	requireStatus(t, f.request(a, "GET", "/api/transactions/"+allocation.Transaction.ID, "", nil, nil), 404)
	r := f.request(a, "POST", "/api/people", testUUID(), map[string]string{"full_name": "Forbidden account", "email": "forbidden@walletledger.test", "password": syntheticPassword}, nil)
	requireStatus(t, r, 403)
	r = f.request(a, "POST", "/api/allocations", testUUID(), map[string]string{"recipient_id": a.auth.User.ID, "amount_minor": "1", "note": "Forbidden allocation"}, nil)
	requireStatus(t, r, 403)
	r = f.request(a, "POST", "/api/transactions/"+allocation.Transaction.ID+"/reverse", testUUID(), map[string]string{"reason": "Forbidden member reversal attempt"}, nil)
	requireStatus(t, r, 403)
	r = f.request(a, "GET", "/api/wallets", "", nil, nil)
	requireStatus(t, r, 200)
	out := decode[struct {
		Items []map[string]any `json:"items"`
	}](t, r)
	if len(out.Items) != 1 {
		t.Fatal("member wallet listing contains foreign wallets")
	}
	f.consistent(t)
}

func TestUnauthenticatedRoutesAndPeoplePrivacy(t *testing.T) {
	f := fixture(t)
	anonymous := newTestActor()
	for _, path := range []string{"/api/wallets", "/api/people", "/api/transactions", "/api/admin/people", "/api/reconciliation", "/api/audit"} {
		requireStatus(t, f.request(anonymous, "GET", path, "", nil, nil), 401)
	}
	a := f.member(t, "Member")
	r := f.request(a, "GET", "/api/people", "", nil, nil)
	requireStatus(t, r, 200)
	out := decode[struct {
		Items []map[string]any `json:"items"`
	}](t, r)
	if len(out.Items) != 2 {
		t.Fatal("recipient directory incomplete")
	}
	for _, person := range out.Items {
		for _, secret := range []string{"email", "password_hash", "password", "balance_minor", "csrf_token"} {
			if _, ok := person[secret]; ok {
				t.Fatalf("recipient directory exposes %s", secret)
			}
		}
	}
}

func TestAuthenticationErrorsDoNotPersistCommandReceipts(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Member")
	n := f.count(t, "receipts")
	r := f.request(a, "POST", "/api/allocations", testUUID(), map[string]string{"recipient_id": a.auth.User.ID, "amount_minor": "1", "note": "Denied role"}, nil)
	requireStatus(t, r, 403)
	r = f.request(a, "POST", "/api/transfers", testUUID(), map[string]string{"recipient_id": f.admin.auth.User.ID, "amount_minor": "1", "note": "Wrong CSRF"}, func(r *http.Request) { r.Header.Set("X-CSRF-Token", "invalid") })
	requireStatus(t, r, 403)
	if f.count(t, "receipts") != n {
		t.Fatal("authentication/authorization failure persisted receipt")
	}
}
