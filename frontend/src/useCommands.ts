import { useRef, useState } from "react";
import { ApiClient, ApiError, UnknownOutcome } from "./api";
import { readPending, removePending, savePending } from "./commands";
import { StaleIdentity } from "./identity";
import type { Command, MutationResult } from "./types";

export function useCommands(client: ApiClient, onSuccess: (result: MutationResult) => void) {
  const [pending, setPending] = useState<Command | null>(() => readPending(client.currentOwner()));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const lock = useRef(false);
  const active = useRef(pending);

  async function run(command: Command) {
    if (lock.current || (active.current && active.current.key !== command.key)) return;
    client.assertCurrent();
    if (!navigator.onLine) { setError("Koneksi offline. Sambungkan internet sebelum mengirim permintaan."); return; }
    lock.current = true;
    setBusy(true);
    setError("");
    try {
      if (command.persistent) {
        try { savePending(command); }
        catch { setError("Browser tidak dapat menyimpan kunci permintaan. Izinkan penyimpanan sesi sebelum mengirim saldo."); return; }
      }
      active.current = command;
      setPending(command);
      const result = await client.command(command);
      client.assertCurrent();
      if (command.persistent) removePending(command.owner);
      active.current = null;
      setPending(null);
      onSuccess(result);
    } catch (cause) {
      if (cause instanceof StaleIdentity) return;
      if (cause instanceof UnknownOutcome) {
        setError(cause.message);
      } else {
        if (command.persistent) removePending(command.owner);
        active.current = null;
        setPending(null);
        setError(cause instanceof ApiError || cause instanceof Error ? cause.message : "Permintaan belum berhasil.");
      }
    } finally {
      lock.current = false;
      setBusy(false);
    }
  }
  return { pending, busy, error, run, clearError: () => setError("") };
}
