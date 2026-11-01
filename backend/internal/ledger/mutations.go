package ledger

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5"
)

func (s *Server) checkWrite(r *http.Request, admin bool) error {
	i, err := s.authenticate(r.Context(), s.pool, r, false)
	if err != nil {
		return err
	}
	if err = s.authenticatedWrite(r, i); err != nil {
		return err
	}
	if admin {
		if err = requireAdmin(i); err != nil {
			return err
		}
	}
	return nil
}

type mutationAction func(context.Context, pgx.Tx, *identity, string) (*MutationResult, error)

func stableDenial(err error) bool {
	var e *apiError
	if !errors.As(err, &e) || e.Status != 409 {
		return false
	}
	return e.Code == "INSUFFICIENT_FUNDS" || e.Code == "LIMIT_EXCEEDED" || e.Code == "ALREADY_REVERSED"
}

func (s *Server) mutate(w http.ResponseWriter, r *http.Request, admin bool, operation string, body any, action mutationAction) {
	key := r.Header.Get("Idempotency-Key")
	if !validID(key) {
		writeError(w, problem(422, "VALIDATION_ERROR", "Idempotency-Key harus berupa UUID."))
		return
	}
	key = lowerID(key)
	canonical, err := json.Marshal(body)
	if err != nil {
		writeError(w, err)
		return
	}
	mac := hmac.New(sha256.New, []byte(s.cfg.ReceiptSecret))
	_, _ = mac.Write([]byte(r.Method + "\n" + r.URL.Path + "\n" + string(canonical)))
	fingerprint := hex.EncodeToString(mac.Sum(nil))
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
	if err == nil && admin {
		err = requireAdmin(i)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "walletledger:receipt:"+i.User.ID+":"+key); err != nil {
		writeError(w, err)
		return
	}
	var previousFingerprint string
	var status int
	var saved []byte
	err = tx.QueryRow(r.Context(), "SELECT fingerprint,status,response FROM receipts WHERE actor_id=$1 AND request_key=$2", i.User.ID, key).Scan(&previousFingerprint, &status, &saved)
	if err == nil {
		if previousFingerprint != fingerprint {
			writeError(w, problem(409, "IDEMPOTENCY_CONFLICT", "Request ID sudah digunakan untuk isi atau endpoint yang berbeda."))
			return
		}
		var payload map[string]any
		if err = json.Unmarshal(saved, &payload); err != nil {
			writeError(w, err)
			return
		}
		if status == 201 {
			payload["replayed"] = true
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, err)
			return
		}
		w.Header().Set("Idempotency-Replayed", "true")
		writeJSON(w, status, payload)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, err)
		return
	}
	result, actionErr := action(r.Context(), tx, i, key)
	if actionErr != nil && !stableDenial(actionErr) {
		writeError(w, actionErr)
		return
	}
	status = 201
	var response any = result
	var entityID *string
	if actionErr != nil {
		status = 409
		var denial *apiError
		_ = errors.As(actionErr, &denial)
		response = map[string]string{"code": denial.Code, "detail": denial.Detail}
	} else {
		result.RequestID = key
		if result.Transaction != nil {
			id := result.Transaction.Transaction.ID
			entityID = &id
		}
		if result.User != nil {
			id := result.User.ID
			entityID = &id
		}
		if entityID == nil {
			writeError(w, errors.New("mutation has no entity"))
			return
		}
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		writeError(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO receipts(actor_id,request_key,fingerprint,operation,status,response,entity_id)
	  VALUES($1,$2,$3,$4,$5,$6,$7)`, i.User.ID, key, fingerprint, operation, status, encoded, entityID); err != nil {
		writeError(w, err)
		return
	}
	if status == 201 {
		_, err = tx.Exec(r.Context(), "INSERT INTO audit_events(id,actor_id,kind,entity_id) VALUES($1,$2,$3,$4)", newID(), i.User.ID, operation, entityID)
		if err != nil {
			writeError(w, err)
			return
		}
	}
	if s.failpoint != nil {
		if err = s.failpoint("before_commit"); err != nil {
			writeError(w, err)
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, status, response)
}

func (s *Server) createPerson(w http.ResponseWriter, r *http.Request) {
	if err := s.checkWrite(r, true); err != nil {
		writeError(w, err)
		return
	}
	var input accountInput
	if err := decodeBody(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	if err := normalizeAccount(&input); err != nil {
		writeError(w, err)
		return
	}
	s.mutate(w, r, true, "USER_CREATED", input, func(ctx context.Context, tx pgx.Tx, i *identity, key string) (*MutationResult, error) {
		// Account registration is rare; its separate lock enforces the cap without serializing transfers.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('walletledger:accounts',0))"); err != nil {
			return nil, err
		}
		var duplicate bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE workspace_id=$2 AND email=$1)", input.Email, i.Workspace.ID).Scan(&duplicate); err != nil {
			return nil, err
		}
		if duplicate {
			return nil, problem(409, "EMAIL_EXISTS", "Email sudah memiliki akun.")
		}
		hash, err := hashPassword(input.Password)
		if err != nil {
			return nil, err
		}
		user := &AuthUser{ID: newID(), Email: input.Email, FullName: input.FullName, Role: "Member"}
		if _, err = tx.Exec(ctx, "INSERT INTO users(id,workspace_id,email,full_name,role,password_hash) VALUES($1,$2,$3,$4,'Member',$5)", user.ID, i.Workspace.ID, user.Email, user.FullName, hash); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO wallets(id,workspace_id,user_id) VALUES($1,$2,$3)", newID(), i.Workspace.ID, user.ID); err != nil {
			return nil, err
		}
		return &MutationResult{User: user}, nil
	})
}

type financialInput struct {
	RecipientID string `json:"recipient_id"`
	AmountMinor string `json:"amount_minor"`
	Note        string `json:"note"`
}
type reversalInput struct {
	Reason string `json:"reason"`
}

func normalizeFinancial(input *financialInput) (int64, error) {
	if !validID(input.RecipientID) {
		return 0, problem(422, "VALIDATION_ERROR", "Penerima harus berupa UUID pengguna.")
	}
	input.RecipientID = lowerID(input.RecipientID)
	var err error
	if input.Note, err = cleanText(input.Note, 1, 500, "Catatan"); err != nil {
		return 0, err
	}
	return parseAmount(input.AmountMinor)
}

func (s *Server) transfer(w http.ResponseWriter, r *http.Request) { s.financialCommand(w, r, false) }
func (s *Server) allocate(w http.ResponseWriter, r *http.Request) { s.financialCommand(w, r, true) }

func (s *Server) financialCommand(w http.ResponseWriter, r *http.Request, allocation bool) {
	if err := s.checkWrite(r, allocation); err != nil {
		writeError(w, err)
		return
	}
	var input financialInput
	if err := decodeBody(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	amount, err := normalizeFinancial(&input)
	if err != nil {
		writeError(w, err)
		return
	}
	kind := "TRANSFER"
	if allocation {
		kind = "ALLOCATION"
	}
	s.mutate(w, r, allocation, kind, input, func(ctx context.Context, tx pgx.Tx, i *identity, key string) (*MutationResult, error) {
		if !allocation && input.RecipientID == i.User.ID {
			return nil, problem(422, "VALIDATION_ERROR", "Pilih pengguna lain sebagai penerima transfer.")
		}
		var toID, fromID string
		err := tx.QueryRow(ctx, "SELECT id FROM wallets WHERE user_id=$1 AND workspace_id=$2 AND NOT is_system", input.RecipientID, i.Workspace.ID).Scan(&toID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, problem(404, "NOT_FOUND", "Penerima tidak ditemukan.")
		}
		if err != nil {
			return nil, err
		}
		if allocation {
			err = tx.QueryRow(ctx, "SELECT id FROM wallets WHERE workspace_id=$1 AND is_system", i.Workspace.ID).Scan(&fromID)
		} else {
			err = tx.QueryRow(ctx, "SELECT id FROM wallets WHERE user_id=$1", i.User.ID).Scan(&fromID)
		}
		if err != nil {
			return nil, err
		}
		detail, err := s.commitFinancial(ctx, tx, i, key, kind, fromID, toID, amount, input.Note, nil)
		if err != nil {
			return nil, err
		}
		return &MutationResult{Transaction: detail}, nil
	})
}

func (s *Server) reverse(w http.ResponseWriter, r *http.Request) {
	if err := s.checkWrite(r, true); err != nil {
		writeError(w, err)
		return
	}
	id := r.PathValue("id")
	if !validID(id) {
		writeError(w, problem(422, "VALIDATION_ERROR", "ID transaksi tidak valid."))
		return
	}
	id = lowerID(id)
	var input reversalInput
	if err := decodeBody(w, r, &input); err != nil {
		writeError(w, err)
		return
	}
	var err error
	if input.Reason, err = cleanText(input.Reason, 10, 500, "Alasan pembalikan"); err != nil {
		writeError(w, err)
		return
	}
	s.mutate(w, r, true, "REVERSAL", input, func(ctx context.Context, tx pgx.Tx, i *identity, key string) (*MutationResult, error) {
		var kind, fromID, toID string
		var amount int64
		err := tx.QueryRow(ctx, "SELECT kind,from_wallet_id,to_wallet_id,amount_minor FROM transactions WHERE id=$1 AND workspace_id=$2 FOR UPDATE", id, i.Workspace.ID).Scan(&kind, &fromID, &toID, &amount)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, problem(404, "NOT_FOUND", "Transaksi tidak ditemukan.")
		}
		if err != nil {
			return nil, err
		}
		if kind != "TRANSFER" {
			return nil, problem(422, "REVERSAL_NOT_ALLOWED", "Hanya transaksi TRANSFER yang dapat dibalik.")
		}
		var reversed bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM transactions WHERE reversal_of=$1)", id).Scan(&reversed); err != nil {
			return nil, err
		}
		if reversed {
			return nil, problem(409, "ALREADY_REVERSED", "Transfer ini sudah memiliki transaksi pembalikan.")
		}
		detail, err := s.commitFinancial(ctx, tx, i, key, "REVERSAL", toID, fromID, amount, input.Reason, &id)
		if err != nil {
			return nil, err
		}
		return &MutationResult{Transaction: detail}, nil
	})
}

type lockedWallet struct {
	ID      string
	Balance int64
	System  bool
}

func (s *Server) commitFinancial(ctx context.Context, tx pgx.Tx, i *identity, key, kind, fromID, toID string, amount int64, note string, reversalOf *string) (*TransactionDetail, error) {
	ids := []string{fromID, toID}
	sort.Strings(ids)
	locked := map[string]lockedWallet{}
	// Identical UUID order is used by transfers in both directions and by reversals.
	for _, id := range ids {
		var wallet lockedWallet
		if err := tx.QueryRow(ctx, "SELECT id,balance_minor,is_system FROM wallets WHERE id=$1 FOR UPDATE", id).Scan(&wallet.ID, &wallet.Balance, &wallet.System); err != nil {
			return nil, err
		}
		locked[id] = wallet
	}
	source, destination := locked[fromID], locked[toID]
	if !source.System && source.Balance < amount {
		return nil, problem(409, "INSUFFICIENT_FUNDS", "Saldo pengirim tidak mencukupi. Tidak ada saldo yang berpindah.")
	}
	if source.System && source.Balance-amount < -100_000_000_000_000 {
		return nil, problem(409, "LIMIT_EXCEEDED", "Batas alokasi simulasi sistem sudah tercapai.")
	}
	if destination.Balance+amount > 1_000_000_000_000 {
		return nil, problem(409, "LIMIT_EXCEEDED", "Saldo penerima akan melebihi batas simulasi.")
	}
	id := newID()
	if _, err := tx.Exec(ctx, `INSERT INTO transactions(id,workspace_id,kind,amount_minor,note,created_by,from_wallet_id,to_wallet_id,request_key,reversal_of)
	    VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, i.Workspace.ID, kind, amount, note, i.User.ID, fromID, toID, key, reversalOf); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "UPDATE wallets SET balance_minor=balance_minor-$1 WHERE id=$2", amount, fromID); err != nil {
		return nil, err
	}
	if s.failpoint != nil {
		if err := s.failpoint("after_debit"); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE wallets SET balance_minor=balance_minor+$1 WHERE id=$2", amount, toID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO journal_entries(id,transaction_id,wallet_id,direction,amount_minor)
	  VALUES($1,$2,$3,'DEBIT',$4),($5,$2,$6,'CREDIT',$4)`, newID(), id, fromID, amount, newID(), toID); err != nil {
		return nil, err
	}
	if s.failpoint != nil {
		if err := s.failpoint("after_journal"); err != nil {
			return nil, err
		}
	}
	return readTransaction(ctx, tx, id, i)
}
