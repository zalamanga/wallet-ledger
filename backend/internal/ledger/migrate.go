package ledger

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('walletledger:migrations',0))"); err != nil {
		return fmt.Errorf("lock migration: %w", err)
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS migration_versions(version integer PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT clock_timestamp())`); err != nil {
		return fmt.Errorf("create migration register: %w", err)
	}
	h := sha256.Sum256([]byte(schema))
	checksum := hex.EncodeToString(h[:])
	var stored string
	err = tx.QueryRow(ctx, "SELECT checksum FROM migration_versions WHERE version=1").Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err = tx.Exec(ctx, schema); err != nil {
			return fmt.Errorf("apply schema version 1: %w", err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO migration_versions(version,checksum) VALUES(1,$1)", checksum); err != nil {
			return fmt.Errorf("record migration: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("read migration: %w", err)
	} else if stored != checksum {
		return errors.New("migration checksum differs from applied version 1")
	}
	return tx.Commit(ctx)
}
