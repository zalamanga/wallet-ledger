package ledger

import "time"

type Config struct {
	AllowedOrigins []string
	CookieSecure   bool
	SessionHours   int
	ReceiptSecret  string
}

type AuthUser struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Role     string `json:"role"`
}

type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type AuthView struct {
	User      AuthUser  `json:"user"`
	Workspace Workspace `json:"workspace"`
	CSRFToken string    `json:"csrf_token"`
}

type Wallet struct {
	ID           string    `json:"id"`
	UserID       *string   `json:"user_id"`
	OwnerName    string    `json:"owner_name"`
	IsSystem     bool      `json:"is_system"`
	Currency     string    `json:"currency"`
	BalanceMinor string    `json:"balance_minor"`
	CreatedAt    time.Time `json:"created_at"`
}

type Transaction struct {
	ID            string    `json:"id"`
	Seq           int64     `json:"seq"`
	Kind          string    `json:"kind"`
	AmountMinor   string    `json:"amount_minor"`
	Note          string    `json:"note"`
	CreatedBy     string    `json:"created_by"`
	CreatedByName string    `json:"created_by_name"`
	FromWalletID  string    `json:"from_wallet_id"`
	ToWalletID    string    `json:"to_wallet_id"`
	FromName      string    `json:"from_name"`
	ToName        string    `json:"to_name"`
	CreatedAt     time.Time `json:"created_at"`
	ReversalOf    *string   `json:"reversal_of"`
	ReversalID    *string   `json:"reversal_id"`
}

type JournalEntry struct {
	WalletID    string `json:"wallet_id"`
	WalletName  string `json:"wallet_name"`
	Direction   string `json:"direction"`
	AmountMinor string `json:"amount_minor"`
}

type TransactionDetail struct {
	Transaction      Transaction    `json:"transaction"`
	Entries          []JournalEntry `json:"entries"`
	DebitTotalMinor  string         `json:"debit_total_minor"`
	CreditTotalMinor string         `json:"credit_total_minor"`
}

type MutationResult struct {
	RequestID   string             `json:"request_id"`
	Replayed    bool               `json:"replayed"`
	Transaction *TransactionDetail `json:"transaction"`
	User        *AuthUser          `json:"user"`
}

type apiError struct {
	Status int    `json:"-"`
	Detail string `json:"detail"`
	Code   string `json:"code"`
}

func (e *apiError) Error() string { return e.Code }

func problem(status int, code, detail string) *apiError {
	return &apiError{Status: status, Code: code, Detail: detail}
}

type identity struct {
	AuthView
	SessionID string
}
