# Arsitektur WalletLedger

Browser mengirim perintah melalui HTTP. Go memvalidasi sesi, peran, identitas yang diharapkan, CSRF dan input. PostgreSQL menyimpan wallet, jurnal, receipt dan audit. Nginx menyajikan frontend production serta meneruskan `/api` ke Go. Tidak ada layanan eksternal pada alur transaksi.

## Batas domain

| Entitas | Tanggung jawab |
|---|---|
| Workspace | Satu ruang latihan per deployment |
| User/session | Identitas Admin atau Member dan sesi cookie yang dapat dicabut |
| Wallet | Satu wallet pengguna, atau sumber saldo simulasi sistem |
| Transaction | Nominal, pasangan wallet, jenis, pembuat, request key dan relasi pembalikan |
| Journal entry | Dua baris append-only yang menunjukkan debit dan kredit |
| Receipt | Hasil asal per aktor/key, termasuk penolakan bisnis yang stabil |
| Audit event | Bukti tindakan akun atau finansial yang berhasil commit |

Saldo memiliki normal **kredit dikurangi debit**. Wallet pengguna tidak boleh negatif. Sumber simulasi dapat negatif sebagai counter-account alokasi; seluruh saldo wallet tetap berjumlah nol. Model ini menggambarkan saldo kredit pengguna pada simulator, bukan klasifikasi universal seluruh akun akuntansi.

## Satu perintah finansial

1. Validasi sesi dan header identitas sebelum memproses/replay intent.
2. Buka transaksi SQL, tahan identitas/sesi yang dipakai, lalu ambil advisory lock aktor/key.
3. Jika receipt tersedia, cocokkan fingerprint HMAC dan kembalikan hasil asal. Tidak membuat jurnal baru.
4. Ambil kedua row wallet dengan `FOR UPDATE`, memakai urutan UUID yang sama untuk kedua arah transfer.
5. Periksa saldo pengirim dan batas penerima setelah lock, sebelum menulis perubahan.
6. Buat transaksi, debit wallet asal, kredit wallet tujuan, dan dua baris jurnal.
7. Simpan receipt dan satu audit event, lalu commit. Constraint deferred memeriksa hasil lengkap pada batas commit.

Tidak ada global lock yang menserialisasi semua transfer pengguna. Dua transfer yang memakai wallet sama tetap berkompetisi pada row tersebut. Pembuatan akun dan percobaan login mempunyai lock tersendiri. Pembalikan mengunci transaksi asal untuk memastikan hanya satu pembalikan diterima, kemudian memakai urutan wallet yang sama.

## Mengapa ada receipt?

Timeout respons tidak memberitahu browser apakah commit terjadi. Idempotency-Key mengidentifikasi tindakan asli. Fingerprint mengikat method, path dan body yang dinormalisasi. Key yang sama dengan isi berbeda menghasilkan 409. Key milik aktor lain mempunyai namespace terpisah.

Fingerprint memakai **HMAC-SHA256** dengan secret deployment stabil di luar database. Ini juga berlaku untuk pembuatan akun yang body-nya memuat password. Receipt tidak menyimpan password maupun body akun mentah. Menggunakan hash cepat tanpa secret untuk body password akan menyediakan cara menebak password secara offline yang lebih murah daripada bcrypt.

Hasil `INSUFFICIENT_FUNDS`, `LIMIT_EXCEEDED` dan `ALREADY_REVERSED` disimpan sebagai receipt 409 tanpa perubahan domain/audit. Validasi dan kegagalan autentikasi tidak menjadi receipt. Gangguan database/commit yang hasilnya belum pasti dikembalikan sebagai 503; client mempertahankan perintah asli. Secret HMAC perlu ikut dicadangkan bersama konfigurasi privat. Menggantinya secara sembarang membuat fingerprint perintah lama tidak cocok.

## Invariant PostgreSQL

- Transaksi berhasil memiliki tepat dua wallet berbeda, satu debit dan satu kredit dengan nominal yang sama.
- Saldo cache tiap wallet sama dengan agregat bertanda pada jurnalnya.
- Wallet biasa berada dalam rentang 0..1e12; sumber simulasi dalam rentang -1e14..0.
- TRANSFER memakai wallet pengirim yang dimiliki aktor. ALLOCATION dan REVERSAL membutuhkan Admin pada bukti domain.
- Reversal hanya merujuk TRANSFER dan mencerminkan nominal serta arah yang dibalik. Relasi asal unik.
- Transaksi, jurnal, receipt dan audit mempunyai guard UPDATE/DELETE/TRUNCATE. Bukti receipt/audit/domain diperiksa bersama.

Schema owner masih menguasai DDL dan dapat mengubah guard. API sehari-hari memakai role runtime dengan hak data, bukan pemilik schema. Ini bukan perlindungan terhadap administrator host/database yang sengaja mengganti schema.

## Pembacaan dan frontend

Rekonsiliasi memakai satu transaksi read-only repeatable-read agar angka wallet, jurnal dan total berasal dari snapshot yang konsisten. Detail jurnal juga dibaca dengan batas transaksi yang jelas. Pagination history memakai sequence descending, dengan halaman terpisah yang dapat melihat commit berbeda; sequence bukan waktu commit global. Tombol refresh memulai dari halaman terbaru.

React menyimpan nominal sebagai string. Konfirmasi menyebut penerima, jumlah dan catatan sebelum POST. Intent yang belum pasti disimpan per tab/pemilik sebelum dikirim dan dicoba ulang dengan key/body yang sama. Setelah hasil sah, UI mengambil saldo dan riwayat terkini. Receipt lama tidak dijadikan saldo terbaru.

Generasi identitas memagari request yang terlambat, termasuk JSON yang selesai diparse setelah logout. Pointer identitas lintas tab hanya berisi ID, tanpa credential. Login/logout/perubahan identitas memicu validasi ulang ke server; state akun sebelumnya dibersihkan. Form pembuatan akun yang memuat password mempertahankan intent hanya di memori tab.

## Batas operasi

Semua data uang sintetis dan hanya IDR integer. Tidak ada offline queue, WebSocket, bunga, biaya, pajak, multi-currency, payment gateway, mutasi akun produksi, atau klaim throughput. [Operasi](OPERATIONS.md) menjelaskan port, role, data dan batas pemulihan. [Verifikasi](VERIFICATION.md) mencatat skenario yang benar-benar dijalankan.
