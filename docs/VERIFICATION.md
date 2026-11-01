# Verifikasi WalletLedger

Pemeriksaan lokal dilakukan pada2 November2026 (Asia/Bangkok). Go1.27.3, PostgreSQL17 nyata dan Microsoft Edge terpasang dipakai pada Windows. Source, compiler, cache dan temporary pengembangan berada di D. Database test UUID terpisah dari workspace demo.

## Backend dan frontend

| Pemeriksaan | Hasil lokal |
|---|---|
| Suite PostgreSQL awal |36 tes Go utama dan43 subkasus lulus;0 test skip. Package cmd/server tidak mempunyai berkas test dan dicatat terpisah. Durasi package172,591 detik. |
| Regresi frontend/API router |1 tes utama tambahan lulus setelah perbaikan rute static; memeriksa HTML, HEAD, aset, API,405/404 dan berkas di luar dist. |
| Unit frontend |23/23 lulus dalam3file. |
| TypeScript dan production build |Lulus; build terbaru memuat label form eksplisit dengan hint terpisah. |
| Browser utama |9 skenario lulus sebelum pemeriksaan Tab native berhenti; kondisi browser chrome kemudian diperbaiki pada harness. |
| Browser terarah terakhir |3/3 lulus: keyboard/fokus, ponsel/teks200%, dan respons akun lama sesudah logout. |
| Setup UI dengan database kosong |Pembuatan workspace melalui formulir dan transfer dua sesi lulus pada database UUID baru. |
| Script PowerShell |Parser menerima kelima script operasional. |

Seluruh12 skenario browser telah lulus melalui run utama dan run terarah; ini tidak menyatakan bahwa run lokal awal lulus12sekaligus. CI menjalankan ulang seluruh suite pada source commit publik. Test-level skip menggagalkan CI; paket Go tanpa test dicatat sebagai kategori terpisah.

## Kasus yang diperiksa

- Transfer bersamaan tidak membuat pengguna membelanjakan saldo yang sama dua kali; transfer berlawanan memakai urutan wallet tetap.
- Retry identik mengembalikan receipt asal, perubahan payload/key ditolak, dan penolakan saldo kurang tetap stabil setelah ada alokasi baru.
- Kegagalan setelah debit, jurnal atau sebelum commit membatalkan seluruh perubahan.
- Nominal ambigu/pecahan/numerik JSON, cap saldo, pembalikan berulang dan saldo penerima asal yang kurang ditangani server.
- Jurnal, transaksi, receipt dan audit menolak UPDATE/DELETE/TRUNCATE; cache yang menyimpang atau jurnal tidak seimbang ditolak pada commit.
- Session token hanya tersimpan sebagai hash; cookie, CSRF, expected identity, role dan ID asing diperiksa.
- Response commit yang hilang dapat dipulihkan setelah reload dengan request/key/pemilik yang sama tanpa debit kedua.
- CSRF stale menghasilkan403 nyata kemudian retry dengan intent yang sama. Pergantian cookie dan logout lintas tab tidak mengirim intent lama sebagai akun baru.
- Form pembuatan akun/alokasi, konfirmasi transfer, pembalikan, rekonsiliasi dan audit dijalankan melalui API dan UI nyata.

Native Edge dapat memindahkan fokus ke browser chrome pada satu langkah di batas modal. Harness hanya menerima BODY yang tidak memiliki page focus dengan modal tetap aktif; Tab berikutnya harus langsung kembali ke dialog. Fokus ke kontrol aplikasi di belakang modal tetap menggagalkan pemeriksaan. Jejak fokus disimpan sebagai artifact.

## Docker dan data latihan

Build image API, frontend dan provision berhasil. PostgreSQL, API dan web healthy; migrator dan provision selesai sukses. API memakai role PostgreSQL runtime nonowner, dengan hak CREATE/TEMP/superuser dicabut. HTTP aplikasi8170 diteruskan melalui Nginx.

Seed HTTP memverifikasi4akun,5wallet dan10transaksi, termasuk jurnal pembalikan. Total debit sama dengan kredit, seluruh selisih cache/jurnal nol, dan total saldo seluruh wallet nol. Nilai pengguna: Admin175.000, Ayu510.000, Dimas450.000 dan Surya565.000 rupiah sintetis; sumber simulasi-1.700.000.

## Tampilan production

Reviewer utama memeriksa delapan [screenshot asli](screenshots/) yang diambil melalui Nginx pada8170. Capture hanya melakukan login dan pembacaan data; hash wallet, transaksi dan audit sebelum/sesudah sama. Sepuluh transaksi dan13audit event tetap utuh.

[Bukti UI](UI-VERIFICATION.json) mencatat54 pemeriksaan layout pada320/390/768/1024/1440px, termasuk teks200% pada390/1440 untuk Member dan Admin. Tidak ada overflow dokumen/dialog. Area tabel internal yang menggulir mempunyai nama aksesibel dan dapat dijangkau keyboard.

Sebanyak2.389 sampel teks mencapai rasio minimum4,89:1,416pengukuran target minimal44px, serta pemeriksaan batas kontrol minimal3:1 lulus. Console/page error yang tidak diharapkan kosong. Hash JS/CSS dari HTTP sama dengan hasil build lokal yang diperiksa. Ini pengukuran skenario yang tercatat, bukan sertifikasi aksesibilitas menyeluruh.

## Batas bukti

Hasil ini memeriksa skenario yang tercatat, bukan pengukuran throughput atau sertifikasi keamanan. Penulisan online, satu workspace, integer IDR dan alokasi sintetis adalah lingkup implementasi. Deployment TLS publik, uang nyata, provider pembayaran, backup/restore operator dan pekerjaan retensi tidak dibuktikan oleh suite ini. [Arsitektur](ARCHITECTURE.md) dan [operasi](OPERATIONS.md) menjelaskan batasnya.
