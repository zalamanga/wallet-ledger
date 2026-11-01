# REST API

Base Docker `http://127.0.0.1:8170/api`. [Kontrak implementasi](CONTRACT.md) memuat semua DTO. OpenAPI tersedia pada `/api/openapi.json`; nilai `servers[0].url` adalah `/api`.

## Sesi dan header

GET `/auth/csrf` memperoleh token dan cookie anonim. Setup/login mengirim Origin, Content-Type JSON dan X-CSRF-Token. Browser/HTTP client mempertahankan cookie jar. Respons AuthView berisi user, workspace dan token CSRF sesi. Cookie sesi HttpOnly; logout mencabut record server.

Untuk setiap mutation terautentikasi selain logout, gunakan header berikut dengan nilai lokal yang benar:

```http
Origin: http://127.0.0.1:8170
Content-Type: application/json
X-CSRF-Token: <token sesi>
X-Wallet-Actor: <UUID user saat ini>
X-Wallet-Workspace: <UUID workspace saat ini>
Idempotency-Key: <UUID tindakan asli>
```

Token CSRF dan key idempotensi berbeda fungsi. Logout memakai header sesi/identitas/CSRF tetapi tidak memerlukan Idempotency-Key. GET tidak menulis domain dan tidak memerlukan header mutation. `/auth/me` memberikan identitas serta CSRF sesi terbaru.

## Transfer

```http
POST /api/transfers
```

```json
{
  "recipient_id": "<UUID pengguna lain>",
  "amount_minor": "50000",
  "note": "Training: pembagian biaya kegiatan"
}
```

Penerima adalah ID pengguna dari GET `/people`, bukan ID wallet. Wallet pengirim ditentukan dari sesi. Nominal harus string integer positif kanonis, maksimal `"1000000000"`. API menolak angka JSON, pecahan, tanda plus/minus, notasi eksponen dan leading zero. Batas ini diterapkan server meskipun frontend telah memvalidasi.

Response 201 berupa `{request_id,replayed,transaction,user}`. Pada transfer, `transaction` memuat detail transaksi dan dua jurnal, sedangkan `user` null. Pada pembuatan akun, `user` terisi dan `transaction` null. Nominal, total dan balance selalu string.

## Alokasi dan pembalikan

Admin POST `/allocations` memakai bentuk body transfer yang sama, tetapi sumbernya wallet sistem. Label UI menyebut saldo latihan. Member yang mencoba endpoint tersebut menerima 403.

Admin POST `/transactions/{id}/reverse` mengirim `{ "reason": "Training: koreksi transfer yang sudah ditinjau" }`. Reason 10..500 karakter. Hanya TRANSFER dapat dibalik, satu kali, dengan saldo penerima asal yang cukup. Hasil adalah transaksi REVERSAL baru yang menunjuk asal; tidak ada endpoint pengeditan/penghapusan jurnal.

## Izin baca

Member membaca wallet sendiri serta transaksi yang melibatkan wallet itu. Detail jurnal menyebut kedua pihak yang terlibat pada transaksi yang boleh dibaca. ID wallet/transaksi asing menghasilkan404. Admin membaca seluruh wallet, transaksi, rekonsiliasi dan audit dalam workspace tunggal. Directory penerima tidak memuat saldo.

History dan audit mendukung `before` sequence positif serta `limit` 1..100, default 50. Respons memuat `items` dan `next_before`. Filter tampilan bekerja pada halaman yang sedang dimuat. Halaman terpisah bukan snapshot yang dibekukan selama transaksi lain commit.

## Error dan retry

| Status/code | Tindakan client |
|---|---|
| 401|Validasi ulang sesi; bersihkan data privat lama|
| 403 `CSRF_INVALID`|Ambil CSRF sesi sekali, retry intent/key/pemilik yang sama|
| 403 `IDENTITY_CHANGED`|Hentikan tindakan akun sebelumnya|
| 404|Resource tidak ada atau tidak dapat dibaca|
| 409 `IDEMPOTENCY_CONFLICT`|Key lama merujuk isi berbeda; jangan ubah body key itu|
| 409 `INSUFFICIENT_FUNDS`|Penolakan bisnis tersimpan; tindakan baru diperlukan setelah keadaan berubah|
| 409 `LIMIT_EXCEEDED`, `ALREADY_REVERSED`|Penolakan bisnis yang stabil pada key tersebut|
| 413/422|Perbaiki ukuran/bentuk input, field tak dikenal atau nominal|
| 429|Hormati Retry-After|
| 503/jaringan sesudah pengiriman|Hasil mungkin belum pasti; pertahankan intent asli|

Error memakai `{detail,code}`. Replay penolakan mempertahankan status/body asal dan header `Idempotency-Replayed: true`. Replay sukses menyatakan `replayed:true`. Membaca ulang saldo/history sesudah sukses diperlukan karena receipt bersifat historis.
