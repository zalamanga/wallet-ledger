import { useState, type FormEvent } from "react";
import { Dialog, Field, Notice, PageHeading } from "./components";
import { parseAmount, rupiah } from "./money";
import type { Person, PersonBody, Transaction, TransferBody } from "./types";

export interface Review {
  path: "/transfers" | "/allocations";
  title: string;
  recipient: string;
  body: TransferBody;
}

export function TransferForm({ people, actorId, allocation, blocked, review }: {
  people: Person[]; actorId: string; allocation: boolean; blocked: boolean; review: (value: Review) => void;
}) {
  const [recipientId, setRecipientId] = useState("");
  const [amount, setAmount] = useState("");
  const [note, setNote] = useState("");
  const [error, setError] = useState("");
  const recipients = people.filter((person) => allocation || person.id !== actorId);
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (blocked) return;
    setError("");
    try {
      const recipient = recipients.find((person) => person.id === recipientId);
      if (!recipient) throw new Error("Pilih akun penerima terlebih dahulu.");
      const amount_minor = parseAmount(amount);
      const trimmed = note.trim();
      if (!trimmed) throw new Error("Tulis catatan transaksi.");
      review({ path: allocation ? "/allocations" : "/transfers", title: allocation ? "Alokasi saldo" : "Transfer saldo", recipient: recipient.full_name, body: { recipient_id: recipient.id, amount_minor, note: trimmed } });
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Periksa isian transaksi."); }
  }
  return <>
    <PageHeading title={allocation ? "Alokasi saldo" : "Kirim saldo"} description={allocation ? "Admin mencatat alokasi saldo sintetis dari dompet pendanaan ke akun pengguna." : "Saldo dipindahkan dari dompetmu ke dompet pengguna lain dalam workspace ini."} />
    <div className="transfer-layout"><section className="document transfer-document"><div className="document-caption"><span>Formulir {allocation ? "alokasi" : "transfer"}</span><span>IDR · Rupiah bulat</span></div>
      {error && <Notice tone="error">{error}</Notice>}
      <form onSubmit={submit}>
        <Field label="Penerima"><select required value={recipientId} onChange={(event) => setRecipientId(event.target.value)}><option value="">Pilih pengguna</option>{recipients.map((person) => <option key={person.id} value={person.id}>{person.full_name}</option>)}</select></Field>
        <Field label="Jumlah (IDR)" hint="Isi angka 1 sampai 1000000000 tanpa pemisah ribuan."><input name="amount" className="amount-input" type="text" inputMode="numeric" required maxLength={10} value={amount} onChange={(event) => setAmount(event.target.value)} /></Field>
        <Field label="Catatan"><textarea name="note" required maxLength={500} rows={3} value={note} onChange={(event) => setNote(event.target.value)} /></Field>
        <button type="submit" className="primary" disabled={blocked || recipients.length === 0}>{allocation ? "Tinjau alokasi" : "Tinjau transfer"}</button>
      </form>
    </section><aside className="form-aside"><h2>Sebelum dicatat</h2><p>Penerima, jumlah dan catatan akan ditampilkan lagi untuk kamu konfirmasi.</p><p>Hanya kirim sekali. Jika hasilnya belum diketahui, ulangi permintaan yang sama melalui pemberitahuan di atas.</p><p className="muted">Semua saldo digunakan untuk latihan. Tidak ada pencairan atau pembayaran nyata.</p></aside></div>
  </>;
}

export function ReviewDialog({ value, busy, blocked, close, confirm }: { value: Review; busy: boolean; blocked: boolean; close: () => void; confirm: () => void }) {
  const allocation = value.path === "/allocations";
  return <Dialog title={allocation ? "Konfirmasi alokasi" : "Konfirmasi transfer"} onClose={close} busy={busy}>
    <p>Periksa rincian berikut sebelum saldo latihan dicatat.</p>
    <dl className="confirmation-facts"><div><dt>Penerima</dt><dd>{value.recipient}</dd></div><div><dt>Jumlah IDR</dt><dd className="money confirmation-amount">{rupiah(value.body.amount_minor)}</dd></div><div><dt>Catatan</dt><dd>{value.body.note}</dd></div></dl>
    <div className="dialog-actions"><button type="button" onClick={close} disabled={busy}>Kembali</button><button type="button" className="primary" onClick={confirm} disabled={blocked}>{allocation ? "Catat alokasi" : "Kirim transfer"}</button></div>
  </Dialog>;
}

export function ReversalDialog({ transaction, close, confirm, blocked }: { transaction: Transaction; close: () => void; confirm: (reason: string) => void; blocked: boolean }) {
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (blocked) return;
    const trimmed = reason.trim();
    if (trimmed.length < 10) { setError("Jelaskan alasan pembalikan sedikitnya 10 karakter."); return; }
    confirm(trimmed);
  }
  return <Dialog title="Konfirmasi pembalikan" onClose={close}>
    <p>Transfer #{transaction.seq} akan dibalik dengan jurnal baru. Transaksi asal tetap tersimpan.</p>
    <dl className="confirmation-facts"><div><dt>Saldo dikembalikan dari</dt><dd>{transaction.to_name}</dd></div><div><dt>Ke pengirim asal</dt><dd>{transaction.from_name}</dd></div><div><dt>Jumlah</dt><dd className="money confirmation-amount">{rupiah(transaction.amount_minor)}</dd></div></dl>
    <p className="muted">Pembalikan hanya berhasil jika saldo penerima asal masih mencukupi.</p>
    {error && <Notice tone="error">{error}</Notice>}
    <form onSubmit={submit}><Field label="Alasan pembalikan" hint="10 sampai 500 karakter."><textarea required minLength={10} maxLength={500} rows={3} value={reason} onChange={(event) => setReason(event.target.value)} /></Field><div className="dialog-actions"><button type="button" onClick={close}>Kembali</button><button type="submit" className="primary" disabled={blocked}>Balikkan transfer</button></div></form>
  </Dialog>;
}

export function CreatePersonForm({ blocked, submit }: { blocked: boolean; submit: (body: PersonBody) => void }) {
  const [error, setError] = useState("");
  function handle(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (blocked) return;
    const form = event.currentTarget;
    const data = new FormData(form);
    const body = { full_name: String(data.get("full_name") ?? "").trim(), email: String(data.get("email") ?? "").trim(), password: String(data.get("password") ?? "") };
    if (new TextEncoder().encode(body.password).length > 128) { setError("Kata sandi awal paling panjang 128 byte."); return; }
    if (!body.full_name) { setError("Masukkan nama lengkap pengguna."); return; }
    setError("");
    submit(body);
    form.reset();
  }
  return <section className="document account-form"><h2>Buat akun Member</h2><p>Akun baru menerima satu dompet dengan saldo nol. Kata sandi awal hanya digunakan dalam permintaan ini.</p>
    {error && <Notice tone="error">{error}</Notice>}
    <form onSubmit={handle}><Field label="Nama lengkap"><input name="full_name" required maxLength={120} autoComplete="off" /></Field><Field label="Email akun"><input name="email" type="email" required maxLength={254} autoComplete="off" /></Field><Field label="Kata sandi awal" hint="12 sampai 128 byte. Tidak disimpan dalam penyimpanan browser."><input name="password" type="password" required minLength={12} maxLength={128} autoComplete="new-password" /></Field><button className="primary" type="submit" disabled={blocked}>Buat akun</button></form>
  </section>;
}
