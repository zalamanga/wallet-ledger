# Frontend WalletLedger: dari sesi sampai bukti jurnal

Frontend ini memakai React dan TypeScript. Browser membaca data dari Go API melalui jalur `/api`, menggunakan cookie sesi yang dikirim browser. Tampilan tidak menghitung atau memutuskan saldo transaksi. PostgreSQL dan backend melakukan pencatatan; frontend membantu pengguna meninjau permintaan dan memahami hasilnya.

Semua nominal adalah string rupiah bulat. `"25000"` berarti Rp25.000. Nilai tersebut tidak dibagi seratus dan tidak dikonversi menjadi floating point.

## 1. Susunan file

| File | Tanggung jawab |
| --- | --- |
| [package.json](../frontend/package.json) dan [package-lock.json](../frontend/package-lock.json) | Perintah pengembangan serta versi dependensi yang dikunci. |
| [index.html](../frontend/index.html) | Dokumen bahasa Indonesia, judul halaman, viewport yang mengizinkan zoom, dan titik pemasangan React. |
| [vite.config.ts](../frontend/vite.config.ts) | React, server pengembangan di port 5175, proxy `/api` ke backend lokal 8171, dan konfigurasi unit test. |
| [tsconfig.json](../frontend/tsconfig.json) | Pemeriksaan TypeScript strict, target ES2022, serta tipe DOM dan Vite. |
| [main.tsx](../frontend/src/main.tsx) | Memasang aplikasi dan stylesheet. |
| [types.ts](../frontend/src/types.ts) | Kontrak DTO, jenis peran, transaksi, jurnal dan permintaan. |
| [money.ts](../frontend/src/money.ts) | Validasi rupiah, format tampilan, tanda masuk/keluar dan tanggal. |
| [identity.ts](../frontend/src/identity.ts) | Pemilik sesi, penanda perubahan identitas dan penolakan respons terlambat. |
| [api.ts](../frontend/src/api.ts) | Fetch, error server, header identitas, pemulihan CSRF, dan hasil permintaan yang belum pasti. |
| [commands.ts](../frontend/src/commands.ts) | Membekukan isi permintaan dan menyimpan kunci idempotensi yang belum selesai. |
| [useCommands.ts](../frontend/src/useCommands.ts) | Menjalankan satu permintaan, mempertahankan hasil yang belum pasti dan menyelesaikan pending state. |
| [App.tsx](../frontend/src/App.tsx) | Pemeriksaan sesi, login/setup, revalidasi lintas tab dan logout. |
| [Auth.tsx](../frontend/src/Auth.tsx) | Formulir masuk atau pembuatan workspace pertama. |
| [Workspace.tsx](../frontend/src/Workspace.tsx) | Navigasi, pembacaan data utama, ringkasan, riwayat dan penghubung tindakan. |
| [Forms.tsx](../frontend/src/Forms.tsx) | Transfer, alokasi, konfirmasi, pembalikan dan pembuatan akun. |
| [Transactions.tsx](../frontend/src/Transactions.tsx) | Baris register, filter, detail transaksi dan jurnal dua sisi. |
| [Admin.tsx](../frontend/src/Admin.tsx) | Daftar akun, rekonsiliasi dan audit. |
| [components.tsx](../frontend/src/components.tsx) | Dialog native, label formulir, pemberitahuan dan judul halaman. |
| [styles.css](../frontend/src/styles.css) | Arah visual register, susunan responsif, fokus keyboard serta keadaan kontrol. |

Tiga file pengujian berada bersama kode yang diperiksa: [money.test.ts](../frontend/src/money.test.ts), [commands.test.ts](../frontend/src/commands.test.ts), dan [api.test.ts](../frontend/src/api.test.ts). Pengujian browser aplikasi penuh berada di tingkat proyek, terpisah dari unit test ini.

## 2. Mulai dari kontrak, bukan dari tabel dummy

Jika membangun antarmuka sejenis dari nol, tulis kontrak respons terlebih dahulu. [CONTRACT.md](./CONTRACT.md) menentukan nama field, izin, batas nominal dan perilaku retry.

Contohnya, `Wallet.balance_minor` bertipe `string`, sedangkan `Transaction.seq` bertipe `number`. Nominal membutuhkan ketepatan aritmetika; nomor urut dipakai untuk register dan pagination. Keduanya mempunyai tujuan berbeda.

`AuthView` berisi pengguna, workspace dan token CSRF. `Owner` hanya berisi pasangan `user_id` dan `workspace_id`. Pasangan ini mengikat permintaan yang dibuat pengguna kepada sesi yang benar.

`TransactionDetail` memisahkan transaksi dan baris jurnal. Dengan bentuk tersebut, bukti bisa memperlihatkan kedua dompet, arah DEBIT/CREDIT serta total masing-masing sisi tanpa mengarang data di browser.

Setelah tipe disepakati, buat pembaca API, pengelola identitas, lalu formulir. Halaman diisi hasil API sesungguhnya. Workspace kosong tetap menampilkan petunjuk yang sesuai, tanpa statistik atau riwayat buatan.

## 3. Nominal uang di frontend

### `parseAmount(value)`

Fungsi ini menerima teks dari kolom Jumlah (IDR). Pola `^[1-9][0-9]*$` menolak nol, tanda positif/negatif, pecahan, spasi, notasi eksponen dan nol di depan.

Setelah bentuknya benar, `BigInt` memeriksa batas 1 sampai 1.000.000.000 rupiah. Nilai yang dikirim tetap string asli. Pemisah ribuan hanya digunakan saat menampilkan, bukan saat mengirim JSON.

Contoh yang diterima: `"1"`, `"25000"`, `"1000000000"`. Contoh yang ditolak: `"01"`, `"25.000"`, `"25000.5"`, `"1e6"`.

### `rupiah(value)`

Fungsi ini menambahkan awalan Rp dan titik ribuan pada string digit. Format saldo negatif dompet pendanaan tetap tepat hingga rentang sistem yang ditetapkan kontrak. Tidak ada `parseFloat`, pembulatan nominal, atau `toFixed`.

### `amountPerspective(transaction, walletId)`

Jika dompet pengguna berada di sisi pengirim, nilai ditampilkan dengan tanda minus dan teks Keluar. Jika berada di sisi penerima, nilai memakai plus dan teks Masuk. Untuk transaksi antarpengguna yang dilihat Admin, nominal tetap netral dan kedua pihak ditampilkan.

`kindLabel` menerjemahkan jenis transaksi menjadi Transfer, Alokasi latihan atau Pembalikan. `dateTime` menampilkan waktu server sesuai lokal browser dengan format Indonesia.

Validasi frontend membantu pengisian formulir. Backend tetap memeriksa bentuk nominal, batas dan kepemilikan sebelum mencatat transaksi.

## 4. Identitas dan respons yang terlambat

### `IdentityFence`

Setiap pemeriksaan identitas mempunyai nomor generasi. `advance()` mengganti generasi ketika sesi harus diperiksa ulang. `capture()` membaca nomor yang berlaku. `assert()` menolak pekerjaan yang berasal dari generasi lama melalui `StaleIdentity`.

Mengapa perlu diperiksa dua kali? Fetch dapat selesai sebelum pergantian akun, sementara pembacaan JSON baru selesai setelahnya. Pemeriksaan hanya sebelum `fetch()` tidak mencegah data akun lama masuk belakangan.

`publicRequest` memeriksa generasi sebelum mengirim, sesudah respons jaringan diterima, dan sesudah JSON selesai dibaca. Jalur error juga memeriksa generasi sebelum meneruskan error ke UI.

### `authOwner` dan `sameOwner`

Keduanya membentuk dan membandingkan pasangan workspace serta pengguna. Mencocokkan pengguna saja tidak cukup untuk permintaan yang terikat workspace.

### `writeIdentityPointer`

LocalStorage hanya memuat penanda identitas tanpa cookie, token atau kata sandi. Peristiwa `storage` memberi tahu tab lain agar menghapus tampilan sesi sebelumnya dan membaca `/auth/me` lagi. Penanda tersebut bukan bukti login; server tetap menjadi sumber identitas.

Perubahan dari penanda logout `null` ke pengguna yang masuk juga memicu revalidasi. UI tidak memakai penanda ini untuk memulihkan data dompet dari penyimpanan offline.

## 5. Lapisan API

### `ApiError` dan `UnknownOutcome`

`ApiError` membawa status HTTP, kode error, pesan, serta penanda replay error dari server. Contohnya, saldo tidak cukup adalah hasil yang sudah diketahui: transaksi tidak dicatat.

`UnknownOutcome` berarti browser belum dapat memastikan hasil. Koneksi bisa terputus setelah database berhasil commit, respons 503 dapat menandakan hasil commit belum pasti, atau respons sukses tidak dapat dibaca. Dalam keadaan tersebut, membuat kunci baru berisiko mencatat tindakan kedua.

### `ApiClient.read(path)`

Metode ini membaca data menggunakan cookie browser. Semua respons terikat generasi identitas. Respons 401 atau `IDENTITY_CHANGED` memulai revalidasi sesi dan menghentikan pemakaian client lama.

### `ApiClient.command(command)`

Sebelum mengirim, client memastikan pemilik command sama dengan sesi yang dipegangnya. Header POST berisi:

```text
Content-Type: application/json
X-CSRF-Token: token sesi saat ini
X-Wallet-Actor: UUID pengguna pembuat permintaan
X-Wallet-Workspace: UUID workspace
Idempotency-Key: UUID permintaan yang dibekukan
```

`command.body` adalah teks JSON yang sudah disimpan, bukan hasil membaca ulang formulir. Ketika mendapat `CSRF_INVALID`, client membaca `/auth/me` sekali. Retry hanya dilakukan jika pasangan pemilik masih sama. Kunci, path, metode dan body tetap identik; hanya token CSRF yang diperbarui.

`IDENTITY_CHANGED` tidak menjalankan retry otomatis. Error bisnis 409 diteruskan sebagai hasil yang diketahui. Network error, respons 5xx, atau respons sukses yang tidak terbaca dipertahankan sebagai hasil yang belum pasti.

### `ApiClient.logout()`

Logout dikirim ke server agar sesi benar-benar dicabut. Jika token CSRF perlu diperbarui, pemulihan dilakukan sekali dengan pemeriksaan pemilik yang sama. Setelah berhasil, aplikasi membersihkan sesi tampilannya dan memeriksa keadaan login kembali.

## 6. Kunci idempotensi dan pending state

### `makeCommand(owner, path, body, title)`

Fungsi ini membuat UUID baru hanya saat pengguna benar-benar mengonfirmasi tindakan. Ia menyalin pemilik, menserialisasi body, dan membekukan objek command. Perubahan pada isian atau objek asal tidak mengubah intent yang sudah dibuat.

### `savePending`, `readPending`, `removePending`

Sebelum POST finansial, SessionStorage menyimpan key, metode, path, body dan pemilik. Namespace penyimpanannya mencakup UUID workspace dan pengguna. `readPending` menolak pemilik berbeda, bentuk tidak valid, path yang tidak diizinkan, serta field body tambahan.

Pembuatan akun mengandung kata sandi. Command `/people` tetap berada di memori tab dan tidak ditulis ke SessionStorage. Pending akun yang belum pasti perlu diselesaikan sebelum menutup tab.

SessionStorage terikat sesi tab, bukan antrean pembayaran permanen. Jika penyimpanan tersebut tidak dapat ditulis, aplikasi menolak mengirim transaksi baru dan menjelaskan masalahnya.

### `useCommands`

Hook ini mengizinkan satu command aktif. Ref lock menahan klik ganda sebelum render React berikutnya. Command lain tidak dapat menggantikan command yang hasilnya belum diketahui.

Saat sukses, pending selesai lalu UI memuat ulang wallet dan history. Saat terjadi penolakan bisnis yang pasti, pending juga selesai dan pesan server ditampilkan. Saat hasil belum pasti, command tetap tersedia melalui Ulangi permintaan yang sama. Pengulangan ini merupakan tindakan eksplisit pengguna.

Tidak ada retry finansial karena timer atau koneksi baru tersambung. Mode offline menonaktifkan tombol kirim. Aplikasi tidak mengantrekan transfer untuk dikirim otomatis nanti.

## 7. Setup, login dan pergantian sesi

`App` mulai dengan pemeriksaan `/auth/me`. Bila server menjawab 401, `/auth/status` menentukan apakah formulir pertama adalah setup atau login.

`authenticate` mengambil token CSRF anonim, mengirim setup/login, kemudian memasang `ApiClient` untuk sesi yang baru. Formulir setup meminta nama workspace dan nama lengkap. Akun pertama menjadi Admin sesuai aturan server.

`revalidate` mengganti generasi dan menghapus seluruh komponen Workspace sebelum membaca identitas baru. Karena Workspace diberi key berdasarkan pemilik dan generasi, dompet, detail, draft serta halaman Admin lama tidak dipertahankan dalam sesi berikutnya.

`Auth` memakai formulir native dengan label terlihat dan atribut autocomplete yang sesuai. Kata sandi tidak dimasukkan ke URL maupun penyimpanan browser oleh kode aplikasi. Pesan login gagal berasal dari API.

## 8. Data workspace dan riwayat

`Workspace.refresh` mengambil wallet, people dan halaman transaksi terbaru. State baru dipasang setelah seluruh pembacaan tersebut berhasil dan generasi pembacaan masih berlaku. Timestamp di toolbar menunjukkan saat pembacaan berhasil, bukan waktu saldo terakhir berubah di server.

Jika refresh gagal sementara data lama masih ada, UI menjelaskan bahwa data sebelumnya tetap terlihat. Tombol Perbarui data mengambil ulang halaman terbaru dan mengulang pembacaan tampilan Admin yang sedang terbuka.

Ringkasan menampilkan dompet pengguna yang sedang masuk dan lima transaksi terbaru dari hasil server. Admin dapat membaca transaksi seluruh workspace. Member hanya menerima transaksi yang diizinkan server untuk dompetnya.

`loadMore` menggunakan `next_before` dari server. Baris digabung berdasarkan ID agar tidak muncul dua kali. Pagination bukan snapshot transaksi seluruh waktu: transaksi baru dapat masuk setelah halaman pertama dibaca. Pesan di halaman menjelaskan bahwa filter berlaku pada baris yang sudah dimuat dan refresh memulai lagi dari data terbaru.

## 9. Transfer dan alokasi

`TransferForm` memakai komponen yang sama untuk transfer dan alokasi, dengan perbedaan izin serta endpoint. Transfer tidak menawarkan akun sendiri sebagai penerima. Alokasi Admin dapat memilih akun pengguna termasuk akun Admin.

Alurnya:

1. Pilih penerima dari `/people`.
2. Isi rupiah bulat serta catatan.
3. Tekan Tinjau transfer atau Tinjau alokasi.
4. `ReviewDialog` menampilkan nama penerima, jumlah dan catatan yang sudah disalin.
5. Tekan Kirim transfer atau Catat alokasi untuk membuat command dan melakukan POST.

Membuka atau menutup konfirmasi tidak mengubah saldo. Tombol kirim akhir ikut dinonaktifkan jika browser menjadi offline setelah dialog terbuka. Setelah hasil sukses diketahui, draft finansial direset dan bukti transaksi dibuka.

## 10. Detail dan pembalikan

`TransactionList` memakai nomor urut sebagai identitas register yang dibaca manusia. Tombol setiap baris memiliki nama aksesibel Buka transaksi diikuti nomor urut. ID UUID tetap tersedia di detail.

`TransactionDialog` membaca detail aktual dari `/transactions/{id}`. Ia menampilkan kedua jurnal dan jumlah debit/kredit. Bukti tidak digunakan sebagai saldo saat ini, termasuk ketika bukti tersebut berasal dari replay permintaan lama.

Admin dapat menekan Tinjau pembalikan pada transfer yang belum dibalik. `ReversalDialog` meminta alasan dan menjelaskan arah pengembalian saldo. Balikkan transfer membuat command baru ke endpoint reverse. Backend memutuskan apakah saldo penerima masih cukup dan apakah transfer sudah pernah dibalik.

Transaksi asal tetap tersimpan. Hubungan `reversal_id` dan `reversal_of` menjadi tombol untuk membuka bukti terkait, tanpa merekayasa baris jurnal di frontend.

## 11. Tampilan Admin

### `Accounts`

`useRemote` membaca `/admin/people` dan menangani loading, kegagalan serta muat ulang. `CreatePersonForm` mengirim nama lengkap, email dan kata sandi awal melalui pengelola command. Setelah pengguna baru berhasil dibuat, daftar pengguna dan pilihan penerima dibaca ulang.

Field kata sandi direset setelah command dibuat. Jika hasil belum pasti, salinan body tetap berada di memori tab untuk retry identik. Password tidak ditampilkan pada daftar akun atau bukti.

### `Reconcile`

Frontend hanya menampilkan laporan `/reconciliation`. Perbandingan saldo tersimpan dan hasil jurnal dihitung backend dalam satu transaksi pembacaan konsisten. Browser tidak menyimpulkan saldo dari halaman transaksi yang mungkin belum lengkap.

Komposisi halaman menonjolkan hasil pemeriksaan, total debit/kredit, lalu rincian setiap dompet. Saldo negatif dompet pendanaan dijelaskan sebagai pasangan alokasi sintetis.

### `Audit`

Halaman ini mengambil `/audit` dengan pagination berbasis nomor urut. Ia menampilkan aktor, jenis peristiwa, waktu dan ID objek. Tidak ada peristiwa lokal yang ditambahkan hanya karena pengguna menekan tombol.

## 12. Dialog, keyboard dan susunan responsif

`Dialog` menggunakan elemen native `<dialog>` dan `showModal()`. Browser mengelola fokus modal. Escape menutup dialog bila permintaan tidak sedang dikirim. Setelah ditutup, fokus kembali ke kontrol asal bila kontrol tersebut masih ada.

`Field` memasangkan label terlihat dan kontrol melalui `htmlFor` serta ID stabil dari `useId`. Teks bantuan ditempatkan di luar label lalu dihubungkan sebagai `aria-describedby`. Nama aksesibel kontrol hanya berisi nama field; pilihan dalam select dan teks bantuan tidak tercampur ke dalamnya. Tombol memiliki ukuran minimal 44 piksel, pointer, keadaan hover/active/disabled dan outline fokus yang terlihat. Skip link membantu pengguna keyboard langsung menuju isi workspace.

[DESIGN.md](../DESIGN.md) menjelaskan warna hutan, kertas hangat, angka tabular dan komposisi register. Ringkasan saldo, formulir, jurnal dan audit memiliki susunan sesuai isi kerjanya. Tidak ada grafik atau metrik yang memerlukan data buatan.

Pada layar sempit, navigasi membungkus, baris transaksi menjadi blok dan formulir menumpuk. Tabel jurnal dan rekonsiliasi memiliki wilayah scroll internal berlabel. Halaman tetap dapat menggulir secara vertikal ketika teks diperbesar; tidak ada penguncian zoom.

## 13. Pengujian dan cara menjelaskan saat interview

Unit test memeriksa angka batas, format persis, pemilik pending, pengecualian kata sandi, retry CSRF dengan key/body yang tetap, pergantian actor, hasil 503 yang belum pasti, serta respons fetch dan JSON yang terlambat. Unit test memakai stub transport secara sengaja untuk mengendalikan urutan kejadian, bukan untuk menggantikan pengujian database atau browser aplikasi penuh.

Jalankan frontend unit test dengan `npm test` dari direktori frontend. `npm run build` memeriksa TypeScript dan membuat distribusi Vite. Langkah aplikasi penuh, database, Docker dan bukti verifikasi ada di [OPERATIONS.md](./OPERATIONS.md) serta [VERIFICATION.md](./VERIFICATION.md).

Penjelasan singkat yang bisa dipakai:

> Saya memisahkan formulir, transport API dan pengelola intent transaksi. Nominal disimpan sebagai string rupiah agar tidak terpengaruh floating point. Sebelum POST, pengguna meninjau penerima dan jumlah, lalu aplikasi membekukan body serta kunci idempotensi. Kalau respons hilang, aplikasi mengulang intent yang sama. Setelah sukses atau replay, saldo dibaca kembali dari server. Pemeriksaan generasi identitas mencegah respons akun sebelumnya masuk ketika sesi berganti.

Frontend membantu pengguna membaca dan mengirim tindakan dengan jelas. Konsistensi saldo, transaksi atomik, otorisasi dan jurnal tetap diperiksa server serta database.
