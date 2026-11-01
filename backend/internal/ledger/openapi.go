package ledger

import "net/http"

func ref(name string) map[string]any   { return map[string]any{"$ref": "#/components/schemas/" + name} }
func field(kind string) map[string]any { return map[string]any{"type": kind} }
func nullable(value map[string]any) map[string]any {
	return map[string]any{"anyOf": []any{value, field("null")}}
}
func object(properties map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
}
func array(item map[string]any) map[string]any { return map[string]any{"type": "array", "items": item} }
func itemsSchema(item map[string]any) map[string]any {
	return object(map[string]any{"items": array(item)}, "items")
}
func pageSchema(item map[string]any) map[string]any {
	return object(map[string]any{"items": array(item), "next_before": nullable(field("integer"))}, "items", "next_before")
}

func (s *Server) openapi(w http.ResponseWriter, r *http.Request) {
	uuid := map[string]any{"type": "string", "format": "uuid"}
	timestamp := map[string]any{"type": "string", "format": "date-time"}
	money := map[string]any{"type": "string", "pattern": "^-?(0|[1-9][0-9]*)$", "description": "Integer rupiah encoded as a decimal string; no floating point values."}
	amount := map[string]any{"type": "string", "pattern": "^[1-9][0-9]{0,9}$", "description": "Positive integer rupiah, at most 1000000000. No sign, fractions, exponent or leading zero."}
	password := map[string]any{"type": "string", "writeOnly": true, "format": "password", "description": "12 to 128 UTF-8 bytes; SHA-256/base64 prehash then bcrypt, never truncated."}
	name := map[string]any{"type": "string", "minLength": 1, "maxLength": 120}
	note := map[string]any{"type": "string", "minLength": 1, "maxLength": 500}
	user := object(map[string]any{"id": uuid, "email": field("string"), "full_name": name, "role": map[string]any{"type": "string", "enum": []string{"Admin", "Member"}}}, "id", "email", "full_name", "role")
	schemas := map[string]any{
		"Error":              object(map[string]any{"detail": field("string"), "code": field("string")}, "detail", "code"),
		"AuthUser":           user,
		"Workspace":          object(map[string]any{"id": uuid, "name": field("string")}, "id", "name"),
		"AuthView":           object(map[string]any{"user": ref("AuthUser"), "workspace": ref("Workspace"), "csrf_token": field("string")}, "user", "workspace", "csrf_token"),
		"Setup":              object(map[string]any{"workspace_name": map[string]any{"type": "string", "minLength": 1, "maxLength": 100}, "full_name": name, "email": field("string"), "password": password}, "workspace_name", "full_name", "email", "password"),
		"Credentials":        object(map[string]any{"email": field("string"), "password": password}, "email", "password"),
		"AccountInput":       object(map[string]any{"full_name": name, "email": field("string"), "password": password}, "full_name", "email", "password"),
		"Person":             object(map[string]any{"id": uuid, "full_name": name, "wallet_id": uuid}, "id", "full_name", "wallet_id"),
		"Wallet":             object(map[string]any{"id": uuid, "user_id": nullable(uuid), "owner_name": name, "is_system": field("boolean"), "currency": map[string]any{"type": "string", "enum": []string{"IDR"}}, "balance_minor": money, "created_at": timestamp}, "id", "user_id", "owner_name", "is_system", "currency", "balance_minor", "created_at"),
		"FinancialInput":     object(map[string]any{"recipient_id": uuid, "amount_minor": amount, "note": note}, "recipient_id", "amount_minor", "note"),
		"ReversalInput":      object(map[string]any{"reason": map[string]any{"type": "string", "minLength": 10, "maxLength": 500}}, "reason"),
		"Transaction":        object(map[string]any{"id": uuid, "seq": field("integer"), "kind": map[string]any{"type": "string", "enum": []string{"TRANSFER", "ALLOCATION", "REVERSAL"}}, "amount_minor": amount, "note": note, "created_by": uuid, "created_by_name": name, "from_wallet_id": uuid, "to_wallet_id": uuid, "from_name": name, "to_name": name, "created_at": timestamp, "reversal_of": nullable(uuid), "reversal_id": nullable(uuid)}, "id", "seq", "kind", "amount_minor", "note", "created_by", "created_by_name", "from_wallet_id", "to_wallet_id", "from_name", "to_name", "created_at", "reversal_of", "reversal_id"),
		"JournalEntry":       object(map[string]any{"wallet_id": uuid, "wallet_name": name, "direction": map[string]any{"type": "string", "enum": []string{"DEBIT", "CREDIT"}}, "amount_minor": amount}, "wallet_id", "wallet_name", "direction", "amount_minor"),
		"TransactionDetail":  object(map[string]any{"transaction": ref("Transaction"), "entries": array(ref("JournalEntry")), "debit_total_minor": money, "credit_total_minor": money}, "transaction", "entries", "debit_total_minor", "credit_total_minor"),
		"MutationResult":     object(map[string]any{"request_id": uuid, "replayed": field("boolean"), "transaction": nullable(ref("TransactionDetail")), "user": nullable(ref("AuthUser"))}, "request_id", "replayed", "transaction", "user"),
		"ReconciliationItem": object(map[string]any{"wallet_id": uuid, "owner_name": name, "is_system": field("boolean"), "cached_balance_minor": money, "ledger_balance_minor": money, "difference_minor": money}, "wallet_id", "owner_name", "is_system", "cached_balance_minor", "ledger_balance_minor", "difference_minor"),
		"Reconciliation":     object(map[string]any{"balanced": field("boolean"), "wallet_count": field("integer"), "transaction_count": field("integer"), "total_debit_minor": money, "total_credit_minor": money, "total_wallet_balance_minor": money, "items": array(ref("ReconciliationItem"))}, "balanced", "wallet_count", "transaction_count", "total_debit_minor", "total_credit_minor", "total_wallet_balance_minor", "items"),
		"AuditEvent":         object(map[string]any{"id": uuid, "seq": field("integer"), "actor_name": name, "kind": field("string"), "entity_id": uuid, "created_at": timestamp}, "id", "seq", "actor_name", "kind", "entity_id", "created_at"),
	}
	paths := map[string]any{}
	add := func(path, method, summary string, input, output map[string]any, status string, auth, write, paged bool) {
		responses := map[string]any{}
		for _, code := range []string{"401", "403", "404", "409", "413", "422", "429", "503"} {
			responses[code] = map[string]any{"description": "Rejected, unavailable, or outcome unknown. Retry an unknown mutation only with its original immutable key and body.", "content": map[string]any{"application/json": map[string]any{"schema": ref("Error")}}}
		}
		responses[status] = map[string]any{"description": "Success"}
		if output != nil {
			responses[status] = map[string]any{"description": "Success; mutation replays retain their original status.", "content": map[string]any{"application/json": map[string]any{"schema": output}}}
		}
		op := map[string]any{"summary": summary, "responses": responses}
		if auth {
			op["security"] = []any{map[string]any{"sessionCookie": []string{}}}
		} else {
			op["security"] = []any{}
		}
		parameters := []any{}
		if input != nil {
			op["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": input}}}
			parameters = append(parameters, map[string]any{"name": "Origin", "in": "header", "required": true, "schema": field("string")}, map[string]any{"name": "X-CSRF-Token", "in": "header", "required": true, "schema": field("string")})
		}
		if write {
			parameters = append(parameters, map[string]any{"name": "X-Wallet-Actor", "in": "header", "required": true, "schema": uuid}, map[string]any{"name": "X-Wallet-Workspace", "in": "header", "required": true, "schema": uuid})
			if path != "/auth/logout" {
				parameters = append(parameters, map[string]any{"name": "Idempotency-Key", "in": "header", "required": true, "schema": uuid})
			}
		}
		if path == "/wallets/{id}" || path == "/transactions/{id}" || path == "/transactions/{id}/reverse" {
			parameters = append(parameters, map[string]any{"name": "id", "in": "path", "required": true, "schema": uuid})
		}
		if paged {
			parameters = append(parameters, map[string]any{"name": "before", "in": "query", "schema": map[string]any{"type": "integer", "minimum": 1}}, map[string]any{"name": "limit", "in": "query", "schema": map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "default": 50}})
		}
		if len(parameters) > 0 {
			op["parameters"] = parameters
		}
		item, exists := paths[path].(map[string]any)
		if !exists {
			item = map[string]any{}
			paths[path] = item
		}
		item[method] = op
	}
	add("/health", "get", "Check database readiness", nil, object(map[string]any{"status": field("string")}, "status"), "200", false, false, false)
	add("/auth/status", "get", "Check first-run setup", nil, object(map[string]any{"setup_required": field("boolean"), "workspace_name": nullable(field("string"))}, "setup_required", "workspace_name"), "200", false, false, false)
	add("/auth/csrf", "get", "Issue anonymous CSRF cookie and token", nil, object(map[string]any{"csrf_token": field("string")}, "csrf_token"), "200", false, false, false)
	add("/auth/setup", "post", "Create the single workspace and Admin without opening balance", ref("Setup"), ref("AuthView"), "201", false, false, false)
	add("/auth/login", "post", "Create opaque cookie session; persistent attempt limits enabled", ref("Credentials"), ref("AuthView"), "200", false, false, false)
	add("/auth/me", "get", "Read current account and session CSRF token", nil, ref("AuthView"), "200", true, false, false)
	add("/auth/logout", "post", "Revoke the current session", object(map[string]any{}), nil, "204", true, true, false)
	add("/people", "get", "List names and recipient wallet IDs without balances or emails", nil, itemsSchema(ref("Person")), "200", true, false, false)
	add("/people", "post", "Admin creates a Member account with a zero-balance wallet", ref("AccountInput"), ref("MutationResult"), "201", true, true, false)
	add("/admin/people", "get", "Admin lists accounts", nil, itemsSchema(ref("AuthUser")), "200", true, false, false)
	add("/wallets", "get", "Member sees own wallet; Admin sees all including funding system", nil, itemsSchema(ref("Wallet")), "200", true, false, false)
	add("/wallets/{id}", "get", "Read an authorized wallet; foreign wallets are hidden with 404", nil, ref("Wallet"), "200", true, false, false)
	add("/transfers", "post", "Transfer synthetic balance from the current user's wallet", ref("FinancialInput"), ref("MutationResult"), "201", true, true, false)
	add("/allocations", "post", "Admin allocates synthetic balance from the system wallet", ref("FinancialInput"), ref("MutationResult"), "201", true, true, false)
	add("/transactions", "get", "Read current authorized history page, descending sequence", nil, pageSchema(ref("Transaction")), "200", true, false, true)
	add("/transactions/{id}", "get", "Read authorized transaction and matching debit/credit journal", nil, ref("TransactionDetail"), "200", true, false, false)
	add("/transactions/{id}/reverse", "post", "Admin mirrors a transfer once if its receiver has enough balance", ref("ReversalInput"), ref("MutationResult"), "201", true, true, false)
	add("/reconciliation", "get", "Admin compares balances, journal totals and cache in a repeatable-read transaction", nil, ref("Reconciliation"), "200", true, false, false)
	add("/audit", "get", "Admin reads immutable mutation evidence, descending sequence", nil, pageSchema(ref("AuditEvent")), "200", true, false, true)
	writeJSON(w, 200, map[string]any{"openapi": "3.1.0", "info": map[string]any{"title": "WalletLedger API", "version": "1.0.0", "description": "Synthetic IDR wallet portfolio. All money uses integer decimal strings. No real payments or public production deployment."}, "servers": []any{map[string]any{"url": "/api"}}, "paths": paths, "components": map[string]any{"schemas": schemas, "securitySchemes": map[string]any{"sessionCookie": map[string]any{"type": "apiKey", "in": "cookie", "name": "walletledger_session"}}}})
}
