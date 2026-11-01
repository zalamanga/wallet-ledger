# Membuat dan menjelaskan WalletLedger

Mulai dengan aturan uang, kemudian database, API dan tampilan. Transfer perlu mengubah dua saldo sekaligus. Jika salah satu langkah gagal, seluruh perubahannya harus dibatalkan. Frontend meminta hasil ke backend; PostgreSQL yang menentukan apakah perubahan boleh commit.

## Urutan membuat proyek

| Tahap | Yang dikerjakan | Tujuan |
|---|---|---|
|1|Definisikan nominal integer, peran dan jenis transaksi|Aturan mempunyai bentuk yang dapat diuji|
|2|Buat schema wallet, transaksi, jurnal dan receipt|Saldo mempunyai bukti asal, bukan angka yang ditimpa bebas|
|3|Tambahkan sesi, CSRF dan expected identity|Perintah terikat akun dan sesi yang tepat|
|4|Implementasikan alokasi dan transfer dalam transaksi SQL|Dua saldo serta jurnal berubah bersama|
|5|Tambahkan idempotensi dan pembalikan|Retry mempertahankan hasil; koreksi tetap mempunyai riwayat|
|6|Buat query riwayat dan rekonsiliasi|Pengguna dan reviewer dapat memeriksa hasilnya|
|7|Bangun form React dan dokumen konfirmasi|Nominal/penerima jelas sebelum dikirim|
|8|Simpan intent yang hasilnya belum pasti|Gangguan jaringan dapat ditangani dengan key asli|
|9|Uji konkurensi, rollback, izin dan browser|Aturan diperiksa melalui database dan UI sebenarnya|
|10|Jalankan Docker, capture layar, tulis hasil|Orang lain dapat mengulang demo dan menilai batasnya|

## Contoh transfer

Ayu memiliki saldo latihan 500.000 rupiah dan mengirim 125.000 rupiah ke Dimas. Server mengunci kedua wallet dalam urutan yang tetap, memeriksa saldo, mencatat debit Ayu 125.000 dan kredit Dimas 125.000, memperbarui saldo, lalu menyimpan receipt dan audit dalam transaksi yang sama.

Jika request sudah commit tetapi respons tidak sampai, pengiriman ulang memakai key dan body asli. Server membaca receipt, bukan memotong saldo lagi. Receipt menyatakan hasil saat itu; saldo terkini dibaca ulang karena transaksi lain mungkin sudah terjadi.

Jika nominal melebihi saldo, hasil penolakan bisnis juga tersimpan dengan key tersebut. Setelah menerima saldo tambahan, pengguna perlu membuat tindakan baru untuk mencoba transfer yang baru. Mengulang key lama tetap berarti meminta hasil tindakan lama.

## Cara membaca source

Panduan [backend](BACKEND-GUIDE.md) menghubungkan setiap file dengan schema, route, autentikasi, lock dan query. Panduan [frontend](FRONTEND-GUIDE.md) menjelaskan state sesi, nominal string, form konfirmasi dan intent pending. Baca [kontrak](CONTRACT.md) ketika membandingkan bentuk request/response antarlapisan.

Untuk melihat aplikasi, ikuti [README](../README.md). Untuk terminal development dan hak database, baca [operasi](OPERATIONS.md). Gunakan [diagram](FLOWS.md) saat menjelaskan urutan request.

## Penjelasan singkat saat interview

“Saya membuat simulator wallet dengan Go, PostgreSQL dan React. Masalah utama yang saya tangani adalah dua transfer yang memakai saldo bersamaan serta request yang dicoba ulang setelah respons hilang. Saya mengunci wallet dengan urutan tetap, mencatat dua baris jurnal dan menyimpan receipt dalam transaksi yang sama. PostgreSQL juga memeriksa bahwa jurnal seimbang dan saldo cache sesuai jurnal. Browser menampilkan konfirmasi dan mempertahankan key asli ketika hasil request belum pasti.”

Lanjutkan dengan demo: transfer berhasil, saldo kurang, detail debit/kredit, pembalikan, lalu rekonsiliasi. Tunjukkan [hasil verifikasi](VERIFICATION.md) untuk skenario yang benar-benar diuji, dan jelaskan bahwa data serta alokasi saldo adalah simulasi.
