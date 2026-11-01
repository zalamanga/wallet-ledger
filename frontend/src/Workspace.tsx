import { useEffect, useRef, useState } from "react";
import { Accounts, Audit, Reconcile } from "./Admin";
import type { ApiClient } from "./api";
import { makeCommand } from "./commands";
import { Empty, Notice, PageHeading } from "./components";
import { ReviewDialog, ReversalDialog, TransferForm, type Review } from "./Forms";
import { StaleIdentity } from "./identity";
import { dateTime, rupiah } from "./money";
import { filterTransactions, HistoryFilters, TransactionDialog, TransactionList } from "./Transactions";
import type { AuthView, MutationResult, Page, Person, PersonBody, Transaction, Wallet } from "./types";
import { useCommands } from "./useCommands";

type View = "summary" | "transfer" | "history" | "allocation" | "accounts" | "reconcile" | "audit";
const memberNavigation: [View, string][] = [["summary", "Ringkasan"], ["transfer", "Kirim saldo"], ["history", "Riwayat"]];
const adminNavigation: [View, string][] = [["allocation", "Alokasi saldo"], ["accounts", "Akun pengguna"], ["reconcile", "Rekonsiliasi"], ["audit", "Audit"]];
interface Data { wallets: Wallet[]; people: Person[]; transactions: Page<Transaction> }

export function Workspace({ auth, client, logout }: { auth: AuthView; client: ApiClient; logout: () => Promise<void> }) {
  const admin = auth.user.role === "Admin";
  const [view, setView] = useState<View>("summary");
  const [data, setData] = useState<Data | null>(null);
  const [loading, setLoading] = useState(true);
  const [moreLoading, setMoreLoading] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [updated, setUpdated] = useState<string | null>(null);
  const [refreshToken, setRefreshToken] = useState(0);
  const [formEpoch, setFormEpoch] = useState(0);
  const [online, setOnline] = useState(navigator.onLine);
  const [loggingOut, setLoggingOut] = useState(false);
  const [review, setReview] = useState<Review | null>(null);
  const [detailId, setDetailId] = useState<string | null>(null);
  const [reversal, setReversal] = useState<Transaction | null>(null);
  const [search, setSearch] = useState("");
  const [kind, setKind] = useState("");
  const readGeneration = useRef(0);
  const paginationLock = useRef(false);
  const alive = useRef(true);

  async function refresh() {
    const current = ++readGeneration.current;
    setLoading(true); setError("");
    try {
      const [wallets, people, transactions] = await Promise.all([
        client.read<{ items: Wallet[] }>("/wallets"),
        client.read<{ items: Person[] }>("/people"),
        client.read<Page<Transaction>>("/transactions?limit=50"),
      ]);
      if (!alive.current || current !== readGeneration.current) return;
      setData({ wallets: wallets.items, people: people.items, transactions });
      setUpdated(new Date().toISOString());
      setRefreshToken((value) => value + 1);
    } catch (cause) {
      if (alive.current && current === readGeneration.current && !(cause instanceof StaleIdentity)) setError(cause instanceof Error ? cause.message : "Data workspace belum dapat dimuat.");
    } finally { if (alive.current && current === readGeneration.current) setLoading(false); }
  }

  function accepted(result: MutationResult) {
    setNotice(result.transaction ? `${result.replayed ? "Bukti permintaan sebelumnya ditemukan" : "Transaksi berhasil dicatat"}. Saldo terkini sedang dimuat kembali.` : `Akun ${result.user?.full_name ?? "pengguna"} berhasil dibuat.`);
    if (result.transaction) { setDetailId(result.transaction.transaction.id); setFormEpoch((value) => value + 1); }
    void refresh();
  }
  const commands = useCommands(client, accepted);
  const blocked = !online || commands.busy || commands.pending !== null || loggingOut;

  useEffect(() => {
    alive.current = true;
    void refresh();
    const changed = () => setOnline(navigator.onLine);
    window.addEventListener("online", changed); window.addEventListener("offline", changed);
    return () => { alive.current = false; readGeneration.current += 1; window.removeEventListener("online", changed); window.removeEventListener("offline", changed); };
  }, [client]);

  async function loadMore() {
    const before = data?.transactions.next_before;
    if (before == null || paginationLock.current) return;
    const current = readGeneration.current;
    paginationLock.current = true; setMoreLoading(true);
    try {
      const result = await client.read<Page<Transaction>>(`/transactions?limit=50&before=${before}`);
      if (!alive.current || current !== readGeneration.current) return;
      setData((old) => old ? { ...old, transactions: { ...result, items: [...old.transactions.items, ...result.items].filter((item, index, all) => all.findIndex((row) => row.id === item.id) === index) } } : old);
    } catch (cause) { if (alive.current && !(cause instanceof StaleIdentity)) setError(cause instanceof Error ? cause.message : "Transaksi lama belum dapat dimuat."); }
    finally { paginationLock.current = false; if (alive.current) setMoreLoading(false); }
  }

  function changeView(next: View) {
    setView(next); setReview(null); setDetailId(null); setReversal(null);
    commands.clearError();
  }
  async function handleLogout() {
    if (loggingOut || commands.busy) return;
    setLoggingOut(true); setError("");
    try { await logout(); }
    catch (cause) { if (!(cause instanceof StaleIdentity)) setError(cause instanceof Error ? cause.message : "Sesi belum dapat ditutup."); }
    finally { if (alive.current) setLoggingOut(false); }
  }
  function submitReview() {
    if (!review || blocked) return;
    const command = makeCommand(client.currentOwner(), review.path, review.body, review.title);
    setReview(null); void commands.run(command);
  }
  function createPerson(body: PersonBody) {
    if (!blocked) void commands.run(makeCommand(client.currentOwner(), "/people", body, "Buat akun Member"));
  }
  function submitReversal(reason: string) {
    if (!reversal || blocked) return;
    const command = makeCommand(client.currentOwner(), `/transactions/${reversal.id}/reverse`, { reason }, `Pembalikan transfer #${reversal.seq}`);
    setReversal(null); void commands.run(command);
  }

  const ownWallet = data?.wallets.find((wallet) => wallet.user_id === auth.user.id);
  const visibleTransactions = filterTransactions(data?.transactions.items ?? [], search, kind);
  const nav = admin ? [...memberNavigation, ...adminNavigation] : memberNavigation;

  return <div className="app-shell">
    <a className="skip-link" href="#workspace-main">Lewati ke isi workspace</a>
    <header className="workspace-header"><div><span className="wordmark">WalletLedger<span className="wordmark-mark"> /</span></span><p>{auth.workspace.name}</p></div><div className="identity-block"><span><strong>{auth.user.full_name}</strong><small>{admin ? "Admin workspace" : "Member"}</small></span><button type="button" className="header-button" onClick={() => void handleLogout()} disabled={loggingOut || commands.busy}>{loggingOut ? "Menutup sesi…" : "Keluar"}</button></div></header>
    <div className="workspace-layout"><aside className="workspace-rail"><nav aria-label="Menu workspace">{nav.map(([key, label], index) => <button type="button" key={key} className={`nav-item ${view === key ? "selected" : ""} ${admin && index === 3 ? "admin-start" : ""}`} aria-current={view === key ? "page" : undefined} onClick={() => changeView(key)}><span className="nav-number" aria-hidden="true">{String(index + 1).padStart(2, "0")}</span>{label}</button>)}</nav><p className="rail-note">Saldo sintetis<br />Rupiah bulat · IDR</p></aside>
      <main id="workspace-main" className="workspace-main" tabIndex={-1}>
        <div className="workspace-toolbar"><p>{online ? "Terhubung" : "Offline"}{updated ? ` · Data dimuat ${dateTime(updated)}` : " · Menunggu data"}</p><button type="button" className="quiet" onClick={() => void refresh()} disabled={loading || !online}>{loading ? "Memuat data…" : "Perbarui data"}</button></div>
        {!online && <Notice tone="error">Koneksi offline. Pengiriman saldo dinonaktifkan. Data yang terlihat berasal dari pembacaan terakhir.</Notice>}
        {commands.pending && <Notice tone={commands.busy ? "neutral" : "error"}><strong>{commands.busy ? "Memproses permintaan" : "Hasil permintaan belum diketahui"}</strong><p>{commands.pending.title}. {commands.pending.persistent ? "Kunci dan isi permintaan disimpan untuk sesi akun ini. Jangan membuat permintaan baru untuk tindakan yang sama." : "Permintaan akun berisi kata sandi dan hanya disimpan dalam memori tab ini. Jangan tutup tab sebelum hasilnya diketahui."}</p><p className="identifier">Kunci: {commands.pending.key}</p><button type="button" onClick={() => void commands.run(commands.pending!)} disabled={commands.busy || !online}>{commands.busy ? "Menunggu hasil…" : "Ulangi permintaan yang sama"}</button></Notice>}
        {commands.error && <Notice tone="error">{commands.error}</Notice>}
        {error && <Notice tone="error">{error}{data && <p>Data sebelumnya tetap terlihat. Perbarui data untuk mengambil saldo terkini.</p>}</Notice>}
        {notice && <Notice tone="success">{notice}<button className="text-button" type="button" onClick={() => setNotice("")}>Sembunyikan pemberitahuan</button></Notice>}
        {!data && !error && <p className="loading-state" role="status">Memuat dompet, penerima dan riwayat transaksi…</p>}
        {!data && error && <button type="button" onClick={() => void refresh()} disabled={loading}>Muat ulang workspace</button>}
        {data && <>
          {view === "summary" && <>
            <PageHeading title="Register dompet" description="Saldo dan riwayat pencatatan dalam workspace latihan." />
            <section className="balance-document document"><div><p className="section-label">Dompet {auth.user.full_name}</p><h2 className="balance-amount money">{ownWallet ? rupiah(ownWallet.balance_minor) : "Dompet belum tersedia"}</h2><p>Saldo simulasi · IDR</p></div><div className="balance-actions"><button type="button" className="primary" onClick={() => changeView("transfer")} disabled={blocked}>Kirim saldo</button>{admin && <button type="button" onClick={() => changeView("allocation")} disabled={blocked}>Alokasi saldo</button>}</div></section>
            <section className="recent-register"><div className="section-heading"><div><h2>Transaksi terbaru</h2><p>{admin ? "Seluruh dompet dalam workspace" : "Transaksi yang melibatkan dompetmu"}</p></div><button type="button" className="quiet" onClick={() => changeView("history")}>Buka riwayat</button></div><TransactionList items={data.transactions.items.slice(0, 5)} walletId={ownWallet?.id} open={setDetailId} compact /></section>
            <div className="register-footnote"><span>Pencatatan berpasangan</span><p>Setiap transaksi menghasilkan satu debit dan satu kredit. Buka bukti untuk melihat jurnalnya.</p></div>
          </>}
          {(view === "transfer" || view === "allocation") && <TransferForm key={`${view}:${formEpoch}`} people={data.people} actorId={auth.user.id} allocation={view === "allocation"} blocked={blocked} review={setReview} />}
          {view === "history" && <><PageHeading title="Riwayat transaksi" description={admin ? "Daftar transaksi seluruh dompet. Nomor register dapat memiliki jeda." : "Hanya transaksi yang melibatkan dompetmu. Nomor register dapat memiliki jeda."} /><HistoryFilters search={search} kind={kind} setSearch={setSearch} setKind={setKind} /><p className="muted filter-scope">Filter hanya berlaku pada {data.transactions.items.length} baris yang sudah dimuat. Perbarui data untuk memulai lagi dari transaksi terbaru.</p>{visibleTransactions.length ? <TransactionList items={visibleTransactions} walletId={ownWallet?.id} open={setDetailId} /> : <Empty>{data.transactions.items.length ? "Tidak ada baris yang cocok dengan filter. Ubah pencarian atau jenis transaksi." : "Belum ada transaksi. Catatan akan muncul setelah alokasi atau transfer selesai."}</Empty>}{data.transactions.next_before != null && <button type="button" onClick={() => void loadMore()} disabled={moreLoading}>{moreLoading ? "Memuat transaksi…" : "Muat transaksi lebih lama"}</button>}</>}
          {admin && view === "accounts" && <Accounts client={client} refresh={refreshToken} blocked={blocked} submit={createPerson} />}
          {admin && view === "reconcile" && <Reconcile client={client} refresh={refreshToken} />}
          {admin && view === "audit" && <Audit client={client} refresh={refreshToken} />}
        </>}
        <footer className="workspace-footer">WalletLedger · Aplikasi demonstrasi lokal. Seluruh saldo dan alokasi bersifat sintetis.</footer>
      </main>
    </div>
    {review && <ReviewDialog value={review} busy={commands.busy} blocked={blocked} close={() => setReview(null)} confirm={submitReview} />}
    {detailId && <TransactionDialog key={detailId} id={detailId} client={client} admin={admin} blocked={blocked} close={() => setDetailId(null)} openReference={setDetailId} reverse={(transaction) => { setDetailId(null); setReversal(transaction); }} />}
    {reversal && <ReversalDialog transaction={reversal} blocked={blocked} close={() => setReversal(null)} confirm={submitReversal} />}
  </div>;
}
