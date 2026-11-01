package ledger

import (
	"context"
	"testing"
)

func TestNativeSchemaRejectsImmutableHistoryMutationAndTruncate(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Funded member")
	f.fund(t, a, "100")
	for _, table := range []string{"transactions", "journal_entries", "receipts", "audit_events"} {
		for _, operation := range []string{"UPDATE", "DELETE", "TRUNCATE"} {
			t.Run(table+"_"+operation, func(t *testing.T) {
				ctx := context.Background()
				tx, err := f.pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(ctx)
				statement := ""
				switch operation {
				case "UPDATE":
					switch table {
					case "transactions":
						statement = "UPDATE transactions SET note='tampered'"
					case "journal_entries":
						statement = "UPDATE journal_entries SET amount_minor=amount_minor+1"
					case "receipts":
						statement = "UPDATE receipts SET status=200"
					case "audit_events":
						statement = "UPDATE audit_events SET kind='tampered'"
					}
				case "DELETE":
					statement = "DELETE FROM " + table
				case "TRUNCATE":
					statement = "TRUNCATE TABLE " + table + " CASCADE"
				}
				_, err = tx.Exec(ctx, statement)
				if err == nil {
					err = tx.Commit(ctx)
				}
				if err == nil {
					t.Fatalf("native schema accepted %s", statement)
				}
			})
		}
	}
	f.requireBalance(t, a, "100")
	f.consistent(t)
}

func TestDeferredSchemaRejectsCacheDriftAndUnbalancedJournal(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Member")
	f.fund(t, a, "100")
	t.Run("wallet_cache_drift", func(t *testing.T) {
		ctx := context.Background()
		tx, err := f.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		_, err = tx.Exec(ctx, "UPDATE wallets SET balance_minor=balance_minor+1 WHERE user_id=$1", a.auth.User.ID)
		if err == nil {
			err = tx.Commit(ctx)
		}
		if err == nil {
			t.Fatal("database accepted wallet cache without a matching journal")
		}
	})
	t.Run("transaction_without_legs", func(t *testing.T) {
		ctx := context.Background()
		tx, err := f.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		var system string
		if err = tx.QueryRow(ctx, "SELECT id::text FROM wallets WHERE is_system").Scan(&system); err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, `INSERT INTO transactions(id,workspace_id,kind,amount_minor,note,created_by,from_wallet_id,to_wallet_id,request_key)
			VALUES($1,$2,'ALLOCATION',1,'Synthetic missing journal test',$3,$4,$5,$6)`, testUUID(), a.auth.Workspace.ID, f.admin.auth.User.ID, system, f.walletID(t, a), testUUID())
		if err == nil {
			err = tx.Commit(ctx)
		}
		if err == nil {
			t.Fatal("database accepted financial transaction without exactly two balanced journal entries")
		}
	})
	f.requireBalance(t, a, "100")
	f.consistent(t)
}

func TestUserWalletCapIsEnforcedAtomicallyAndDenialReplays(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "At capacity")
	ctx := context.Background()
	var system string
	if err := f.pool.QueryRow(ctx, "SELECT id::text FROM wallets WHERE is_system").Scan(&system); err != nil {
		t.Fatal(err)
	}
	wallet := f.walletID(t, a)
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `WITH inserted AS (
		INSERT INTO transactions(id,workspace_id,kind,amount_minor,note,created_by,from_wallet_id,to_wallet_id,request_key)
		SELECT gen_random_uuid(),$1,'ALLOCATION',1000000000,'Synthetic capacity fixture',$2,$3,$4,gen_random_uuid()
		FROM generate_series(1,1000) RETURNING id,from_wallet_id,to_wallet_id,amount_minor,created_by,request_key
	), evidence AS (
		INSERT INTO receipts(actor_id,request_key,fingerprint,operation,status,response,entity_id)
		SELECT created_by,request_key,repeat('a',64),'ALLOCATION',201,'{}'::jsonb,id FROM inserted
	), audit AS (
		INSERT INTO audit_events(id,actor_id,kind,entity_id)
		SELECT gen_random_uuid(),created_by,'ALLOCATION',id FROM inserted
	)
	INSERT INTO journal_entries(id,transaction_id,wallet_id,direction,amount_minor)
	SELECT gen_random_uuid(),id,from_wallet_id,'DEBIT',amount_minor FROM inserted
	UNION ALL SELECT gen_random_uuid(),id,to_wallet_id,'CREDIT',amount_minor FROM inserted`, a.auth.Workspace.ID, f.admin.auth.User.ID, system, wallet)
	if err != nil {
		t.Fatalf("capacity fixture journals: %s", safeTestError(err))
	}
	if _, err = tx.Exec(ctx, "UPDATE wallets SET balance_minor=CASE WHEN id=$1 THEN -1000000000000 WHEN id=$2 THEN 1000000000000 ELSE balance_minor END WHERE id IN($1,$2)", system, wallet); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatalf("balanced capacity fixture rejected: %s", safeTestError(err))
	}
	f.requireBalance(t, a, "1000000000000")
	f.consistent(t)
	before := f.count(t, "transactions")
	key := testUUID()
	body := map[string]string{"recipient_id": a.auth.User.ID, "amount_minor": "1", "note": "Synthetic capacity boundary"}
	r := f.request(f.admin, "POST", "/api/allocations", key, body, nil)
	requireCode(t, r, 409, "LIMIT_EXCEEDED")
	original := string(r.body)
	r = f.request(f.admin, "POST", "/api/allocations", key, body, nil)
	requireCode(t, r, 409, "LIMIT_EXCEEDED")
	if string(r.body) != original || r.header.Get("Idempotency-Replayed") != "true" {
		t.Fatal("capacity denial did not replay stable receipt")
	}
	if f.count(t, "transactions") != before {
		t.Fatal("capacity denial created a transaction")
	}
	f.requireBalance(t, a, "1000000000000")
	f.consistent(t)
}
