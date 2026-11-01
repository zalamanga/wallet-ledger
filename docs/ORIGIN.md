# Asal source

Backend domain WalletLedger ditulis baru. ReserveFlow API pada commit `e3547e69851165fa7b20154502aa05ad8d3481cd` menjadi referensi pola transaksi, pengurutan lock dan receipt. Implementasi reservasi, database, API key, source backend dan Git history proyek tersebut tidak disalin ke WalletLedger.

Manifest/lock dependency frontend, konfigurasi TypeScript/formatter, atribut Git dan lisensi MIT memakai fondasi CollabBoard commit `b77e530f4359dfa64ab9f0e6418eeb4f13096420`. Komponen board, autentikasi, akun, `.env`, snapshot, screenshot dan database CollabBoard tetap terpisah. UI dan domain wallet dibuat baru.

Source baru dilisensikan MIT untuk Muhammad Rafik 2026, sesuai [LICENSE](../LICENSE). Dependency pihak ketiga mengikuti lisensi masing-masing.

Rujukan primer: [transaksi Go](https://go.dev/doc/database/execute-transactions), [pgx](https://pkg.go.dev/github.com/jackc/pgx/v5), dan [lock PostgreSQL 17](https://www.postgresql.org/docs/17/explicit-locking.html). Nomor versi implementasi dipatok di manifest; jangan menganggap halaman dokumentasi latest selalu sama dengan versi yang dipakai.
