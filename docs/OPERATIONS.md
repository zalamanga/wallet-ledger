# Menjalankan WalletLedger

## Docker

[Start.ps1](../scripts/Start.ps1) menjalankan project Compose `walletledger`. PostgreSQL 5462 dan web 8170 hanya dipublikasikan pada loopback; API 8080 berada di jaringan Compose. Urutan startup: db sehat, migrasi selesai, provision role runtime selesai, API sehat, web sehat.

Password database pemilik dan runtime berbeda, acak, dan disimpan dalam `.env` privat bersama secret receipt HMAC stabil. CLI migrasi memakai pemilik schema. API memakai `walletledger_runtime` yang tidak memiliki superuser, createdb, createrole, replication, bypassrls, CREATE schema/database atau TEMP. Runtime memperoleh hak data/sequence; perubahan history tetap dijaga trigger. DML migration_versions dicabut dari runtime.

[Stop.ps1](../scripts/Stop.ps1) menghentikan layanan proyek ini. Volume `walletledger_postgres_data` dipertahankan. Tidak ada cleanup global terhadap proyek/container pengguna lain. Source/cache development ditempatkan di D pada lingkungan kerja ini. Lokasi disk image/volume tetap mengikuti pengaturan Docker Desktop.

## Credential dan data latihan

Untuk data sendiri, isi setup. Untuk demo, jalankan `python scripts/seed-demo.py --confirm-synthetic` **sebelum** setup manual. Seed membuat empat akun dengan password acak dan menyimpan credential ke `.local/LOCAL-CREDENTIALS.txt`. Checkpoint, request key dan hasil langkah ada pada `.local/demo-state.json`; keduanya privat dan diabaikan Git.

Jika seed menandai langkah inflight yang belum pasti, periksa key/request dan record server. Jangan menghapus checkpoint atau mengganti key untuk menyembunyikan ketidakpastian. Seed yang selesai memverifikasi lima wallet, sepuluh transaksi, jumlah debit/kredit sama, seluruh cache sesuai jurnal, serta saldo pengguna yang diharapkan.

Pulihkan `.env` asli jika hilang saat data/volume masih ada. Mengedit password env tidak otomatis mengubah role PostgreSQL yang telah dibuat. Secret receipt harus stabil agar request lama tetap dapat diverifikasi; rotasi memerlukan prosedur kompatibilitas tersendiri.

## Development native

Go 1.27.3, Node.js 22.14+ dan Docker PostgreSQL diperlukan. Pada mesin ini Go portable berada di `D:/DevTools/go1.27.3/go`; pengguna lain dapat memasang Go pada lokasi sendiri. [Common.ps1](../scripts/Common.ps1) memilih tool portable bila ada, kemudian Go pada PATH. Cache Go/npm dan temporary diarahkan ke D untuk lingkungan kerja ini.

```powershell
npm.cmd ci
npm.cmd --prefix frontend ci
.\scripts\Dev-Api.ps1
```

Pada terminal lain jalankan `npm.cmd --prefix frontend run dev`, lalu buka `http://127.0.0.1:5175`. Vite mem-proxy `/api` ke server 8171. Role runtime harus diprovision dari startup Docker lebih dulu. Jangan menjalankan server native pada port yang telah dipakai proses lain.

```powershell
.\scripts\Test.ps1
```

Test membuat database `walletledger_test_<UUID>` dari admin URL privat, lalu menghapus database yang dibuatnya. Browser hanya diizinkan menulis ke instance test8171 dengan data sintetis yang terpisah. Main demo8170 tidak dipakai sebagai target mutation E2E. [CI](../.github/workflows/ci.yml) menyiapkan instance dan role SQL tersebut serta menjalankan build/health Compose.

## Batas deployment dan pemulihan

Konfigurasi lokal memakai HTTP dengan cookie Secure=false. Untuk host publik, operator perlu TLS, allowed Origin/domain tepat, Secure=true, pengelolaan akun, penyimpanan secret, backup dan monitoring yang sesuai beban. Bucket IP pada proxy default melihat peer proxy; konfigurasinya perlu ditinjau sebelum akses publik.

Cadangkan PostgreSQL dan konfigurasi privat termasuk secret HMAC. Uji restore ke instance terpisah sebelum mengandalkannya. Test rollback/idempotensi dan image sehat tidak membuktikan bahwa backup operator sudah dapat direstore.

Satu workspace, currency IDR, alokasi saldo sintetis dan online-only writes adalah batas implementasi. History/receipt tidak mempunyai job retensi. Penghapusan data, role editing, payment gateway dan deployment internet tidak menjadi bagian startup lokal ini.
