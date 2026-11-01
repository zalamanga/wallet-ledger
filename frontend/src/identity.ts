import type { AuthView, Owner } from "./types";

export class StaleIdentity extends Error {
  constructor() { super("Sesi berubah. Respons sebelumnya diabaikan."); }
}

export class IdentityFence {
  private generation = 0;
  advance(): number { this.generation += 1; return this.generation; }
  capture(): number { return this.generation; }
  assert(generation: number): void {
    if (generation !== this.generation) throw new StaleIdentity();
  }
}

export function authOwner(auth: AuthView): Owner {
  return { user_id: auth.user.id, workspace_id: auth.workspace.id };
}
export function sameOwner(a: Owner | null, b: Owner | null): boolean {
  return a?.user_id === b?.user_id && a?.workspace_id === b?.workspace_id;
}
export const IDENTITY_STORAGE_KEY = "walletledger.identity.v1";

export function writeIdentityPointer(owner: Owner | null): void {
  try { localStorage.setItem(IDENTITY_STORAGE_KEY, JSON.stringify(owner)); }
  catch { /* The pointer is advisory; the server still validates every request. */ }
}
