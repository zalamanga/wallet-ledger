import { expect, request, test as base, type APIRequestContext, type BrowserContext, type Page } from '@playwright/test';
import { randomUUID } from 'node:crypto';

export const BASE_URL = process.env.WALLETLEDGER_BASE_URL || 'http://127.0.0.1:8171';
const target = new URL(BASE_URL);
if (target.origin !== 'http://127.0.0.1:8171' || target.pathname !== '/' || target.search || target.hash) {
  throw new Error('Browser checks only mutate the isolated synthetic server at http://127.0.0.1:8171.');
}
export const PASSWORD = 'SyntheticWalletLedgerAccess947!';
export interface User { id: string; email: string; full_name: string; role: 'Admin' | 'Member' }
export interface Auth { user: User; workspace: { id: string; name: string }; csrf_token: string }
export interface Wallet { id: string; user_id: string | null; owner_name: string; is_system: boolean; balance_minor: string }
export interface Transaction { id: string; seq: number; kind: string; amount_minor: string; note: string; from_wallet_id: string; to_wallet_id: string; reversal_id: string | null; reversal_of: string | null }
export interface Mutation { request_id: string; replayed: boolean; user: User | null; transaction: { transaction: Transaction; entries: unknown[]; debit_total_minor: string; credit_total_minor: string } | null }
export interface Actor { context: BrowserContext; page: Page; auth: Auth }
let admin: APIRequestContext;
let adminAuth: Auth;

export function headers(auth: Auth, key = randomUUID()) {
  return { Origin: BASE_URL, 'X-CSRF-Token': auth.csrf_token, 'X-Wallet-Actor': auth.user.id,
    'X-Wallet-Workspace': auth.workspace.id, 'Idempotency-Key': key };
}
export async function login(api: APIRequestContext, email: string): Promise<Auth> {
  const csrf = await api.get('/api/auth/csrf');
  expect(csrf.ok(), await csrf.text()).toBeTruthy();
  const response = await api.post('/api/auth/login', { data: { email, password: PASSWORD },
    headers: { Origin: BASE_URL, 'X-CSRF-Token': (await csrf.json()).csrf_token } });
  expect(response.status(), await response.text()).toBe(200);
  return response.json();
}
export async function write(actor: Actor, path: string, body: object, key = randomUUID()): Promise<Mutation> {
  const response = await actor.context.request.post(path, { data: body, headers: headers(actor.auth, key) });
  expect(response.status(), await response.text()).toBe(201);
  return response.json();
}
export async function wallets(actor: Actor): Promise<Wallet[]> {
  const response = await actor.context.request.get('/api/wallets');
  expect(response.ok(), await response.text()).toBeTruthy();
  return (await response.json()).items;
}
export async function balance(actor: Actor): Promise<string> {
  const own = (await wallets(actor)).find((wallet) => wallet.user_id === actor.auth.user.id);
  if (!own) throw new Error('Own synthetic wallet not found.');
  return own.balance_minor;
}
export async function history(actor: Actor): Promise<Transaction[]> {
  const response = await actor.context.request.get('/api/transactions?limit=100');
  expect(response.ok(), await response.text()).toBeTruthy();
  return (await response.json()).items;
}
export async function fund(adminActor: Actor, recipient: Actor, amount: string) {
  return write(adminActor, '/api/allocations', { recipient_id: recipient.auth.user.id, amount_minor: amount, note: 'Synthetic browser balance allocation' });
}
export async function openApp(actor: Actor) {
  await actor.page.goto('/');
  await expect(actor.page.getByRole('button', { name: 'Keluar', exact: true })).toBeVisible();
  await expect(actor.page.getByRole('heading', { name: 'Register dompet', exact: true })).toBeVisible();
}
export async function transferForm(actor: Actor, recipient: Actor, amount: string, note: string) {
  await actor.page.getByRole('navigation', { name: 'Menu workspace' }).getByRole('button', { name: 'Kirim saldo', exact: true }).click();
  const recipientField = actor.page.getByLabel('Penerima', { exact: true });
  await expect(recipientField).toHaveAccessibleName('Penerima');
  await recipientField.selectOption(recipient.auth.user.id);
  await expect(recipientField).toHaveAccessibleName('Penerima');
  const amountField = actor.page.getByLabel(/^Jumlah \(IDR\)/);
  await expect(amountField).toHaveAccessibleName('Jumlah (IDR)');
  await expect(amountField).toHaveAccessibleDescription('Isi angka 1 sampai 1000000000 tanpa pemisah ribuan.');
  await amountField.fill(amount);
  await actor.page.getByLabel('Catatan', { exact: true }).fill(note);
  await actor.page.getByRole('button', { name: 'Tinjau transfer', exact: true }).click();
  const dialog = actor.page.getByRole('dialog', { name: 'Konfirmasi transfer', exact: true });
  await expect(dialog).toBeVisible();
  return dialog;
}
export async function commitTransfer(actor: Actor, recipient: Actor, amount: string, note: string) {
  const dialog = await transferForm(actor, recipient, amount, note);
  await dialog.getByRole('button', { name: 'Kirim transfer', exact: true }).click();
  return dialog;
}

type Team = { member(name?: string): Promise<Actor>; administrator(): Promise<Actor>; existing(email: string): Promise<Actor> };
export const test = base.extend<{ team: Team }>({
  team: async ({ browser }, use) => {
    const contexts: BrowserContext[] = [];
    const errors: string[] = [];
    async function contextFor(storageState?: Awaited<ReturnType<APIRequestContext['storageState']>>) {
      const context = await browser.newContext({ baseURL: BASE_URL, storageState, viewport: { width: 1440, height: 1000 }, locale: 'id-ID', timezoneId: 'Asia/Jakarta' });
      context.setDefaultTimeout(15_000);
      context.setDefaultNavigationTimeout(45_000);
      contexts.push(context);
      const page = await context.newPage();
      page.on('pageerror', (error) => errors.push(error.message));
      return { context, page };
    }
    async function existing(email: string): Promise<Actor> {
      const actor = await contextFor();
      return { ...actor, auth: await login(actor.context.request, email) };
    }
    async function administrator(): Promise<Actor> { return { ...(await contextFor(await admin.storageState())), auth: adminAuth }; }
    async function member(name = 'Browser Member'): Promise<Actor> {
      const email = `browser-${randomUUID()}@walletledger.test`;
      const response = await admin.post('/api/people', { data: { full_name: `${name} ${randomUUID().slice(0, 8)}`, email, password: PASSWORD }, headers: headers(adminAuth) });
      expect(response.status(), await response.text()).toBe(201);
      return existing(email);
    }
    try { await use({ member, administrator, existing }); }
    finally { await Promise.all(contexts.map((context) => context.close())); expect(errors, 'No uncaught UI exceptions').toEqual([]); }
  },
});
test.beforeAll(async ({ browser }) => {
  admin = await request.newContext({ baseURL: BASE_URL });
  const status = await admin.get('/api/auth/status');
  expect(status.ok()).toBeTruthy();
  if ((await status.json()).setup_required) {
    const context = await browser.newContext({ baseURL: BASE_URL, viewport: { width: 1440, height: 1000 }, locale: 'id-ID', timezoneId: 'Asia/Jakarta' });
    try {
      const page = await context.newPage();
      const errors: string[] = [];
      page.on('pageerror', (error) => errors.push(error.message));
      await page.goto('/');
      await expect(page.getByRole('heading', { name: 'Buat workspace latihan', exact: true })).toBeVisible();
      await page.getByLabel('Nama workspace', { exact: true }).fill('Synthetic browser ledger');
      await page.getByLabel('Nama lengkap', { exact: true }).fill('Synthetic Browser Administrator');
      await page.getByLabel('Email', { exact: true }).fill('browser-admin@walletledger.test');
      await page.getByLabel('Kata sandi', { exact: true }).fill(PASSWORD);
      const setupResponse = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/auth/setup' && response.request().method() === 'POST');
      await page.getByRole('button', { name: 'Buat workspace', exact: true }).click();
      const response = await setupResponse;
      expect(response.status(), await response.text()).toBe(201);
      adminAuth = await response.json();
      await expect(page.getByRole('heading', { name: 'Register dompet', exact: true })).toBeVisible();
      expect(errors, 'First-run setup has no uncaught UI exceptions').toEqual([]);
      await admin.dispose();
      admin = await request.newContext({ baseURL: BASE_URL, storageState: await context.storageState() });
    } finally {
      await context.close();
    }
  } else { adminAuth = await login(admin, 'browser-admin@walletledger.test'); }
});
test.afterAll(async () => { await admin?.dispose(); });
