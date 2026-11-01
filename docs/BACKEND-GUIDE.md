# Membuat backend WalletLedger dari nol

Panduan ini mengikuti kode yang ada di proyek. Bacalah bersama [kontrak API](CONTRACT.md), lalu buka berkas pada setiap langkah. WalletLedger menyimpan saldo **simulasi**, bukan uang atau rekening pembayaran sungguhan. Semua jumlah memakai IDR dalam rupiah bulat: `"25000"` berarti Rp25.000, tanpa pembagian 100.

Tujuan utamanya adalah membuktikan tiga hal: saldo tidak dapat dibelanjakan dua kali, satu permintaan yang diulang tidak menciptakan transfer baru, dan setiap perubahan saldo mempunyai pasangan jurnal yang dapat diperiksa.

## 1. Peta berkas

| Berkas | Tugas | Fungsi yang dibaca dahulu |
|---|---|---|
| [go.mod](../backend/go.mod) | Versi Go dan pustaka langsung | Module `walletledger`, pgx, x/crypto |
| [main.go](../backend/cmd/server/main.go) | Membaca konfigurasi, membuat pool dan server, menjalankan migrasi | `run` |
| [types.go](../backend/internal/ledger/types.go) | Bentuk JSON dan identitas internal | `AuthView`, `Wallet`, `TransactionDetail`, `MutationResult` |
| [schema.sql](../backend/internal/ledger/schema.sql) | Tabel, indeks dan penjaga integritas PostgreSQL | `validate_financial_transaction`, `validate_wallet_balance` |
| [migrate.go](../backend/internal/ledger/migrate.go) | Memasang skema dengan advisory lock dan checksum | `Migrate` |
| [server.go](../backend/internal/ledger/server.go) | Rute, batas konkurensi, autentikasi dan pembungkus pembacaan | `New`, `Handler`, `routes`, `authenticate`, `withRead` |
| [helpers.go](../backend/internal/ledger/helpers.go) | Validasi, password, token, JSON dan kesalahan | `parseAmount`, `decodeBody`, `hashPassword`, `writeError` |
| [auth.go](../backend/internal/ledger/auth.go) | Setup, masuk, sesi, keluar dan direktori pengguna | `setup`, `login`, `recordLoginAttempt`, `logout` |
| [mutations.go](../backend/internal/ledger/mutations.go) | Akun baru, alokasi, transfer, pembalikan dan receipt | `mutate`, `financialCommand`, `commitFinancial`, `reverse` |
| [queries.go](../backend/internal/ledger/queries.go) | Wallet, riwayat, detail jurnal, rekonsiliasi dan audit | `readTransaction`, `listTransactions`, `reconciliation` |
| [openapi.go](../backend/internal/ledger/openapi.go) | Skema REST API yang dapat diunduh tanpa CDN | `openapi` |
| [Dockerfile](../backend/Dockerfile) | Membangun binary dan menjalankannya sebagai UID 10001 | Stage `build` dan `runtime` |

Tes berada di paket yang sama agar dapat menyuntikkan kegagalan internal, tanpa menyediakan endpoint debug. Lihat [helper database tes](../backend/internal/ledger/test_helpers_test.go), [tes autentikasi](../backend/internal/ledger/auth_integration_test.go), [tes transaksi](../backend/internal/ledger/ledger_integration_test.go), serta [tes penjaga SQL](../backend/internal/ledger/schema_integration_test.go).

## 2. Tentukan aturan sebelum membuat endpoint

Aturan disimpan dalam [kontrak](CONTRACT.md). Jangan memulai dengan sekadar mengurangi kolom saldo pengirim lalu menambah saldo penerima lewat dua permintaan terpisah. Kedua perubahan dan bukti jurnal harus masuk dalam **satu transaksi PostgreSQL**.

Wallet pribadi mempunyai saldo antara nol dan satu triliun rupiah simulasi. Wallet sistem mempunyai saldo negatif atau nol, paling rendah minus seratus triliun. Jumlah satu perintah paling besar satu miliar. Batas ini berada jauh di bawah batas `int64`, sehingga penambahan atau pengurangan yang sudah divalidasi tidak meluap.

`amount_minor` tetap bernama demikian untuk konsistensi kontrak, tetapi unitnya satu rupiah. Nilai JSON harus berupa string:

```json
{
  "recipient_id": "11111111-1111-4111-8111-111111111111",
  "amount_minor": "25000",
  "note": "Simulasi penggantian biaya alat tulis"
}
```

Di [parseAmount](../backend/internal/ledger/helpers.go), pola bilangan menerima digit positif tanpa nol di depan, kemudian `strconv.ParseInt` mengubahnya menjadi `int64`. JSON angka `25000`, string `"025000"`, `"2.5"`, `"-1"`, dan `"1e3"` ditolak. Backend tidak memakai `float64` untuk uang.

## 3. Rancang tabel dan arti debit/kredit

Buka [schema.sql](../backend/internal/ledger/schema.sql). Urutannya mengikuti dependensi foreign key.

| Tabel | Isi dan alasan |
|---|---|
| `workspaces` | Satu workspace untuk satu instalasi; kolom singleton menolak workspace kedua |
| `users` | Email unik yang dinormalisasi, nama, role dan hash password |
| `wallets` | Satu wallet per pengguna dan satu wallet pendanaan sistem; `balance_minor` adalah cache |
| `sessions` | Hash token cookie, token CSRF, pengguna dan waktu kedaluwarsa |
| `login_attempts` | Catatan percobaan masuk untuk batas per email dan alamat peer |
| `transactions` | Dokumen transaksi, pihak asal/tujuan, pembuat dan hubungan pembalikan |
| `journal_entries` | Dua baris jurnal setiap transaksi, masing-masing untuk satu wallet |
| `receipts` | Hasil tetap berdasarkan pasangan actor dan Idempotency-Key |
| `audit_events` | Satu bukti audit untuk setiap pembuatan akun atau transaksi yang diterima |
| `migration_versions` | Nomor skema dan checksum, dibuat oleh `Migrate` |

Dalam proyek ini, saldo wallet = **total CREDIT dikurangi total DEBIT**. Ini adalah konvensi ledger wallet, bukan seluruh bagan akun perusahaan.

Contoh alokasi Rp100.000 dari sistem ke pengguna A:

| Wallet | Arah | Jumlah | Saldo sesudah alokasi |
|---|---|---:|---:|
| Sistem | DEBIT | 100000 | -100000 |
| A | CREDIT | 100000 | 100000 |

Transfer Rp25.000 dari A ke B menambah satu dokumen transaksi dan dua baris lagi: A DEBIT 25000, B CREDIT 25000. Saldo A menjadi 75000 dan B menjadi 25000. Total seluruh wallet, termasuk sistem, tetap nol. Setiap transaksi mempunyai total debit sama dengan kredit.

Saldo pada `wallets` mempercepat pemeriksaan dana dan tampilan. Jurnal tetap menjadi bukti pergerakan yang dibandingkan dalam rekonsiliasi.

## 4. Pakai migrasi terpisah dari server

[Migrate](../backend/internal/ledger/migrate.go) membuka transaksi, mengambil advisory lock khusus migrasi, membaca `migration_versions`, kemudian menjalankan SQL versi pertama bila belum ada. Checksum SHA-256 dari SQL disimpan bersama nomor versi. Menjalankan migrasi lagi dengan SQL yang sama aman; mengubah SQL versi yang sudah terpasang menghasilkan kesalahan checksum.

[main.go](../backend/cmd/server/main.go) menyediakan tiga perintah:

```powershell
go run ./cmd/server migrate
go run ./cmd/server serve
go run ./cmd/server health
```

Perintah tersebut dijalankan dari direktori backend dengan `DATABASE_URL` yang sesuai. Gunakan skrip di [README](../README.md) untuk konfigurasi lokal yang sudah disediakan. Migrasi memakai akun pemilik skema. Server aplikasi memakai role runtime yang tidak memiliki izin membuat tabel atau melewati penjaga integritas. Menyalakan server tidak otomatis membuat tabel, akun ataupun saldo demo.

Untuk perubahan skema setelah publikasi, buat jalur migrasi versi berikutnya. Jangan mengganti checksum yang sudah tercatat agar perubahan lama terlihat berhasil.

## 5. Buat server HTTP dan batas sumber daya

[New](../backend/internal/ledger/server.go) menerima pool PostgreSQL dan `Config`, memasang rute pada `http.ServeMux`, lalu menyediakan `Handler()`. Konfigurasi memuat origin yang diizinkan, keamanan cookie, durasi sesi dan `ReceiptSecret` privat minimal 32 byte. Constructor menolak secret yang terlalu pendek; CLI membaca `WALLETLEDGER_RECEIPT_SECRET`. Durasi nol memakai 12 jam; batas percobaan login tetap aktif.

[Handler](../backend/internal/ledger/server.go) memberikan batas 32 permintaan bersamaan dan konteks 15 detik. Permintaan berikutnya menerima 503 saat slot penuh. Pool pada [main.go](../backend/cmd/server/main.go) dibatasi delapan koneksi, dengan waktu koneksi, statement, lock dan transaksi diam yang terbatas. Ini adalah batas sumber daya lokal, bukan klaim throughput.

Server native mendengar pada loopback secara default. Image Docker mengatur `WALLETLEDGER_HOST=0.0.0.0` agar proxy dalam jaringan Compose dapat mengaksesnya. Port publik tetap dibatasi oleh konfigurasi Compose.

[decodeBody](../backend/internal/ledger/helpers.go) membatasi JSON hingga 64 KiB, mewajibkan objek JSON, menolak kolom yang tidak dikenal dan menolak dokumen JSON tambahan. Semua nilai SQL dikirim sebagai parameter `$1`, `$2`, dan seterusnya.

## 6. Tambahkan setup dan sesi

Alur setup ada di [setup](../backend/internal/ledger/auth.go):

1. Validasi origin dan token CSRF anonim.
2. Validasi nama workspace, nama pengguna, email dan password.
3. Ambil lock khusus setup dan pastikan workspace belum ada.
4. Buat workspace, akun Admin, wallet pribadi bersaldo nol, wallet sistem bersaldo nol dan sesi.
5. Commit sebelum mengirim cookie serta `AuthView`.

`GET /api/auth/csrf` memasang cookie HttpOnly anonim dan mengembalikan token dalam JSON. Frontend mengirim token itu melalui `X-CSRF-Token` pada setup/login. Double-submit membandingkan cookie dengan header; origin juga harus cocok persis.

[hashPassword](../backend/internal/ledger/helpers.go) melakukan SHA-256 terhadap byte UTF-8 password, mengubah hasilnya menjadi base64, lalu bcrypt dengan cost 12. Prefix `sha256$` menandai formatnya. Prehash diperlukan karena bcrypt standar hanya menerima paling banyak 72 byte, sedangkan kontrak menerima 12 sampai 128 byte. Password tidak dipotong. Ini memakai SHA-256 untuk menyiapkan input bcrypt, bukan menyimpan SHA-256 password sebagai hash akhir.

[insertSession](../backend/internal/ledger/auth.go) membuat token acak 32 byte dan token CSRF acak terpisah. Browser memperoleh token sesi melalui cookie `walletledger_session` yang HttpOnly dan SameSite=Lax. PostgreSQL menyimpan hash SHA-256 token sesi. ID pengguna dalam `AuthView` bukan token autentikasi.

[authenticate](../backend/internal/ledger/server.go) mencari hash cookie, memeriksa kedaluwarsa dan mengembalikan identitas. Pada perintah mutasi, baris sesi serta pengguna dikunci `FOR SHARE` agar pencabutan sesi dan mutasi mempunyai urutan transaksi yang jelas. [logout](../backend/internal/ledger/auth.go) menghapus sesi di server sebelum menghapus cookie browser.

[recordLoginAttempt](../backend/internal/ledger/auth.go) memakai advisory lock untuk bucket email dan alamat peer, dengan urutan kunci yang tetap. Dalam jendela 600 detik, batasnya sepuluh percobaan per email dan enam puluh per peer. Semua percobaan yang melewati validasi dicatat, termasuk login berhasil. Header `X-Forwarded-For` tidak dipercaya sebagai identitas peer. Jika seluruh trafik datang melalui satu proxy, bucket peer dapat dipakai bersama; perencanaan deployment perlu mempertimbangkan hal tersebut.

## 7. Pisahkan autentikasi, role, identitas tab dan CSRF

Sebelum transaksi, [checkWrite](../backend/internal/ledger/mutations.go) memeriksa sesi. [authenticatedWrite](../backend/internal/ledger/server.go) lalu memeriksa:

- `Origin` cocok dengan daftar konfigurasi.
- `X-Wallet-Actor` cocok dengan pengguna dalam cookie.
- `X-Wallet-Workspace` cocok dengan workspace dalam cookie.
- `X-CSRF-Token` cocok dengan token sesi.

Header actor/workspace mencegah formulir tab lama berjalan diam-diam sebagai akun yang baru login pada tab lain. Cookie tetap menjadi bukti autentikasi. Header tersebut tidak menggantikan cookie dan tidak memperbolehkan memilih pengirim sewenang-wenang.

Admin dapat membuat Member, mengalokasikan saldo, membalik transfer dan membaca rekonsiliasi/audit. Member hanya dapat membaca wallet dan transaksi yang melibatkan dirinya. Direktori penerima hanya mengembalikan ID, nama serta ID wallet, tanpa email atau saldo. Wallet atau transaksi asing dikembalikan sebagai 404.

## 8. Buat pembungkus mutasi yang sama untuk semua perintah

Bagian inti ada di [mutate](../backend/internal/ledger/mutations.go). Handler memvalidasi input terlebih dahulu, lalu menyerahkan fungsi domain kepada pembungkus ini.

Urutannya:

1. Validasi UUID pada `Idempotency-Key`.
2. Buat fingerprint HMAC-SHA256 dari metode, path dan JSON input yang sudah divalidasi, memakai secret deployment yang berada di luar database.
3. Mulai transaksi database; autentikasi dan otorisasi ulang di dalam transaksi.
4. Ambil advisory transaction lock berdasarkan pasangan pengguna dan key.
5. Cari receipt yang sudah ada. Fingerprint berbeda menghasilkan `IDEMPOTENCY_CONFLICT`.
6. Jika receipt cocok, kembalikan hasil lama tanpa menjalankan domain lagi.
7. Jika belum ada, jalankan fungsi domain, simpan receipt dan audit yang relevan.
8. Commit semua perubahan sebelum mengirim respons.

Key sama dari dua akun berbeda mempunyai namespace berbeda. Key sama dari satu akun dengan isi berbeda tidak dapat menimpa receipt.

Pembuatan akun juga mempunyai password dalam input. HMAC mencegah fingerprint pada salinan database menjadi pemeriksa tebakan password yang cepat tanpa mengetahui secret deployment; password itu sendiri tidak disimpan dalam receipt. Secret harus privat dan tetap sama setelah restart atau pemulihan database. Menggantinya begitu saja membuat fingerprint receipt lama tidak cocok. Simpan cadangan secret bersama prosedur pemulihan yang terlindungi, bukan dalam Git atau screenshot.

Hasil sukses berbentuk [MutationResult](../backend/internal/ledger/types.go), status 201. Replay tetap 201, dengan `replayed: true`. Hasil pada receipt adalah bukti historis; tampilan saldo terkini harus dimuat ulang dari endpoint wallet.

Tiga penolakan bisnis disimpan sebagai receipt status 409: `INSUFFICIENT_FUNDS`, `LIMIT_EXCEEDED`, dan `ALREADY_REVERSED`. Mengulangi key yang sama tetap menghasilkan penolakan yang sama walaupun keadaan saldo kemudian berubah. Untuk mencoba keputusan bisnis baru, pengguna memulai perintah baru secara sadar. Kesalahan autentikasi dan validasi tidak membuat receipt.

Jika koneksi terputus tepat setelah commit, browser mungkin tidak mengetahui bahwa operasi berhasil. Backend mengembalikan 503 ketika hasil tidak dapat dipastikan. Retry harus memakai key dan isi asli agar server dapat menemukan receipt, bukan memakai key baru yang dapat membuat transfer kedua.

## 9. Implementasikan transfer dengan urutan lock yang tetap

[financialCommand](../backend/internal/ledger/mutations.go) dipakai oleh transfer dan alokasi. Sumber transfer selalu wallet pengguna dalam sesi. Alokasi menggunakan wallet sistem dan hanya dapat dipanggil Admin.

[commitFinancial](../backend/internal/ledger/mutations.go) menerima kedua ID wallet, jumlah, catatan dan hubungan pembalikan bila ada. Bagian yang menentukan urutan lock:

```go
ids := []string{fromID, toID}
sort.Strings(ids)
for _, id := range ids {
    // SELECT id, balance_minor, is_system
    // FROM wallets WHERE id=$1 FOR UPDATE
}
```

Ini cuplikan penjelasan; SQL lengkap dan penanganan error berada pada [fungsi aslinya](../backend/internal/ledger/mutations.go). Transfer A ke B dan B ke A mengunci kedua wallet dalam urutan UUID yang sama. Transfer antara pasangan lain tidak menunggu satu lock global workspace.

Setelah lock didapat, fungsi memeriksa dana pengirim, batas sistem dan batas saldo penerima. Hanya setelah pemeriksaan lolos, fungsi menulis:

- Satu dokumen `transactions`.
- Pengurangan cache saldo sumber.
- Penambahan cache saldo tujuan.
- Dua `journal_entries`, DEBIT sumber dan CREDIT tujuan.

Pembungkus `mutate` menambahkan receipt dan audit pada transaksi SQL yang sama. Bila salah satu langkah gagal, rollback menghapus seluruh perubahan, termasuk pengurangan saldo yang sempat dilakukan di dalam transaksi.

## 10. Pasang pertahanan di database

Validasi Go memberi pesan bisnis yang jelas. Penjaga pada [schema.sql](../backend/internal/ledger/schema.sql) memastikan penulisan SQL yang tidak sah juga ditolak.

`validate_financial_transaction` memeriksa jumlah dan arah dua leg, wallet sumber/tujuan yang cocok, workspace, pemilik transfer, role alokasi, bentuk pembalikan, receipt dan audit. `validate_wallet_balance` menghitung jurnal dan membandingkannya dengan cache.

Constraint trigger tersebut **DEFERRABLE INITIALLY DEFERRED**. Database mengizinkan keadaan sementara di tengah transaksi, misalnya saldo sudah didebit tetapi jurnal belum ditulis. Saat commit, seluruh keadaan akhir wajib konsisten. Mengurangi saldo lalu commit tanpa jurnal gagal.

`reject_history_mutation` menolak UPDATE/DELETE pada transaksi, jurnal, receipt dan audit. Statement trigger menolak TRUNCATE, termasuk percobaan melalui CASCADE. Identitas wallet tidak dapat diubah; hanya cache saldonya yang dapat diperbarui dengan bukti jurnal yang sesuai. Akun Member baru juga memerlukan wallet, receipt dan audit dalam transaksi yang sama. Setup Admin merupakan pengecualian yang eksplisit.

Role runtime tidak mempunyai akses DDL atau kemampuan menonaktifkan trigger. Pemilik skema tetap merupakan pihak tepercaya untuk migrasi. Penjaga ini tidak berarti administrator database atau sistem operasi tidak dapat mengubah database; batas kepercayaannya dijelaskan dalam konfigurasi runtime.

## 11. Buat reversal sebagai dokumen baru

[reverse](../backend/internal/ledger/mutations.go) mengunci transaksi asal, memastikan jenisnya TRANSFER, lalu memeriksa apakah sudah ada `reversal_of` yang menunjuk kepadanya. Indeks unik menyediakan pertahanan tambahan terhadap pembalikan ganda.

Pembalikan A ke B menjadi transaksi baru B ke A dengan jumlah sama. B harus masih mempunyai dana yang cukup, dan saldo A harus tetap di bawah batas. Alasan minimal sepuluh karakter menjadi catatan transaksi baru. Jika B telah membelanjakan dananya, penolakan tidak mengubah transaksi asal.

Transaksi asal tidak diperbarui. Field `reversal_id` pada DTO dihitung dari hubungan transaksi baru, sementara `reversal_of` tersimpan pada dokumen pembalikan. ALLOCATION dan REVERSAL tidak dapat dibalik melalui API ini.

## 12. Tambahkan pembacaan yang konsisten

[withRead](../backend/internal/ledger/server.go) menjalankan pembacaan dalam transaksi read-only dengan isolasi repeatable read. Ini penting untuk [reconciliation](../backend/internal/ledger/queries.go): jumlah wallet, jumlah transaksi, total debit/kredit dan perbedaan cache harus berasal dari snapshot database yang sama.

[readTransaction](../backend/internal/ledger/queries.go) mengembalikan dokumen, kedua leg dan totalnya. [listTransactions](../backend/internal/ledger/queries.go) membatasi anggota pada transaksi yang melibatkan walletnya dan memakai urutan `seq` menurun. Query mengambil `limit+1` untuk menentukan apakah ada halaman berikutnya.

Cursor `before` berarti ambil nomor urut yang lebih kecil. Nomor dapat mempunyai celah akibat transaksi SQL yang rollback; ini wajar untuk sequence PostgreSQL. Paginasi bukan snapshot tetap lintas beberapa permintaan. Refresh memulai lagi dari transaksi terbaru.

Rekonsiliasi mempunyai tiga pemeriksaan yang terlihat pada respons:

1. Setiap cache wallet sama dengan jumlah jurnalnya.
2. Total debit sama dengan total kredit.
3. Total saldo wallet termasuk sistem sama dengan nol.

## 13. Dokumentasikan dan uji jalur kegagalan

[openapi](../backend/internal/ledger/openapi.go) menyediakan bentuk request/response melalui `/api/openapi.json`. Dokumentasi tidak memuat akun demo, password, DSN atau token. Skema uang tetap berupa string. Tidak ada Swagger UI dari CDN yang dibutuhkan untuk menjalankan aplikasi.

Pada tes, `Server.failpoint` dapat menghasilkan error pada `after_debit`, `after_journal`, atau `before_commit`. Fungsinya tidak diekspos sebagai endpoint atau konfigurasi jaringan. Tes memeriksa bahwa saldo, jumlah jurnal, receipt dan audit tidak berubah setelah rollback.

Tes konkurensi harus menggunakan koneksi PostgreSQL nyata: dua transfer yang memperebutkan saldo, transfer berlawanan arah, key identik bersamaan, serta pembalikan ganda. Tes SQL mencoba drift cache, transaksi tanpa leg dan perubahan history. Jumlah dan hasil eksekusi yang benar-benar dilakukan dicatat pada [laporan verifikasi](VERIFICATION.md).

## 14. Apa yang dilakukan Docker

Stage build pada [Dockerfile](../backend/Dockerfile) mengambil dependensi sesuai `go.mod` dan `go.sum`, kemudian menghasilkan binary Go tanpa CGO. Stage runtime hanya membawa binary dan pengguna aplikasi UID 10001. Perintah defaultnya `serve`; migrasi tetap layanan terpisah.

Health check menjalankan subperintah `health`, yang memanggil `/api/health`. Endpoint itu memeriksa koneksi PostgreSQL. Container sehat tidak otomatis berarti fitur bisnis sudah teruji; bukti bisnis berasal dari tes HTTP/database dan pemeriksaan browser.

Jaringan, role runtime, volume, proxy frontend, serta langkah menyalakan dan menghentikan layanan dijelaskan dalam [README](../README.md). Database memakai volume tersendiri; menghentikan aplikasi tidak perlu menghapus volumenya.

## 15. Menjelaskan proyek saat interview

“Saya membuat simulator wallet menggunakan Go dan PostgreSQL. Uang diproses sebagai integer rupiah dan dikirim sebagai string JSON. Transfer mengunci dua wallet dalam urutan UUID yang tetap, lalu menyimpan saldo, dua jurnal, receipt idempotency dan audit dalam satu transaksi database. Database juga memeriksa jurnal seimbang dan kecocokan cache saat commit. Kalau respons terputus, klien mengulang key yang sama dan mendapat receipt lama. Pembalikan membuat transaksi baru sehingga bukti transaksi asal tetap ada.”

Lanjutkan dengan bukti yang dapat dibuka: satu transfer, dua leg jurnal, replay key yang sama, penolakan saldo tidak cukup dan rekonsiliasi. Jelaskan batas proyek: satu workspace, saldo sintetis, penulisan online, tanpa payment gateway, bunga, uang asli, sertifikasi atau klaim kesiapan layanan keuangan publik.
