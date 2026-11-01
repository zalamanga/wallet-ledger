import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError, publicRequest, UnknownOutcome } from "./api";
import { makeCommand } from "./commands";
import { authOwner, IdentityFence, StaleIdentity } from "./identity";
import type { AuthView, MutationResult } from "./types";

const auth: AuthView = { user: { id: "11111111-1111-4111-8111-111111111111", email: "synthetic@example.test", full_name: "Akun latihan", role: "Member" }, workspace: { id: "22222222-2222-4222-8222-222222222222", name: "Latihan" }, csrf_token: "original-csrf" };
const accepted: MutationResult = { request_id: "key", replayed: true, transaction: null, user: null };
function json(value: unknown, status = 200, headers?: Record<string, string>) { return new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json", ...headers } }); }
function create() {
  const fence = new IdentityFence();
  const invalidate = vi.fn(() => { fence.advance(); });
  const client = new ApiClient(auth, fence, fence.capture(), invalidate);
  const command = makeCommand(authOwner(auth), "/transfers", { recipient_id: "33333333-3333-4333-8333-333333333333", amount_minor: "1", note: "Latihan" }, "Transfer saldo");
  return { client, fence, invalidate, command };
}
afterEach(() => { vi.unstubAllGlobals(); });

describe("authenticated transport", () => {
  it("retries CSRF once with the identical immutable key, body and expected owner", async () => {
    const mock = vi.fn().mockResolvedValueOnce(json({ detail: "CSRF stale", code: "CSRF_INVALID" }, 403)).mockResolvedValueOnce(json({ ...auth, csrf_token: "fresh-csrf" })).mockResolvedValueOnce(json(accepted, 201));
    vi.stubGlobal("fetch", mock);
    const { client, command } = create();
    expect(await client.command(command)).toEqual(accepted);
    const first = mock.mock.calls[0][1] as RequestInit;
    const second = mock.mock.calls[2][1] as RequestInit;
    expect(first.body).toBe(second.body);
    expect(first.body).toBe(command.body);
    expect(first.headers).toMatchObject({ "Idempotency-Key": command.key, "X-Wallet-Actor": auth.user.id, "X-Wallet-Workspace": auth.workspace.id, "X-CSRF-Token": "original-csrf" });
    expect(second.headers).toMatchObject({ "Idempotency-Key": command.key, "X-Wallet-Actor": auth.user.id, "X-CSRF-Token": "fresh-csrf" });
    expect(mock.mock.calls[1][0]).toBe("/api/auth/me");
  });
  it("never retries a command after a refreshed session belongs to another actor", async () => {
    const mock = vi.fn().mockResolvedValueOnce(json({ detail: "CSRF stale", code: "CSRF_INVALID" }, 403)).mockResolvedValueOnce(json({ ...auth, user: { ...auth.user, id: "other" } }));
    vi.stubGlobal("fetch", mock);
    const { client, command, invalidate } = create();
    await expect(client.command(command)).rejects.toBeInstanceOf(StaleIdentity);
    expect(mock).toHaveBeenCalledTimes(2); expect(invalidate).toHaveBeenCalledTimes(1);
  });
  it("rejects expected-identity mismatch without an automatic replay", async () => {
    const mock = vi.fn().mockResolvedValue(json({ detail: "Cookie changed", code: "IDENTITY_CHANGED" }, 403));
    vi.stubGlobal("fetch", mock);
    const { client, command, invalidate } = create();
    await expect(client.command(command)).rejects.toBeInstanceOf(StaleIdentity);
    expect(mock).toHaveBeenCalledTimes(1); expect(invalidate).toHaveBeenCalledTimes(1);
  });
  it("does not send a command created for another workspace", async () => {
    const mock = vi.fn(); vi.stubGlobal("fetch", mock);
    const { client, command } = create();
    await expect(client.command({ ...command, owner: { ...command.owner, workspace_id: "other" } })).rejects.toBeInstanceOf(StaleIdentity);
    expect(mock).not.toHaveBeenCalled();
  });
  it("keeps 503 commit uncertainty distinct from a known business denial", async () => {
    const mock = vi.fn().mockResolvedValueOnce(json({ detail: "Commit unknown", code: "OUTCOME_UNKNOWN" }, 503)).mockResolvedValueOnce(json({ detail: "Saldo tidak cukup", code: "INSUFFICIENT_FUNDS" }, 409, { "Idempotency-Replayed": "true" }));
    vi.stubGlobal("fetch", mock);
    const { client, command } = create();
    await expect(client.command(command)).rejects.toBeInstanceOf(UnknownOutcome);
    await expect(client.command(command)).rejects.toMatchObject({ status: 409, code: "INSUFFICIENT_FUNDS", replayed: true });
  });
  it("treats a network failure and an unreadable success response as unknown outcomes", async () => {
    const mock = vi.fn().mockRejectedValueOnce(new TypeError("Network disconnected")).mockResolvedValueOnce(new Response("not JSON", { status: 201 }));
    vi.stubGlobal("fetch", mock);
    const { client, command } = create();
    await expect(client.command(command)).rejects.toBeInstanceOf(UnknownOutcome);
    await expect(client.command(command)).rejects.toBeInstanceOf(UnknownOutcome);
  });
  it("ignores a delayed fetch after its identity lease expires", async () => {
    let resolve!: (response: Response) => void;
    vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>((done) => { resolve = done; })));
    const { client, fence } = create();
    const pending = client.read("/wallets");
    fence.advance(); resolve(json({ items: [] }));
    await expect(pending).rejects.toBeInstanceOf(StaleIdentity);
  });
  it("ignores delayed JSON even when fetch itself completed before identity changed", async () => {
    let decode!: (value: unknown) => void;
    const started = vi.fn();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 200, headers: new Headers(), json: () => { started(); return new Promise((done) => { decode = done; }); } }));
    const { client, fence } = create();
    const pending = client.read("/wallets");
    await Promise.resolve(); await Promise.resolve();
    expect(started).toHaveBeenCalled();
    fence.advance(); decode({ items: [{ balance_minor: "999" }] });
    await expect(pending).rejects.toBeInstanceOf(StaleIdentity);
  });
  it("limits CSRF recovery to one retry", async () => {
    const mock = vi.fn().mockResolvedValueOnce(json({ detail: "CSRF stale", code: "CSRF_INVALID" }, 403)).mockResolvedValueOnce(json(auth)).mockResolvedValueOnce(json({ detail: "Still invalid", code: "CSRF_INVALID" }, 403));
    vi.stubGlobal("fetch", mock);
    const { client, command } = create();
    await expect(client.command(command)).rejects.toBeInstanceOf(ApiError);
    expect(mock).toHaveBeenCalledTimes(3);
  });
  it("fences delayed anonymous authentication responses too", async () => {
    let resolve!: (response: Response) => void;
    vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>((done) => { resolve = done; })));
    const fence = new IdentityFence();
    const pending = publicRequest("/auth/login", fence, fence.capture(), { method: "POST" });
    fence.advance(); resolve(json(auth));
    await expect(pending).rejects.toBeInstanceOf(StaleIdentity);
  });
});
