import { useEffect, useRef, useState } from "react";
import type { ApiClient } from "./api";
import { Empty, Notice, PageHeading } from "./components";
import { CreatePersonForm } from "./Forms";
import { StaleIdentity } from "./identity";
import { dateTime, rupiah } from "./money";
import type { AuditEvent, AuthUser, Page, PersonBody, Reconciliation } from "./types";

function useRemote<T>(client: ApiClient, path: string, refresh: number) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    let alive = true;
    setLoading(true); setError("");
    client.read<T>(path).then((value) => { if (alive) setData(value); }).catch((cause) => {
      if (alive && !(cause instanceof StaleIdentity)) setError(cause instanceof Error ? cause.message : "Data belum dapat dimuat.");
    }).finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
  }, [client, path, refresh, retry]);
  return { data, error, loading, retry: () => setRetry((value) => value + 1) };
}

export function Accounts({ client, refresh, blocked, submit }: { client: ApiClient; refresh: number; blocked: boolean; submit: (body: PersonBody) => void }) {
  const state = useRemote<{ items: AuthUser[] }>(client, "/admin/people", refresh);
  return <><PageHeading title="Akun pengguna" description="Admin membuat akun Member. Setiap pengguna memiliki satu dompet IDR." />
    <div className="accounts-layout"><CreatePersonForm blocked={blocked} submit={submit} /><section className="account-register"><h2>Daftar akun</h2>
      {state.loading && <p role="status">Memuat akun workspace…</p>}
      {state.error && <><Notice tone="error">{state.error}</Notice><button type="button" onClick={state.retry}>Muat ulang akun</button></>}
      {state.data && <><p className="muted">{state.data.items.length} akun tercatat</p><ul className="people-list">{state.data.items.map((person) => <li key={person.id}><strong>{person.full_name}</strong><span>{person.email}</span><small>{person.role === "Admin" ? "Admin workspace" : "Member"}</small></li>)}</ul></>}
    </section></div></>;
}

export function Reconcile({ client, refresh }: { client: ApiClient; refresh: number }) {
  const state = useRemote<Reconciliation>(client, "/reconciliation", refresh);
  return <><PageHeading title="Rekonsiliasi" description="Bandingkan saldo tersimpan dengan jumlah jurnal dalam satu pembacaan database yang konsisten." />
    {state.loading && <p role="status">Menghitung saldo dan jurnal…</p>}
    {state.error && <><Notice tone="error">{state.error}</Notice><button type="button" onClick={state.retry}>Hitung ulang rekonsiliasi</button></>}
    {state.data && <>
      <section className="reconciliation-summary document"><div><p className="section-label">Hasil pemeriksaan</p><h2>{state.data.balanced ? "Seluruh jurnal seimbang" : "Ada selisih yang perlu diperiksa"}</h2><p>{state.data.wallet_count} dompet · {state.data.transaction_count} transaksi tercatat</p></div>
        <dl><div><dt>Total debit</dt><dd className="money">{rupiah(state.data.total_debit_minor)}</dd></div><div><dt>Total kredit</dt><dd className="money">{rupiah(state.data.total_credit_minor)}</dd></div><div><dt>Total saldo seluruh dompet</dt><dd className="money">{rupiah(state.data.total_wallet_balance_minor)}</dd></div></dl>
      </section>
      <div className="table-region reconciliation-table" role="region" aria-label="Perbandingan saldo setiap dompet" tabIndex={0}><table><thead><tr><th>Dompet</th><th className="numeric">Saldo tersimpan</th><th className="numeric">Hasil jurnal</th><th className="numeric">Selisih</th></tr></thead><tbody>{state.data.items.map((item) => <tr key={item.wallet_id}><th scope="row">{item.owner_name}{item.is_system && <small>Dompet pendanaan sintetis</small>}</th><td className="numeric money">{rupiah(item.cached_balance_minor)}</td><td className="numeric money">{rupiah(item.ledger_balance_minor)}</td><td className="numeric money">{rupiah(item.difference_minor)}</td></tr>)}</tbody></table></div>
      <p className="muted">Saldo dompet pendanaan bernilai negatif untuk mengimbangi alokasi sintetis. Total seluruh dompet tetap nol.</p>
    </>}
  </>;
}

const auditLabels: Record<string, string> = {
  USER_CREATED: "Akun dibuat", PERSON_CREATED: "Akun dibuat", TRANSFER: "Transfer dicatat", TRANSFER_CREATED: "Transfer dicatat",
  ALLOCATION: "Alokasi dicatat", ALLOCATION_CREATED: "Alokasi dicatat", REVERSAL: "Pembalikan dicatat", REVERSAL_CREATED: "Pembalikan dicatat",
};

export function Audit({ client, refresh }: { client: ApiClient; refresh: number }) {
  const [page, setPage] = useState<Page<AuditEvent> | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const generation = useRef(0);
  const busy = useRef(false);
  async function load(before?: number) {
    if (busy.current && before !== undefined) return;
    const current = before === undefined ? ++generation.current : generation.current;
    busy.current = true; setLoading(true); setError("");
    try {
      const result = await client.read<Page<AuditEvent>>(`/audit?limit=50${before === undefined ? "" : `&before=${before}`}`);
      if (current !== generation.current) return;
      setPage((old) => before === undefined ? result : { ...result, items: [...(old?.items ?? []), ...result.items].filter((item, index, all) => all.findIndex((row) => row.id === item.id) === index) });
    } catch (cause) { if (current === generation.current && !(cause instanceof StaleIdentity)) setError(cause instanceof Error ? cause.message : "Audit belum dapat dimuat."); }
    finally { if (current === generation.current) { busy.current = false; setLoading(false); } }
  }
  useEffect(() => { void load(); return () => { generation.current += 1; }; }, [client, refresh]);
  return <><PageHeading title="Audit" description="Peristiwa pencatatan akun dan transaksi dari server. Replay permintaan tidak menambah peristiwa baru." />
    {error && <><Notice tone="error">{error}</Notice><button type="button" onClick={() => void load()}>Muat ulang audit</button></>}
    {loading && <p role="status">Memuat peristiwa audit…</p>}
    {page && (page.items.length ? <ol className="audit-list">{page.items.map((event) => <li key={event.id}><span className="register-number">#{event.seq}</span><div><strong>{auditLabels[event.kind] ?? event.kind}</strong><p>{event.actor_name} · {dateTime(event.created_at)}</p><small className="identifier">ID objek: {event.entity_id}</small></div></li>)}</ol> : <Empty>Belum ada peristiwa audit. Pencatatan akun dan transaksi berikutnya akan muncul di sini.</Empty>)}
    {page?.next_before != null && <button type="button" onClick={() => void load(page.next_before!)} disabled={loading}>Muat audit lebih lama</button>}
  </>;
}
