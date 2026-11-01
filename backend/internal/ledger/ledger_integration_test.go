package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestAllocationTransferAndConsistentJournal(t *testing.T) {
	f := fixture(t)
	sender := f.member(t, "Sender")
	receiver := f.member(t, "Receiver")
	f.requireBalance(t, sender, "0")
	allocation := f.fund(t, sender, "125000")
	requireTwoLegs(t, allocation, "125000")
	r := f.transfer(sender, receiver.auth.User.ID, "42000", testUUID())
	requireStatus(t, r, 201)
	m := decode[testMutation](t, r)
	if m.Replayed || m.Transaction == nil {
		t.Fatal("new transfer must return non-replayed transaction")
	}
	requireTwoLegs(t, *m.Transaction, "42000")
	if m.Transaction.Transaction.Kind != "TRANSFER" || m.Transaction.Transaction.From != f.walletID(t, sender) || m.Transaction.Transaction.To != f.walletID(t, receiver) {
		t.Fatal("wrong transfer direction")
	}
	f.requireBalance(t, sender, "83000")
	f.requireBalance(t, receiver, "42000")
	f.consistent(t)
}

func TestConcurrentOverspendHasExactlyOneWinner(t *testing.T) {
	f := fixture(t)
	sender := f.member(t, "Limited sender")
	receiver := f.member(t, "Recipient")
	f.fund(t, sender, "100")
	start := make(chan struct{})
	replies := make(chan testReply, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			replies <- f.transfer(sender, receiver.auth.User.ID, "80", testUUID())
		}()
	}
	close(start)
	wg.Wait()
	close(replies)
	winners := 0
	for r := range replies {
		if r.status == 201 {
			winners++
		} else {
			requireCode(t, r, 409, "INSUFFICIENT_FUNDS")
		}
	}
	if winners != 1 {
		t.Fatalf("overspend accepted %d transfers", winners)
	}
	f.requireBalance(t, sender, "20")
	f.requireBalance(t, receiver, "80")
	if f.count(t, "transactions") != 2 || f.count(t, "journal_entries") != 4 {
		t.Fatal("denied transfers wrote journal state")
	}
	f.consistent(t)
}

func TestOppositeTransfersCompleteWithoutDeadlock(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Alpha")
	b := f.member(t, "Beta")
	f.fund(t, a, "10000")
	f.fund(t, b, "10000")
	start := make(chan struct{})
	replies := make(chan testReply, 16)
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if i%2 == 0 {
				replies <- f.transfer(a, b.auth.User.ID, "100", testUUID())
			} else {
				replies <- f.transfer(b, a.auth.User.ID, "100", testUUID())
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(replies)
	for r := range replies {
		requireStatus(t, r, 201)
	}
	f.requireBalance(t, a, "10000")
	f.requireBalance(t, b, "10000")
	if f.count(t, "transactions") != 18 {
		t.Fatal("opposite transfer lost a transaction")
	}
	f.consistent(t)
}

func TestConcurrentIdempotencyWritesOneTransaction(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Sender")
	b := f.member(t, "Receiver")
	f.fund(t, a, "1000")
	key := testUUID()
	start := make(chan struct{})
	replies := make(chan testReply, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; replies <- f.transfer(a, b.auth.User.ID, "250", key) }()
	}
	close(start)
	wg.Wait()
	close(replies)
	first := ""
	original := 0
	for r := range replies {
		requireStatus(t, r, 201)
		m := decode[testMutation](t, r)
		if m.Transaction == nil || m.RequestID != key {
			t.Fatal("missing idempotency receipt")
		}
		if !m.Replayed {
			original++
		}
		if first == "" {
			first = m.Transaction.Transaction.ID
		}
		if m.Transaction.Transaction.ID != first {
			t.Fatal("same request created several transfers")
		}
	}
	if original != 1 {
		t.Fatalf("expected one original receipt, got %d", original)
	}
	f.requireBalance(t, a, "750")
	f.requireBalance(t, b, "250")
	if f.count(t, "transactions") != 2 {
		t.Fatal("same idempotency key changed journal twice")
	}
	f.consistent(t)
}

func TestIdempotencyCanonicalPayloadAndActorNamespace(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Alpha")
	b := f.member(t, "Beta")
	f.fund(t, a, "1000")
	f.fund(t, b, "1000")
	key := testUUID()
	r := f.transfer(a, b.auth.User.ID, "100", key)
	requireStatus(t, r, 201)
	id := decode[testMutation](t, r).Transaction.Transaction.ID
	r = f.request(a, "POST", "/api/transfers", key, json.RawMessage(`{ "note":"Synthetic integration transfer", "amount_minor":"100", "recipient_id":"`+b.auth.User.ID+`" }`), nil)
	requireStatus(t, r, 201)
	replay := decode[testMutation](t, r)
	if !replay.Replayed || replay.Transaction.Transaction.ID != id {
		t.Fatal("canonical reordered JSON did not replay original command")
	}
	r = f.transfer(a, b.auth.User.ID, "101", key)
	requireCode(t, r, 409, "IDEMPOTENCY_CONFLICT")
	r = f.transfer(b, a.auth.User.ID, "100", key)
	requireStatus(t, r, 201)
	if decode[testMutation](t, r).Replayed {
		t.Fatal("actor-scoped key collided with another account")
	}
	f.requireBalance(t, a, "1000")
	f.requireBalance(t, b, "1000")
	f.consistent(t)
}

func TestInsufficientFundsReceiptIsStableAfterLaterAllocation(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Unfunded")
	b := f.member(t, "Receiver")
	key := testUUID()
	r := f.transfer(a, b.auth.User.ID, "500", key)
	requireCode(t, r, 409, "INSUFFICIENT_FUNDS")
	original := string(r.body)
	beforeAudit := f.count(t, "audit_events")
	f.fund(t, a, "1000")
	afterFunding := f.count(t, "transactions")
	r = f.transfer(a, b.auth.User.ID, "500", key)
	requireCode(t, r, 409, "INSUFFICIENT_FUNDS")
	if string(r.body) != original || r.header.Get("Idempotency-Replayed") != "true" {
		t.Fatal("business denial receipt changed after balance changed")
	}
	if f.count(t, "transactions") != afterFunding || f.count(t, "audit_events") != beforeAudit+1 {
		t.Fatal("replayed denial changed financial/audit state")
	}
	f.requireBalance(t, a, "1000")
	f.requireBalance(t, b, "0")
	r = f.transfer(a, b.auth.User.ID, "500", testUUID())
	requireStatus(t, r, 201)
	f.consistent(t)
}

func TestReplayReceiptDoesNotReturnCurrentBalance(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Sender")
	b := f.member(t, "Receiver")
	f.fund(t, a, "1000")
	key := testUUID()
	r := f.transfer(a, b.auth.User.ID, "100", key)
	requireStatus(t, r, 201)
	original := decode[testMutation](t, r)
	requireStatus(t, f.transfer(a, b.auth.User.ID, "200", testUUID()), 201)
	r = f.transfer(a, b.auth.User.ID, "100", key)
	requireStatus(t, r, 201)
	replay := decode[testMutation](t, r)
	if !replay.Replayed || replay.Transaction.Transaction.ID != original.Transaction.Transaction.ID || replay.Transaction.Transaction.Amount != "100" {
		t.Fatal("historical receipt changed")
	}
	f.requireBalance(t, a, "700")
	f.requireBalance(t, b, "300")
	f.consistent(t)
}

func TestReceiptReplaysAfterServerRestartWithStableSecret(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Restart sender")
	b := f.member(t, "Restart receiver")
	f.fund(t, a, "1000")
	key := testUUID()
	r := f.transfer(a, b.auth.User.ID, "250", key)
	requireStatus(t, r, 201)
	original := decode[testMutation](t, r)
	f.http.Close()
	f.server = New(f.pool, Config{AllowedOrigins: []string{testOrigin}, SessionHours: 12, ReceiptSecret: syntheticReceiptSecret})
	f.http = httptest.NewServer(f.server.Handler())
	r = f.transfer(a, b.auth.User.ID, "250", key)
	requireStatus(t, r, 201)
	replay := decode[testMutation](t, r)
	if !replay.Replayed || replay.Transaction.Transaction.ID != original.Transaction.Transaction.ID {
		t.Fatal("restart lost stable idempotency fingerprint")
	}
	f.requireBalance(t, a, "750")
	f.requireBalance(t, b, "250")
	if f.count(t, "transactions") != 2 {
		t.Fatal("restart duplicated accepted transfer")
	}
	f.consistent(t)
}

func TestRollbackAtEveryFinancialBoundary(t *testing.T) {
	for _, stage := range []string{"after_debit", "after_journal", "before_commit"} {
		t.Run(stage, func(t *testing.T) {
			f := fixture(t)
			a := f.member(t, "Sender")
			b := f.member(t, "Receiver")
			f.fund(t, a, "1000")
			counts := map[string]int64{}
			for _, table := range []string{"transactions", "journal_entries", "receipts", "audit_events"} {
				counts[table] = f.count(t, table)
			}
			key := testUUID()
			f.server.failpoint = func(current string) error {
				if current == stage {
					return errors.New("synthetic rollback injection")
				}
				return nil
			}
			r := f.transfer(a, b.auth.User.ID, "200", key)
			if r.status < 500 {
				t.Fatalf("injected failure accepted: %s", describeReply(r))
			}
			f.server.failpoint = nil
			for table, n := range counts {
				if f.count(t, table) != n {
					t.Fatalf("rollback left %s rows", table)
				}
			}
			f.requireBalance(t, a, "1000")
			f.requireBalance(t, b, "0")
			f.consistent(t)
			r = f.transfer(a, b.auth.User.ID, "200", key)
			requireStatus(t, r, 201)
			if decode[testMutation](t, r).Replayed {
				t.Fatal("rolled-back attempt left an accepted receipt")
			}
			f.requireBalance(t, a, "800")
			f.requireBalance(t, b, "200")
			f.consistent(t)
		})
	}
}

func TestIntegerAmountsRejectAmbiguousJSON(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Sender")
	b := f.member(t, "Receiver")
	f.fund(t, a, "1000000000")
	for _, raw := range []string{`0`, `1`, `1.5`, `"0"`, `"01"`, `"+1"`, `"-1"`, `"1.0"`, `"1e3"`, `" 1"`, `"1 "`, `"1000000001"`, `"9223372036854775808"`, `""`, `null`, `true`} {
		t.Run(strings.ReplaceAll(raw, "\"", "q"), func(t *testing.T) {
			r := f.request(a, "POST", "/api/transfers", testUUID(), json.RawMessage(`{"recipient_id":"`+b.auth.User.ID+`","amount_minor":`+raw+`,"note":"Integer validation test"}`), nil)
			requireStatus(t, r, 422)
		})
	}
	f.requireBalance(t, a, "1000000000")
	f.requireBalance(t, b, "0")
	r := f.transfer(a, b.auth.User.ID, "1000000000", testUUID())
	requireStatus(t, r, 201)
	requireTwoLegs(t, *decode[testMutation](t, r).Transaction, "1000000000")
	f.consistent(t)
}

func TestMalformedAndUnknownCommandFieldsHaveNoReceipt(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Sender")
	b := f.member(t, "Receiver")
	f.fund(t, a, "10")
	n := f.count(t, "receipts")
	for _, body := range []json.RawMessage{
		json.RawMessage(`{"recipient_id":"` + b.auth.User.ID + `","amount_minor":"1","note":"Unknown field","balance_minor":"99"}`),
		json.RawMessage(`{"recipient_id":"not-a-uuid","amount_minor":"1","note":"Invalid recipient"}`),
		json.RawMessage(`{"recipient_id":"` + b.auth.User.ID + `","amount_minor":"1","note":""}`),
		json.RawMessage(`{"recipient_id":"` + b.auth.User.ID + `","amount_minor":"1","note":"Trailing JSON"} {}`),
	} {
		r := f.request(a, "POST", "/api/transfers", testUUID(), body, nil)
		requireStatus(t, r, 422)
	}
	if f.count(t, "receipts") != n {
		t.Fatal("validation errors must not become durable business receipts")
	}
	f.consistent(t)
}

func TestFinancialWritesRequireIdempotencyUUID(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Sender")
	b := f.member(t, "Receiver")
	f.fund(t, a, "10")
	before := f.count(t, "transactions")
	for _, key := range []string{"", "not-a-uuid", strings.Repeat("x", 128)} {
		requireStatus(t, f.transfer(a, b.auth.User.ID, "1", key), 422)
	}
	if f.count(t, "transactions") != before {
		t.Fatal("invalid idempotency key mutated journal")
	}
	f.consistent(t)
}

func TestSelfTransferAndUnknownRecipientDoNotMutate(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Sender")
	f.fund(t, a, "10")
	before := f.count(t, "transactions")
	r := f.transfer(a, a.auth.User.ID, "1", testUUID())
	if r.status != 422 && r.status != 409 {
		t.Fatalf("self transfer accepted: %s", describeReply(r))
	}
	r = f.transfer(a, testUUID(), "1", testUUID())
	requireStatus(t, r, 404)
	if f.count(t, "transactions") != before {
		t.Fatal("invalid recipient mutated journal")
	}
	f.requireBalance(t, a, "10")
	f.consistent(t)
}

func TestReversalCreatesBalancedNewTransactionExactlyOnce(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Original sender")
	b := f.member(t, "Original receiver")
	f.fund(t, a, "1000")
	r := f.transfer(a, b.auth.User.ID, "400", testUUID())
	requireStatus(t, r, 201)
	original := decode[testMutation](t, r).Transaction.Transaction
	path := "/api/transactions/" + original.ID + "/reverse"
	key := testUUID()
	body := map[string]string{"reason": "Synthetic correction of original transfer"}
	r = f.request(f.admin, "POST", path, key, body, nil)
	requireStatus(t, r, 201)
	reverse := decode[testMutation](t, r)
	if reverse.Transaction == nil || reverse.Transaction.Transaction.Kind != "REVERSAL" || reverse.Transaction.Transaction.ID == original.ID || reverse.Transaction.Transaction.ReversalOf == nil || *reverse.Transaction.Transaction.ReversalOf != original.ID {
		t.Fatal("reversal did not retain original transaction relation")
	}
	requireTwoLegs(t, *reverse.Transaction, "400")
	f.requireBalance(t, a, "1000")
	f.requireBalance(t, b, "0")
	r = f.request(f.admin, "GET", "/api/transactions/"+original.ID, "", nil, nil)
	requireStatus(t, r, 200)
	detail := decode[testDetail](t, r)
	if detail.Transaction.ReversalID == nil || *detail.Transaction.ReversalID != reverse.Transaction.Transaction.ID {
		t.Fatal("original transaction does not expose derived reversal relation")
	}
	r = f.request(f.admin, "POST", path, key, body, nil)
	requireStatus(t, r, 201)
	if !decode[testMutation](t, r).Replayed {
		t.Fatal("repeat reversal did not replay")
	}
	r = f.request(f.admin, "POST", path, testUUID(), body, nil)
	requireCode(t, r, 409, "ALREADY_REVERSED")
	if f.count(t, "transactions") != 3 {
		t.Fatal("double reversal wrote another transaction")
	}
	f.consistent(t)
}

func TestReversalDenialIsAtomicAndItsReceiptStable(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Sender")
	b := f.member(t, "Receiver")
	c := f.member(t, "Third party")
	f.fund(t, a, "500")
	r := f.transfer(a, b.auth.User.ID, "400", testUUID())
	requireStatus(t, r, 201)
	original := decode[testMutation](t, r).Transaction.Transaction
	requireStatus(t, f.transfer(b, c.auth.User.ID, "400", testUUID()), 201)
	before := f.count(t, "transactions")
	key := testUUID()
	path := "/api/transactions/" + original.ID + "/reverse"
	body := map[string]string{"reason": "Receiver has already spent the synthetic amount"}
	r = f.request(f.admin, "POST", path, key, body, nil)
	requireCode(t, r, 409, "INSUFFICIENT_FUNDS")
	denied := string(r.body)
	if f.count(t, "transactions") != before {
		t.Fatal("insufficient reversal wrote transaction")
	}
	f.fund(t, b, "400")
	r = f.request(f.admin, "POST", path, key, body, nil)
	requireCode(t, r, 409, "INSUFFICIENT_FUNDS")
	if string(r.body) != denied || r.header.Get("Idempotency-Replayed") != "true" {
		t.Fatal("reversal denial receipt changed")
	}
	r = f.request(f.admin, "POST", path, testUUID(), body, nil)
	requireStatus(t, r, 201)
	f.consistent(t)
}

func TestConcurrentReversalsHaveOneWinner(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Sender")
	b := f.member(t, "Receiver")
	f.fund(t, a, "500")
	r := f.transfer(a, b.auth.User.ID, "250", testUUID())
	requireStatus(t, r, 201)
	id := decode[testMutation](t, r).Transaction.Transaction.ID
	start := make(chan struct{})
	replies := make(chan testReply, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			replies <- f.request(f.admin, "POST", "/api/transactions/"+id+"/reverse", testUUID(), map[string]string{"reason": "Concurrent synthetic reversal test"}, nil)
		}()
	}
	close(start)
	wg.Wait()
	close(replies)
	winners := 0
	for r := range replies {
		if r.status == 201 {
			winners++
		} else {
			requireCode(t, r, 409, "ALREADY_REVERSED")
		}
	}
	if winners != 1 || f.count(t, "transactions") != 3 {
		t.Fatalf("reversal winners=%d", winners)
	}
	f.consistent(t)
}

func TestOnlyTransfersCanBeReversedAndReasonIsValidated(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Sender")
	b := f.member(t, "Receiver")
	allocation := f.fund(t, a, "100")
	r := f.request(f.admin, "POST", "/api/transactions/"+allocation.Transaction.ID+"/reverse", testUUID(), map[string]string{"reason": "Synthetic allocation cannot be reversed"}, nil)
	if r.status != 409 && r.status != 422 {
		t.Fatalf("allocation reversal accepted: %s", describeReply(r))
	}
	r = f.transfer(a, b.auth.User.ID, "25", testUUID())
	requireStatus(t, r, 201)
	id := decode[testMutation](t, r).Transaction.Transaction.ID
	r = f.request(f.admin, "POST", "/api/transactions/"+id+"/reverse", testUUID(), map[string]string{"reason": "short"}, nil)
	requireStatus(t, r, 422)
	f.consistent(t)
}

func TestTransactionPaginationIsDescendingPrivateAndNonDuplicating(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Visible")
	b := f.member(t, "Other")
	c := f.member(t, "Unrelated")
	f.fund(t, a, "100")
	f.fund(t, c, "100")
	for range 5 {
		requireStatus(t, f.transfer(a, b.auth.User.ID, "1", testUUID()), 201)
	}
	seen := map[string]bool{}
	before := ""
	lastSeq := int64(1 << 62)
	for {
		path := "/api/transactions?limit=2"
		if before != "" {
			path += "&before=" + before
		}
		r := f.request(a, "GET", path, "", nil, nil)
		requireStatus(t, r, 200)
		out := decode[struct {
			Items []testTransaction `json:"items"`
			Next  *int64            `json:"next_before"`
		}](t, r)
		for _, txn := range out.Items {
			if seen[txn.ID] || txn.Seq >= lastSeq {
				t.Fatal("pagination repeated/out-of-order transaction")
			}
			seen[txn.ID] = true
			lastSeq = txn.Seq
			if txn.From != f.walletID(t, a) && txn.To != f.walletID(t, a) {
				t.Fatal("history disclosed unrelated wallet")
			}
		}
		if out.Next == nil {
			break
		}
		before = json.Number(stringifyInt(*out.Next)).String()
		if len(seen) > 20 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != 6 {
		t.Fatalf("member history has %d items, expected6", len(seen))
	}
	f.consistent(t)
}

func stringifyInt(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestReconciliationOneSnapshotDuringConcurrentTransfers(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Alpha")
	b := f.member(t, "Beta")
	f.fund(t, a, "10000")
	f.fund(t, b, "10000")
	finished := make(chan struct{})
	errors := make(chan testReply, 1)
	go func() {
		defer close(finished)
		for range 12 {
			r := f.transfer(a, b.auth.User.ID, "1", testUUID())
			if r.status != 201 {
				errors <- r
				return
			}
			r = f.transfer(b, a.auth.User.ID, "1", testUUID())
			if r.status != 201 {
				errors <- r
				return
			}
		}
	}()
	for range 12 {
		f.consistent(t)
	}
	<-finished
	select {
	case r := <-errors:
		t.Fatal(describeReply(r))
	default:
	}
	f.requireBalance(t, a, "10000")
	f.requireBalance(t, b, "10000")
	f.consistent(t)
}

func TestAuditHasOneEventPerAcceptedCommandAndNoSecrets(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Sender")
	b := f.member(t, "Receiver")
	initial := f.count(t, "audit_events")
	f.fund(t, a, "100")
	key := testUUID()
	requireStatus(t, f.transfer(a, b.auth.User.ID, "25", key), 201)
	requireStatus(t, f.transfer(a, b.auth.User.ID, "25", key), 201)
	requireCode(t, f.transfer(a, b.auth.User.ID, "999", testUUID()), 409, "INSUFFICIENT_FUNDS")
	if f.count(t, "audit_events") != initial+2 {
		t.Fatal("replay or denial appended an audit event")
	}
	r := f.request(f.admin, "GET", "/api/audit?limit=2", "", nil, nil)
	requireStatus(t, r, 200)
	out := decode[struct {
		Items []map[string]any `json:"items"`
		Next  *int64           `json:"next_before"`
	}](t, r)
	if len(out.Items) != 2 || out.Next == nil {
		t.Fatal("audit pagination missing")
	}
	for _, forbidden := range []string{syntheticPassword, "csrf_token", "password_hash", "token_hash"} {
		if strings.Contains(string(r.body), forbidden) {
			t.Fatal("audit leaked credential material")
		}
	}
	f.consistent(t)
}

func TestRequestBodyLimitPreventsFinancialChanges(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Sender")
	b := f.member(t, "Receiver")
	f.fund(t, a, "100")
	before := f.count(t, "transactions")
	r := f.request(a, "POST", "/api/transfers", testUUID(), map[string]string{"recipient_id": b.auth.User.ID, "amount_minor": "1", "note": strings.Repeat("z", 70<<10)}, nil)
	requireStatus(t, r, 413)
	if f.count(t, "transactions") != before {
		t.Fatal("oversized body changed financial state")
	}
	f.consistent(t)
}

func TestMigrationsAreRepeatableAndChecksumProtected(t *testing.T) {
	f := fixture(t)
	if err := Migrate(context.Background(), f.pool); err != nil {
		t.Fatalf("second migration failed: %s", safeTestError(err))
	}
	f.consistent(t)
	if _, err := f.pool.Exec(context.Background(), "UPDATE migration_versions SET checksum=repeat('0',64)"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(context.Background(), f.pool); err == nil {
		t.Fatal("migration checksum drift accepted")
	}
}

func TestAuthenticatedReceiptCannotBypassCurrentIdentity(t *testing.T) {
	f := fixture(t)
	a := f.member(t, "Sender")
	b := f.member(t, "Receiver")
	f.fund(t, a, "100")
	key := testUUID()
	requireStatus(t, f.transfer(a, b.auth.User.ID, "1", key), 201)
	r := f.request(a, "POST", "/api/transfers", key, map[string]string{"recipient_id": b.auth.User.ID, "amount_minor": "1", "note": "Synthetic integration transfer"}, func(req *http.Request) { req.Header.Set("X-Wallet-Actor", b.auth.User.ID) })
	requireCode(t, r, 403, "IDENTITY_CHANGED")
	f.requireBalance(t, a, "99")
	f.requireBalance(t, b, "1")
	f.consistent(t)
}
