import { useState, type FormEvent } from "react";
import { Field, Notice } from "./components";
import type { AuthView } from "./types";

export function Auth({ setup, workspaceName, submit }: {
  setup: boolean; workspaceName: string | null;
  submit: (setup: boolean, body: Record<string, string>) => Promise<AuthView>;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    const form = event.currentTarget;
    const data = new FormData(form);
    const body: Record<string, string> = { email: String(data.get("email") ?? "").trim(), password: String(data.get("password") ?? "") };
    if (setup) {
      body.full_name = String(data.get("full_name") ?? "").trim();
      body.workspace_name = String(data.get("workspace_name") ?? "").trim();
    }
    if (new TextEncoder().encode(body.password).length > 128) { setError("Kata sandi paling panjang 128 byte."); return; }
    setBusy(true); setError("");
    try { await submit(setup, body); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Tidak dapat masuk. Coba lagi."); }
    finally { setBusy(false); }
  }
  return <div className="auth-page">
    <header className="auth-brand"><span className="wordmark">WalletLedger<span className="wordmark-mark"> /</span></span><p>Register saldo latihan</p></header>
    <main className="auth-layout">
      <section className="auth-intro"><p className="section-label">IDR · Saldo simulasi</p><h1>Setiap perpindahan,<br />dua sisi jurnal.</h1>
        <p>Catat alokasi dan transfer saldo latihan. Telusuri debit, kredit, serta hasil rekonsiliasinya dalam satu workspace.</p>
        <dl className="auth-facts"><div><dt>Satuan</dt><dd>Rupiah bulat</dd></div><div><dt>Pencatatan</dt><dd>Debit = kredit</dd></div><div><dt>Penggunaan</dt><dd>Demonstrasi lokal</dd></div></dl>
        <p className="muted">Saldo dalam aplikasi ini bersifat sintetis dan tidak terhubung ke uang atau pembayaran nyata.</p>
      </section>
      <section className="auth-form document"><h2>{setup ? "Buat workspace latihan" : "Masuk workspace"}</h2>
        <p>{setup ? "Akun pertama menjadi Admin. Saldo awal seluruh dompet adalah nol." : workspaceName ?? "Gunakan akun yang dibuat oleh Admin workspace."}</p>
        {error && <Notice tone="error">{error}</Notice>}
        <form onSubmit={handleSubmit}>
          {setup && <><Field label="Nama workspace"><input name="workspace_name" required maxLength={100} autoComplete="organization" /></Field>
            <Field label="Nama lengkap"><input name="full_name" required maxLength={120} autoComplete="name" /></Field></>}
          <Field label="Email"><input name="email" type="email" required maxLength={254} autoComplete="username" /></Field>
          <Field label="Kata sandi" hint={setup ? "12 sampai 128 byte. Simpan kata sandi akun latihanmu." : undefined}><input name="password" type="password" required minLength={setup ? 12 : undefined} maxLength={128} autoComplete={setup ? "new-password" : "current-password"} /></Field>
          <button className="primary" type="submit" disabled={busy}>{busy ? "Memeriksa akun…" : setup ? "Buat workspace" : "Masuk"}</button>
        </form>
      </section>
    </main>
  </div>;
}
