import { describe, expect, it } from "vitest";
import { makeCommand, pendingKey, readPending, removePending, savePending } from "./commands";
import { IdentityFence, sameOwner, StaleIdentity } from "./identity";
import type { Owner } from "./types";

const owner: Owner = { user_id: "11111111-1111-4111-8111-111111111111", workspace_id: "22222222-2222-4222-8222-222222222222" };
function storage() {
  const values = new Map<string, string>();
  return { values, getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value); }, removeItem: (key: string) => { values.delete(key); } };
}
describe("immutable pending intent", () => {
  it("freezes both ownership and serialized body before submission", () => {
    const input = { recipient_id: "recipient", amount_minor: "1", note: "Latihan" };
    const copiedOwner = { ...owner };
    const command = makeCommand(copiedOwner, "/transfers", input, "Transfer saldo");
    input.amount_minor = "2"; copiedOwner.user_id = "other";
    expect(JSON.parse(command.body).amount_minor).toBe("1");
    expect(command.owner).toEqual(owner);
    expect(Object.isFrozen(command)).toBe(true);
    expect(Object.isFrozen(command.owner)).toBe(true);
  });
  it("restores the original key and body only for its exact workspace and actor", () => {
    const store = storage();
    const command = makeCommand(owner, "/allocations", { recipient_id: "recipient", amount_minor: "12", note: "Latihan" }, "Alokasi saldo");
    savePending(command, store);
    expect(readPending(owner, store)).toEqual(command);
    expect(readPending({ ...owner, user_id: "other" }, store)).toBeNull();
    expect(readPending({ ...owner, workspace_id: "other" }, store)).toBeNull();
  });
  it("never serializes account passwords into browser storage", () => {
    const store = storage();
    const command = makeCommand(owner, "/people", { full_name: "Akun latihan", email: "synthetic@example.test", password: "SyntheticOnlyPassword947!" }, "Buat akun");
    expect(command.persistent).toBe(false);
    savePending(command, store);
    expect(store.values.size).toBe(0);
  });
  it("rejects malformed or credential-bearing restored bodies", () => {
    const store = storage();
    const command = makeCommand(owner, "/transfers", { recipient_id: "recipient", amount_minor: "12", note: "Latihan" }, "Transfer saldo");
    store.setItem(pendingKey(owner), JSON.stringify({ ...command, body: '{"recipient_id":"recipient","amount_minor":"12","note":"Latihan","password":"secret"}' }));
    expect(readPending(owner, store)).toBeNull();
    store.setItem(pendingKey(owner), "{");
    expect(readPending(owner, store)).toBeNull();
  });
  it("rejects a stored owner substitution even in the current owner namespace", () => {
    const store = storage();
    const command = makeCommand(owner, "/transfers", { recipient_id: "recipient", amount_minor: "12", note: "Latihan" }, "Transfer saldo");
    store.setItem(pendingKey(owner), JSON.stringify({ ...command, owner: { ...owner, user_id: "other" } }));
    expect(readPending(owner, store)).toBeNull();
  });
  it("clears a resolved pending request", () => {
    const store = storage();
    savePending(makeCommand(owner, "/transfers", { recipient_id: "recipient", amount_minor: "12", note: "Latihan" }, "Transfer saldo"), store);
    removePending(owner, store);
    expect(readPending(owner, store)).toBeNull();
  });
});
describe("identity generation", () => {
  it("invalidates an older asynchronous lease immediately on identity revalidation", () => {
    const fence = new IdentityFence();
    const old = fence.capture(); fence.advance();
    expect(() => fence.assert(old)).toThrow(StaleIdentity);
    expect(() => fence.assert(fence.capture())).not.toThrow();
  });
  it("compares both identity dimensions and supports logged-out pointers", () => {
    expect(sameOwner(null, null)).toBe(true);
    expect(sameOwner(owner, null)).toBe(false);
    expect(sameOwner(owner, { ...owner, workspace_id: "different" })).toBe(false);
  });
});
