import { useCallback, useEffect, useRef, useState } from "react";
import { ApiClient, ApiError, publicRequest } from "./api";
import { Auth } from "./Auth";
import { Notice } from "./components";
import { authOwner, IDENTITY_STORAGE_KEY, IdentityFence, StaleIdentity, writeIdentityPointer } from "./identity";
import type { AuthView } from "./types";
import { Workspace } from "./Workspace";

interface Session { auth: AuthView; client: ApiClient; generation: number }
interface AuthStatus { setup_required: boolean; workspace_name: string | null }

export default function App() {
  const fence = useRef(new IdentityFence()).current;
  const [session, setSession] = useState<Session | null>(null);
  const [status, setStatus] = useState<AuthStatus | null>(null);
  const [checking, setChecking] = useState(true);
  const [error, setError] = useState("");
  const revalidateRef = useRef<() => void>(() => {});

  const install = useCallback((auth: AuthView, generation: number) => {
    fence.assert(generation);
    const client = new ApiClient(auth, fence, generation, () => revalidateRef.current());
    setSession({ auth, client, generation });
    setError(""); setChecking(false);
    writeIdentityPointer(authOwner(auth));
  }, [fence]);

  const revalidate = useCallback(async () => {
    const generation = fence.advance();
    setSession(null); setChecking(true); setError("");
    try {
      const auth = await publicRequest<AuthView>("/auth/me", fence, generation);
      install(auth, generation);
    } catch (cause) {
      if (cause instanceof StaleIdentity) return;
      try {
        fence.assert(generation);
        if (!(cause instanceof ApiError && cause.status === 401)) throw cause;
        const next = await publicRequest<AuthStatus>("/auth/status", fence, generation);
        setStatus(next); writeIdentityPointer(null); setChecking(false);
      } catch (failure) {
        if (failure instanceof StaleIdentity) return;
        setError(failure instanceof Error ? failure.message : "Server belum dapat dihubungi."); setChecking(false);
      }
    }
  }, [fence, install]);
  revalidateRef.current = () => { void revalidate(); };

  useEffect(() => { void revalidate(); return () => { fence.advance(); }; }, [fence, revalidate]);
  useEffect(() => {
    const handle = (event: StorageEvent) => { if (event.key === IDENTITY_STORAGE_KEY) void revalidate(); };
    window.addEventListener("storage", handle);
    return () => window.removeEventListener("storage", handle);
  }, [revalidate]);

  async function authenticate(setup: boolean, body: Record<string, string>) {
    const generation = fence.advance();
    const csrf = await publicRequest<{ csrf_token: string }>("/auth/csrf", fence, generation);
    const auth = await publicRequest<AuthView>(setup ? "/auth/setup" : "/auth/login", fence, generation, {
      method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf.csrf_token }, body: JSON.stringify(body),
    });
    install(auth, generation);
    return auth;
  }

  async function logout() {
    if (!session) return;
    await session.client.logout();
    fence.advance(); setSession(null); writeIdentityPointer(null);
    await revalidate();
  }

  if (checking) return <main className="connection-page"><span className="wordmark">WalletLedger /</span><p role="status">Memeriksa sesi workspace…</p></main>;
  if (error) return <main className="connection-page"><span className="wordmark">WalletLedger /</span><Notice tone="error">{error}</Notice><button type="button" onClick={() => void revalidate()}>Hubungkan kembali</button></main>;
  if (session) return <Workspace key={`${session.auth.workspace.id}:${session.auth.user.id}:${session.generation}`} auth={session.auth} client={session.client} logout={logout} />;
  if (status) return <Auth setup={status.setup_required} workspaceName={status.workspace_name} submit={authenticate} />;
  return null;
}
