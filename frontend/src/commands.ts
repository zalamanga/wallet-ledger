import { sameOwner } from "./identity";
import type { Command, CommandBody, Owner } from "./types";

export function pendingKey(owner: Owner): string {
  return `walletledger.pending.v1:${owner.workspace_id}:${owner.user_id}`;
}

export function makeCommand(owner: Owner, path: string, body: CommandBody, title: string): Command {
  return Object.freeze({
    owner: Object.freeze({ ...owner }), key: crypto.randomUUID(), method: "POST" as const,
    path, body: JSON.stringify(body), title,
    persistent: path !== "/people",
  });
}

export function savePending(command: Command, storage: Pick<Storage, "setItem"> = sessionStorage): void {
  if (!command.persistent) return;
  storage.setItem(pendingKey(command.owner), JSON.stringify(command));
}

export function removePending(owner: Owner, storage: Pick<Storage, "removeItem"> = sessionStorage): void {
  try { storage.removeItem(pendingKey(owner)); }
  catch { /* A resolved immutable key can safely replay if an older stored copy remains. */ }
}

export function readPending(owner: Owner, storage: Pick<Storage, "getItem"> = sessionStorage): Command | null {
  try {
    const stored: unknown = JSON.parse(storage.getItem(pendingKey(owner)) ?? "null");
    if (!stored || typeof stored !== "object") return null;
    const item = stored as Partial<Command>;
    if (!item.owner || !sameOwner(item.owner, owner) || item.method !== "POST" || item.persistent !== true || typeof item.key !== "string" || !/^[0-9a-f-]{36}$/i.test(item.key) || typeof item.title !== "string" || typeof item.body !== "string") return null;
    if (item.path !== "/transfers" && item.path !== "/allocations" && !/^\/transactions\/[0-9a-f-]{36}\/reverse$/i.test(item.path ?? "")) return null;
    const body: unknown = JSON.parse(item.body);
    if (!body || typeof body !== "object" || Array.isArray(body)) return null;
    const fields = Object.keys(body);
    const allowed = item.path?.endsWith("/reverse") ? ["reason"] : ["recipient_id", "amount_minor", "note"];
    if (fields.length !== allowed.length || fields.some((key) => !allowed.includes(key))) return null;
    if (Object.values(body).some((value) => typeof value !== "string")) return null;
    return Object.freeze({ ...item, owner: Object.freeze({ ...owner }) }) as Command;
  } catch { return null; }
}
