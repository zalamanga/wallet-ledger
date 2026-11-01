import { useEffect, useState } from "react";
import type { ApiClient } from "./api";
import { Dialog, Empty, Field, Notice } from "./components";
import { StaleIdentity } from "./identity";
import { amountPerspective, dateTime, kindLabel, rupiah } from "./money";
import type { Transaction, TransactionDetail } from "./types";

export function TransactionList({ items, walletId, open, compact = false }: {
  items: Transaction[]; walletId?: string; open: (id: string) => void; compact?: boolean;
}) {
  if (!items.length) return <Empty>Belum ada transaksi pada daftar ini. Alokasi atau transfer yang selesai akan tercatat di sini.</Empty>;
  return <ol className={`transaction-list ${compact ? "compact" : ""}`}>
    {items.map((item) => {
      const direction = item.from_wallet_id === walletId ? "Keluar" : item.to_wallet_id === walletId ? "Masuk" : "Antardompet";
      return <li key={item.id} className="transaction-row">
        <div className="register-number"><span>#{item.seq}</span><small>{kindLabel[item.kind]}</small></div>
        <div className="transaction-description"><strong>{item.from_name} <span className="muted">ke</span> {item.to_name}</strong><p>{item.note}</p><small>{dateTime(item.created_at)}{item.reversal_id ? " · Sudah dibalik" : item.reversal_of ? " · Merujuk transfer asli" : ""}</small></div>
        <div className="transaction-amount"><strong className="money">{amountPerspective(item, walletId)}</strong><small>{direction}</small></div>
        <button type="button" className="quiet" aria-label={`Buka transaksi ${item.seq}`} onClick={() => open(item.id)}>Lihat bukti</button>
      </li>;
    })}
  </ol>;
}

export function HistoryFilters({ search, kind, setSearch, setKind }: {
  search: string; kind: string; setSearch: (value: string) => void; setKind: (value: string) => void;
}) {
  return <div className="history-filters"><Field label="Cari pada baris dimuat"><input type="search" value={search} onChange={(event) => setSearch(event.target.value)} /></Field>
    <Field label="Jenis pada baris dimuat"><select value={kind} onChange={(event) => setKind(event.target.value)}><option value="">Semua jenis</option><option value="TRANSFER">Transfer</option><option value="ALLOCATION">Alokasi latihan</option><option value="REVERSAL">Pembalikan</option></select></Field>
  </div>;
}

export function filterTransactions(items: Transaction[], search: string, kind: string) {
  const needle = search.trim().toLocaleLowerCase("id-ID");
  return items.filter((item) => (!kind || item.kind === kind) && (!needle || [item.from_name, item.to_name, item.note, String(item.seq)].some((value) => value.toLocaleLowerCase("id-ID").includes(needle))));
}

export function TransactionDialog({ id, client, admin, close, reverse, openReference, blocked }: {
  id: string; client: ApiClient; admin: boolean; close: () => void;
  reverse: (transaction: Transaction) => void; openReference: (id: string) => void; blocked: boolean;
}) {
  const [detail, setDetail] = useState<TransactionDetail | null>(null);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    let alive = true;
    setDetail(null); setError("");
    client.read<TransactionDetail>(`/transactions/${id}`).then((result) => { if (alive) setDetail(result); }).catch((cause) => {
      if (alive && !(cause instanceof StaleIdentity)) setError(cause instanceof Error ? cause.message : "Bukti belum dapat dimuat.");
    });
    return () => { alive = false; };
  }, [client, id, retry]);
  return <Dialog title={detail ? `Bukti transaksi #${detail.transaction.seq}` : "Bukti transaksi"} onClose={close} wide>
    {error && <><Notice tone="error">{error}</Notice><button type="button" onClick={() => setRetry((value) => value + 1)}>Muat ulang bukti</button></>}
    {!detail && !error && <p role="status">Memuat jurnal transaksi…</p>}
    {detail && <>
      <div className="receipt-top"><div><p className="section-label">{kindLabel[detail.transaction.kind]}</p><p className="receipt-amount money">{rupiah(detail.transaction.amount_minor)}</p></div><p className="receipt-seal">Tercatat<br /><small>{dateTime(detail.transaction.created_at)}</small></p></div>
      <dl className="receipt-facts"><div><dt>Dari</dt><dd>{detail.transaction.from_name}</dd></div><div><dt>Ke</dt><dd>{detail.transaction.to_name}</dd></div><div><dt>Catatan</dt><dd>{detail.transaction.note}</dd></div><div><dt>Dicatat oleh</dt><dd>{detail.transaction.created_by_name}</dd></div><div className="full"><dt>ID transaksi</dt><dd className="identifier">{detail.transaction.id}</dd></div></dl>
      <h3>Jurnal dua sisi</h3>
      <div className="table-region" role="region" aria-label="Jurnal debit dan kredit" tabIndex={0}><table className="journal-table"><thead><tr><th>Dompet</th><th>Sisi</th><th className="numeric">Jumlah</th></tr></thead><tbody>{detail.entries.map((entry) => <tr key={`${entry.wallet_id}:${entry.direction}`}><th scope="row">{entry.wallet_name}</th><td>{entry.direction}</td><td className="numeric money">{rupiah(entry.amount_minor)}</td></tr>)}</tbody></table></div>
      <div className="journal-totals"><p>Debit <strong className="money">{rupiah(detail.debit_total_minor)}</strong></p><p>Kredit <strong className="money">{rupiah(detail.credit_total_minor)}</strong></p><strong>{detail.debit_total_minor === detail.credit_total_minor ? "Debit dan kredit seimbang" : "Perlu pemeriksaan jurnal"}</strong></div>
      <p className="muted">Bukti ini adalah catatan transaksi. Saldo terkini dimuat kembali dari dompet, bukan dari bukti lama.</p>
      {detail.transaction.reversal_id && <Notice>Transfer ini sudah dibalik. <button className="text-button" type="button" onClick={() => openReference(detail.transaction.reversal_id!)}>Lihat transaksi pembalikan</button></Notice>}
      {detail.transaction.reversal_of && <p>Transfer asal: <button className="text-button" type="button" onClick={() => openReference(detail.transaction.reversal_of!)}>Buka bukti transfer asal</button></p>}
      {admin && detail.transaction.kind === "TRANSFER" && !detail.transaction.reversal_id && <div className="receipt-actions"><p>Pembalikan mengembalikan saldo penerima kepada pengirim melalui transaksi baru.</p><button type="button" disabled={blocked} onClick={() => reverse(detail.transaction)}>Tinjau pembalikan</button></div>}
    </>}
  </Dialog>;
}
