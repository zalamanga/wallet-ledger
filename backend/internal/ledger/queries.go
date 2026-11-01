package ledger

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

const walletColumns = `w.id,w.user_id,coalesce(u.full_name,'Sistem alokasi simulasi'),w.is_system,w.currency,w.balance_minor::text,w.created_at`

func scanWallet(row pgx.Row) (Wallet, error) {
	var wallet Wallet
	err := row.Scan(&wallet.ID, &wallet.UserID, &wallet.OwnerName, &wallet.IsSystem, &wallet.Currency, &wallet.BalanceMinor, &wallet.CreatedAt)
	return wallet, err
}

func (s *Server) listWallets(w http.ResponseWriter, r *http.Request) {
	s.withRead(w, r, false, func(tx pgx.Tx, i *identity) (any, error) {
		rows, err := tx.Query(r.Context(), `SELECT `+walletColumns+` FROM wallets w LEFT JOIN users u ON u.id=w.user_id
		  WHERE w.workspace_id=$1 AND ($2 OR w.user_id=$3) ORDER BY w.is_system,lower(coalesce(u.full_name,'')),w.id`, i.Workspace.ID, i.User.Role == "Admin", i.User.ID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := []Wallet{}
		for rows.Next() {
			item, err := scanWallet(rows)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return map[string]any{"items": items}, rows.Err()
	})
}

func (s *Server) getWallet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		writeError(w, problem(422, "VALIDATION_ERROR", "ID wallet tidak valid."))
		return
	}
	s.withRead(w, r, false, func(tx pgx.Tx, i *identity) (any, error) {
		item, err := scanWallet(tx.QueryRow(r.Context(), `SELECT `+walletColumns+` FROM wallets w LEFT JOIN users u ON u.id=w.user_id
		  WHERE w.id=$1 AND w.workspace_id=$2 AND ($3 OR w.user_id=$4)`, lowerID(id), i.Workspace.ID, i.User.Role == "Admin", i.User.ID))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, problem(404, "NOT_FOUND", "Wallet tidak ditemukan.")
		}
		return item, err
	})
}

const transactionColumns = `t.id,t.seq,t.kind,t.amount_minor::text,t.note,t.created_by,actor.full_name,
    t.from_wallet_id,t.to_wallet_id,coalesce(source_user.full_name,'Sistem alokasi simulasi'),
    coalesce(destination_user.full_name,'Sistem alokasi simulasi'),t.created_at,t.reversal_of,
    (SELECT reversal.id FROM transactions reversal WHERE reversal.reversal_of=t.id)`
const transactionJoins = ` FROM transactions t JOIN users actor ON actor.id=t.created_by
    JOIN wallets source ON source.id=t.from_wallet_id LEFT JOIN users source_user ON source_user.id=source.user_id
    JOIN wallets destination ON destination.id=t.to_wallet_id LEFT JOIN users destination_user ON destination_user.id=destination.user_id `

func scanTransaction(row pgx.Row) (Transaction, error) {
	var t Transaction
	err := row.Scan(&t.ID, &t.Seq, &t.Kind, &t.AmountMinor, &t.Note, &t.CreatedBy, &t.CreatedByName, &t.FromWalletID, &t.ToWalletID, &t.FromName, &t.ToName, &t.CreatedAt, &t.ReversalOf, &t.ReversalID)
	return t, err
}

func readTransaction(ctx context.Context, tx pgx.Tx, id string, i *identity) (*TransactionDetail, error) {
	t, err := scanTransaction(tx.QueryRow(ctx, `SELECT `+transactionColumns+transactionJoins+`
	  WHERE t.id=$1 AND t.workspace_id=$2 AND ($3 OR source.user_id=$4 OR destination.user_id=$4)`, id, i.Workspace.ID, i.User.Role == "Admin", i.User.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, problem(404, "NOT_FOUND", "Transaksi tidak ditemukan.")
	}
	if err != nil {
		return nil, err
	}
	detail := &TransactionDetail{Transaction: t, Entries: []JournalEntry{}}
	rows, err := tx.Query(ctx, `SELECT e.wallet_id,coalesce(u.full_name,'Sistem alokasi simulasi'),e.direction,e.amount_minor::text
	  FROM journal_entries e JOIN wallets w ON w.id=e.wallet_id LEFT JOIN users u ON u.id=w.user_id
	  WHERE e.transaction_id=$1 ORDER BY CASE e.direction WHEN 'DEBIT' THEN 0 ELSE 1 END,e.wallet_id`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var entry JournalEntry
		if err = rows.Scan(&entry.WalletID, &entry.WalletName, &entry.Direction, &entry.AmountMinor); err != nil {
			rows.Close()
			return nil, err
		}
		detail.Entries = append(detail.Entries, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	err = tx.QueryRow(ctx, `SELECT coalesce(sum(amount_minor) FILTER(WHERE direction='DEBIT'),0)::text,
	 coalesce(sum(amount_minor) FILTER(WHERE direction='CREDIT'),0)::text FROM journal_entries WHERE transaction_id=$1`, id).Scan(&detail.DebitTotalMinor, &detail.CreditTotalMinor)
	return detail, err
}

func (s *Server) getTransaction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		writeError(w, problem(422, "VALIDATION_ERROR", "ID transaksi tidak valid."))
		return
	}
	s.withRead(w, r, false, func(tx pgx.Tx, i *identity) (any, error) { return readTransaction(r.Context(), tx, lowerID(id), i) })
}

func (s *Server) listTransactions(w http.ResponseWriter, r *http.Request) {
	before, limit, err := pageParams(r)
	if err != nil {
		writeError(w, err)
		return
	}
	s.withRead(w, r, false, func(tx pgx.Tx, i *identity) (any, error) {
		rows, err := tx.Query(r.Context(), `SELECT `+transactionColumns+transactionJoins+`
		 WHERE t.workspace_id=$1 AND ($2 OR source.user_id=$3 OR destination.user_id=$3)
		 AND ($4::bigint=0 OR t.seq<$4) ORDER BY t.seq DESC LIMIT $5`, i.Workspace.ID, i.User.Role == "Admin", i.User.ID, before, limit+1)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := []Transaction{}
		for rows.Next() {
			item, err := scanTransaction(rows)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
		var next *int64
		if len(items) > limit {
			seq := items[limit-1].Seq
			next = &seq
			items = items[:limit]
		}
		return map[string]any{"items": items, "next_before": next}, nil
	})
}

func (s *Server) reconciliation(w http.ResponseWriter, r *http.Request) {
	s.withRead(w, r, true, func(tx pgx.Tx, i *identity) (any, error) {
		type item struct {
			WalletID           string `json:"wallet_id"`
			OwnerName          string `json:"owner_name"`
			IsSystem           bool   `json:"is_system"`
			CachedBalanceMinor string `json:"cached_balance_minor"`
			LedgerBalanceMinor string `json:"ledger_balance_minor"`
			DifferenceMinor    string `json:"difference_minor"`
		}
		result := struct {
			Balanced                bool   `json:"balanced"`
			WalletCount             int    `json:"wallet_count"`
			TransactionCount        int64  `json:"transaction_count"`
			TotalDebitMinor         string `json:"total_debit_minor"`
			TotalCreditMinor        string `json:"total_credit_minor"`
			TotalWalletBalanceMinor string `json:"total_wallet_balance_minor"`
			Items                   []item `json:"items"`
		}{Balanced: true, Items: []item{}}
		rows, err := tx.Query(r.Context(), `SELECT w.id,coalesce(u.full_name,'Sistem alokasi simulasi'),w.is_system,w.balance_minor::text,
		 coalesce(sum(CASE WHEN e.direction='CREDIT' THEN e.amount_minor ELSE -e.amount_minor END),0)::text,
		 (w.balance_minor-coalesce(sum(CASE WHEN e.direction='CREDIT' THEN e.amount_minor ELSE -e.amount_minor END),0))::text
		 FROM wallets w LEFT JOIN users u ON u.id=w.user_id LEFT JOIN journal_entries e ON e.wallet_id=w.id
		 WHERE w.workspace_id=$1 GROUP BY w.id,u.full_name ORDER BY w.is_system,lower(coalesce(u.full_name,'')),w.id`, i.Workspace.ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row item
			if err = rows.Scan(&row.WalletID, &row.OwnerName, &row.IsSystem, &row.CachedBalanceMinor, &row.LedgerBalanceMinor, &row.DifferenceMinor); err != nil {
				rows.Close()
				return nil, err
			}
			if row.DifferenceMinor != "0" {
				result.Balanced = false
			}
			result.Items = append(result.Items, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		result.WalletCount = len(result.Items)
		err = tx.QueryRow(r.Context(), `SELECT count(*) FROM transactions WHERE workspace_id=$1`, i.Workspace.ID).Scan(&result.TransactionCount)
		if err != nil {
			return nil, err
		}
		err = tx.QueryRow(r.Context(), `SELECT coalesce(sum(e.amount_minor) FILTER(WHERE e.direction='DEBIT'),0)::text,
		 coalesce(sum(e.amount_minor) FILTER(WHERE e.direction='CREDIT'),0)::text
		 FROM journal_entries e JOIN transactions t ON t.id=e.transaction_id WHERE t.workspace_id=$1`, i.Workspace.ID).Scan(&result.TotalDebitMinor, &result.TotalCreditMinor)
		if err != nil {
			return nil, err
		}
		err = tx.QueryRow(r.Context(), "SELECT coalesce(sum(balance_minor),0)::text FROM wallets WHERE workspace_id=$1", i.Workspace.ID).Scan(&result.TotalWalletBalanceMinor)
		if err != nil {
			return nil, err
		}
		result.Balanced = result.Balanced && result.TotalDebitMinor == result.TotalCreditMinor && result.TotalWalletBalanceMinor == "0"
		return result, nil
	})
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	before, limit, err := pageParams(r)
	if err != nil {
		writeError(w, err)
		return
	}
	s.withRead(w, r, true, func(tx pgx.Tx, i *identity) (any, error) {
		type event struct {
			ID        string    `json:"id"`
			Seq       int64     `json:"seq"`
			ActorName string    `json:"actor_name"`
			Kind      string    `json:"kind"`
			EntityID  string    `json:"entity_id"`
			CreatedAt time.Time `json:"created_at"`
		}
		rows, err := tx.Query(r.Context(), `SELECT a.id,a.seq,u.full_name,a.kind,a.entity_id,a.created_at FROM audit_events a
		 JOIN users u ON u.id=a.actor_id WHERE u.workspace_id=$1 AND ($2::bigint=0 OR a.seq<$2) ORDER BY a.seq DESC LIMIT $3`, i.Workspace.ID, before, limit+1)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := []event{}
		for rows.Next() {
			var item event
			if err = rows.Scan(&item.ID, &item.Seq, &item.ActorName, &item.Kind, &item.EntityID, &item.CreatedAt); err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
		var next *int64
		if len(items) > limit {
			seq := items[limit-1].Seq
			next = &seq
			items = items[:limit]
		}
		return map[string]any{"items": items, "next_before": next}, nil
	})
}
