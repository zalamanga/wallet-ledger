import { authOwner, IdentityFence, sameOwner, StaleIdentity } from "./identity";
import type { AuthView, Command, MutationResult } from "./types";

export class ApiError extends Error {
  constructor(public status: number, public code: string, detail: string, public replayed = false) {
    super(detail);
  }
}
export class UnknownOutcome extends Error {
  constructor() { super("Server belum mengonfirmasi hasil permintaan. Periksa koneksi, lalu ulangi permintaan yang sama."); }
}

async function decode<T>(response: Response): Promise<T> {
  let body: unknown;
  try { body = response.status === 204 ? null : await response.json(); }
  catch { throw new ApiError(response.status, "INVALID_RESPONSE", "Respons server tidak dapat dibaca. Perbarui data untuk mencoba lagi."); }
  if (!response.ok) {
    const error = body as { code?: string; detail?: string } | null;
    throw new ApiError(response.status, error?.code ?? "REQUEST_FAILED", error?.detail ?? "Permintaan belum berhasil.", response.headers.get("Idempotency-Replayed") === "true");
  }
  return body as T;
}

export async function publicRequest<T>(path: string, fence: IdentityFence, generation: number, options?: RequestInit): Promise<T> {
  fence.assert(generation);
  const response = await fetch(`/api${path}`, { credentials: "same-origin", cache: "no-store", ...options });
  fence.assert(generation);
  let result: T;
  try { result = await decode<T>(response); }
  catch (error) { fence.assert(generation); throw error; }
  fence.assert(generation);
  return result;
}

export class ApiClient {
  private auth: AuthView;
  constructor(auth: AuthView, private fence: IdentityFence, private generation: number, private invalidate: () => void) {
    this.auth = auth;
  }
  currentOwner() { return authOwner(this.auth); }
  assertCurrent() { this.fence.assert(this.generation); }
  private async request<T>(path: string, options?: RequestInit): Promise<T> {
    this.assertCurrent();
    try {
      return await publicRequest<T>(path, this.fence, this.generation, options);
    } catch (error) {
      this.assertCurrent();
      if (error instanceof ApiError && (error.status === 401 || error.code === "IDENTITY_CHANGED")) {
        this.invalidate();
        throw new StaleIdentity();
      }
      throw error;
    }
  }
  read<T>(path: string): Promise<T> { return this.request<T>(path); }

  async command(command: Command): Promise<MutationResult> {
    this.assertCurrent();
    if (!sameOwner(command.owner, this.currentOwner())) throw new StaleIdentity();
    for (let attempt = 0; attempt < 2; attempt += 1) {
      try {
        return await this.request<MutationResult>(command.path, {
          method: command.method,
          headers: {
            "Content-Type": "application/json",
            "X-CSRF-Token": this.auth.csrf_token,
            "X-Wallet-Actor": command.owner.user_id,
            "X-Wallet-Workspace": command.owner.workspace_id,
            "Idempotency-Key": command.key,
          },
          body: command.body,
        });
      } catch (error) {
        this.assertCurrent();
        if (error instanceof ApiError && error.code === "CSRF_INVALID" && attempt === 0) {
          const fresh = await this.read<AuthView>("/auth/me");
          if (!sameOwner(authOwner(fresh), command.owner)) {
            this.invalidate();
            throw new StaleIdentity();
          }
          this.auth = fresh;
          continue;
        }
        if (error instanceof ApiError && error.code !== "INVALID_RESPONSE" && error.status < 500 && ![408, 425].includes(error.status)) throw error;
        if (error instanceof StaleIdentity) throw error;
        throw new UnknownOutcome();
      }
    }
    throw new UnknownOutcome();
  }

  async logout(): Promise<void> {
    for (let attempt = 0; attempt < 2; attempt += 1) {
      try {
        await this.request("/auth/logout", {
          method: "POST", body: "{}",
          headers: {
            "Content-Type": "application/json", "X-CSRF-Token": this.auth.csrf_token,
            "X-Wallet-Actor": this.auth.user.id, "X-Wallet-Workspace": this.auth.workspace.id,
          },
        });
        return;
      } catch (error) {
        if (!(error instanceof ApiError && error.code === "CSRF_INVALID" && attempt === 0)) throw error;
        const auth = await this.read<AuthView>("/auth/me");
        if (!sameOwner(authOwner(auth), this.currentOwner())) { this.invalidate(); throw new StaleIdentity(); }
        this.auth = auth;
      }
    }
  }
}
