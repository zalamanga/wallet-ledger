import { expect } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { BASE_URL, PASSWORD, balance, commitTransfer, fund, headers, history, openApp, test, transferForm, wallets, write } from './helpers.js';

test('two real sessions transfer only after explicit confirmation and expose balanced journal', async ({ team }) => {
  const admin = await team.administrator();
  const sender = await team.member('Transfer sender');
  const receiver = await team.member('Transfer receiver');
  await fund(admin, sender, '125000');
  await openApp(sender);
  await openApp(receiver);
  const note = `Transfer UI ${randomUUID()}`;
  const dialog = await transferForm(sender, receiver, '42000', note);
  await expect(dialog).toContainText(receiver.auth.user.full_name);
  await expect(dialog).toContainText('42.000');
  expect(await balance(sender)).toBe('125000');
  expect(await balance(receiver)).toBe('0');
  await dialog.getByRole('button', { name: 'Kirim transfer', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect.poll(() => balance(sender)).toBe('83000');
  await receiver.page.getByRole('button', { name: 'Perbarui data', exact: true }).click();
  await expect.poll(() => balance(receiver)).toBe('42000');
  const accepted = (await history(sender)).find((transaction) => transaction.note === note);
  expect(accepted).toBeTruthy();
  const receipt = sender.page.getByRole('dialog', { name: `Bukti transaksi #${accepted!.seq}`, exact: true });
  await expect(receipt).toContainText('DEBIT');
  await expect(receipt).toContainText('CREDIT');
  const actual = await sender.context.request.get(`/api/transactions/${accepted!.id}`);
  const detail = await actual.json();
  expect(detail.entries).toHaveLength(2);
  expect(detail.debit_total_minor).toBe('42000');
  expect(detail.credit_total_minor).toBe('42000');
});

test('unknown committed response survives reload and retries the original immutable key once', async ({ team }) => {
  const admin = await team.administrator();
  const sender = await team.member('Unknown sender');
  const receiver = await team.member('Unknown receiver');
  await fund(admin, sender, '1000');
  await openApp(sender);
  const note = `Unknown committed UI ${randomUUID()}`;
  let firstKey = '';
  let firstBody = '';
  let committed = false;
  await sender.page.route('**/api/transfers', async (route) => {
    if (route.request().method() !== 'POST' || committed) return route.continue();
    firstKey = route.request().headers()['idempotency-key'];
    firstBody = route.request().postData() || '';
    const response = await route.fetch();
    expect(response.status()).toBe(201);
    committed = true;
    await route.abort('failed');
  });
  await commitTransfer(sender, receiver, '250', note);
  await expect(sender.page.getByText('Hasil permintaan belum diketahui', { exact: true })).toBeVisible();
  expect(committed).toBeTruthy();
  expect(await balance(sender)).toBe('750');
  expect(await balance(receiver)).toBe('250');
  await sender.page.unroute('**/api/transfers');
  await sender.page.reload();
  await expect(sender.page.getByText('Hasil permintaan belum diketahui', { exact: true })).toBeVisible();
  // A later accepted command makes the original receipt historical, so the UI must refresh current wallets.
  await fund(admin, sender, '100');
  const retries: { key: string; body: string }[] = [];
  sender.page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/transfers') retries.push({ key: request.headers()['idempotency-key'], body: request.postData() || '' });
  });
  await sender.page.getByRole('button', { name: 'Ulangi permintaan yang sama', exact: true }).click();
  await expect(sender.page.getByText('Hasil permintaan belum diketahui', { exact: true })).not.toBeVisible();
  expect(retries).toEqual([{ key: firstKey, body: firstBody }]);
  expect((await history(sender)).filter((transaction) => transaction.note === note)).toHaveLength(1);
  expect(await balance(sender)).toBe('850');
  expect(await balance(receiver)).toBe('250');
  await sender.page.getByRole('dialog').getByRole('button', { name: 'Tutup', exact: true }).click();
  await sender.page.getByRole('navigation', { name: 'Menu workspace' }).getByRole('button', { name: 'Ringkasan', exact: true }).click();
  await expect(sender.page.locator('main')).toContainText('850');
});

test('stale client CSRF receives a real denial then refreshes once with the same command', async ({ team }) => {
  const admin = await team.administrator();
  const sender = await team.member('CSRF sender');
  const receiver = await team.member('CSRF receiver');
  await fund(admin, sender, '100');
  let injected = false;
  await sender.page.route('**/api/auth/me', async (route) => {
    const upstream = await route.fetch();
    if (!injected && upstream.status() === 200) {
      injected = true;
      const body = await upstream.json();
      await route.fulfill({ response: upstream, json: { ...body, csrf_token: 'synthetic-stale-client-token' } });
    } else await route.fulfill({ response: upstream });
  });
  await openApp(sender);
  const posts: { key: string; body: string; status: number }[] = [];
  sender.page.on('response', (response) => {
    const request = response.request();
    if (request.method() === 'POST' && new URL(response.url()).pathname === '/api/transfers') posts.push({ key: request.headers()['idempotency-key'], body: request.postData() || '', status: response.status() });
  });
  await commitTransfer(sender, receiver, '25', `Real CSRF retry ${randomUUID()}`);
  await expect.poll(() => balance(sender)).toBe('75');
  expect(injected).toBeTruthy();
  expect(posts.map((post) => post.status)).toEqual([403, 201]);
  expect(posts[0].key).toBe(posts[1].key);
  expect(posts[0].body).toBe(posts[1].body);
  expect(await balance(receiver)).toBe('25');
});

test('cookie-switched actor cannot submit the previous account draft or replay automatically', async ({ team }) => {
  const admin = await team.administrator();
  const sender = await team.member('Original actor');
  const replacement = await team.member('Replacement actor');
  const receiver = await team.member('Identity recipient');
  await fund(admin, sender, '100');
  await openApp(sender);
  const dialog = await transferForm(sender, receiver, '10', `Identity draft ${randomUUID()}`);
  await sender.context.addCookies(await replacement.context.cookies());
  const responses: number[] = [];
  sender.page.on('response', (response) => { if (response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/transfers') responses.push(response.status()); });
  await dialog.getByRole('button', { name: 'Kirim transfer', exact: true }).click();
  await expect.poll(() => responses.length).toBe(1);
  expect(responses).toEqual([403]);
  const senderRead = await team.existing(sender.auth.user.email);
  expect(await balance(senderRead)).toBe('100');
  expect(await balance(replacement)).toBe('0');
  expect(await balance(receiver)).toBe('0');
  await expect(sender.page.getByRole('button', { name: 'Ulangi permintaan yang sama', exact: true })).toHaveCount(0);
});

test('cross-tab logout and login revalidate identity and clear the previous account data', async ({ team }) => {
  const admin = await team.administrator();
  const first = await team.member('Cross-tab first');
  const second = await team.member('Cross-tab second');
  await fund(admin, first, '123456');
  await openApp(first);
  const other = await first.context.newPage();
  await other.goto('/');
  await expect(other.getByRole('button', { name: 'Keluar', exact: true })).toBeVisible();
  await first.page.getByRole('button', { name: 'Keluar', exact: true }).click();
  await expect(other.getByRole('button', { name: 'Masuk', exact: true })).toBeVisible();
  await first.page.getByLabel('Email', { exact: true }).fill(second.auth.user.email);
  await first.page.getByLabel('Kata sandi', { exact: true }).fill(PASSWORD);
  await first.page.getByRole('button', { name: 'Masuk', exact: true }).click();
  await expect(other.getByRole('button', { name: 'Keluar', exact: true })).toBeVisible();
  await expect(other.locator('main')).not.toContainText('123.456');
  const me = await first.context.request.get('/api/auth/me');
  expect((await me.json()).user.id).toBe(second.auth.user.id);
});

test('offline disables financial confirmation and reconnect never submits a queued transfer', async ({ team }) => {
  const admin = await team.administrator();
  const sender = await team.member('Offline sender');
  const receiver = await team.member('Offline receiver');
  await fund(admin, sender, '100');
  await openApp(sender);
  const dialog = await transferForm(sender, receiver, '10', `Offline draft ${randomUUID()}`);
  const posts: string[] = [];
  sender.page.on('request', (request) => { if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/transfers') posts.push(request.url()); });
  await sender.context.setOffline(true);
  await expect(dialog.getByRole('button', { name: 'Kirim transfer', exact: true })).toBeDisabled();
  await sender.context.setOffline(false);
  await expect(dialog.getByRole('button', { name: 'Kirim transfer', exact: true })).toBeEnabled();
  expect(posts).toEqual([]);
  expect(await balance(sender)).toBe('100');
  await sender.page.keyboard.press('Escape');
  await expect(dialog).not.toBeVisible();
});

test('administrator creates Member and allocates synthetic balance using actual forms', async ({ team }) => {
  const admin = await team.administrator();
  await openApp(admin);
  const name = `UI Member ${randomUUID().slice(0, 8)}`;
  const email = `ui-${randomUUID()}@walletledger.test`;
  await admin.page.getByRole('navigation', { name: 'Menu workspace' }).getByRole('button', { name: 'Akun pengguna', exact: true }).click();
  await admin.page.getByLabel('Nama lengkap', { exact: true }).fill(name);
  await admin.page.getByLabel('Email akun', { exact: true }).fill(email);
  await admin.page.getByLabel(/^Kata sandi awal/).fill(PASSWORD);
  await admin.page.getByRole('button', { name: 'Buat akun', exact: true }).click();
  const people = await admin.context.request.get('/api/admin/people');
  await expect.poll(async () => (await (await admin.context.request.get('/api/admin/people')).json()).items.some((person: { email: string }) => person.email === email)).toBeTruthy();
  expect(people.ok()).toBeTruthy();
  const recipient = await team.existing(email);
  expect(recipient.auth.user.role).toBe('Member');
  await admin.page.getByRole('navigation', { name: 'Menu workspace' }).getByRole('button', { name: 'Alokasi saldo', exact: true }).click();
  const recipientField = admin.page.getByLabel('Penerima', { exact: true });
  await expect(recipientField).toHaveAccessibleName('Penerima');
  await recipientField.selectOption(recipient.auth.user.id);
  await expect(recipientField).toHaveAccessibleName('Penerima');
  const amountField = admin.page.getByLabel(/^Jumlah \(IDR\)/);
  await expect(amountField).toHaveAccessibleName('Jumlah (IDR)');
  await expect(amountField).toHaveAccessibleDescription('Isi angka 1 sampai 1000000000 tanpa pemisah ribuan.');
  await amountField.fill('750000');
  await admin.page.getByLabel('Catatan', { exact: true }).fill('Alokasi sintetis dari formulir admin');
  await admin.page.getByRole('button', { name: 'Tinjau alokasi', exact: true }).click();
  const dialog = admin.page.getByRole('dialog', { name: 'Konfirmasi alokasi', exact: true });
  expect(await balance(recipient)).toBe('0');
  await dialog.getByRole('button', { name: 'Catat alokasi', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect.poll(() => balance(recipient)).toBe('750000');
  await admin.page.getByRole('dialog').getByRole('button', { name: 'Tutup', exact: true }).click();
  await admin.page.getByRole('navigation', { name: 'Menu workspace' }).getByRole('button', { name: 'Rekonsiliasi', exact: true }).click();
  const reconciliation = await admin.context.request.get('/api/reconciliation');
  const data = await reconciliation.json();
  expect(data.balanced).toBe(true);
  expect(data.total_wallet_balance_minor).toBe('0');
  expect(data.total_debit_minor).toBe(data.total_credit_minor);
  await expect(admin.page.getByRole('heading', { name: 'Rekonsiliasi', exact: true })).toBeVisible();
});

test('administrator reverses a transfer once through the confirmation and sees immutable audit', async ({ team }) => {
  const admin = await team.administrator();
  const sender = await team.member('Reversal sender');
  const receiver = await team.member('Reversal receiver');
  await fund(admin, sender, '1000');
  const transfer = (await write(sender, '/api/transfers', { recipient_id: receiver.auth.user.id, amount_minor: '300', note: `UI reversal ${randomUUID()}` })).transaction!.transaction;
  await openApp(admin);
  await admin.page.getByRole('navigation', { name: 'Menu workspace' }).getByRole('button', { name: 'Riwayat', exact: true }).click();
  await admin.page.getByRole('button', { name: `Buka transaksi ${transfer.seq}`, exact: true }).click();
  await admin.page.getByRole('button', { name: 'Tinjau pembalikan', exact: true }).click();
  const confirmation = admin.page.getByRole('dialog', { name: 'Konfirmasi pembalikan', exact: true });
  await confirmation.getByLabel(/^Alasan pembalikan/).fill('Pembalikan sintetis melalui formulir admin');
  expect(await balance(receiver)).toBe('300');
  await confirmation.getByRole('button', { name: 'Balikkan transfer', exact: true }).click();
  await expect.poll(() => balance(sender)).toBe('1000');
  expect(await balance(receiver)).toBe('0');
  const original = await admin.context.request.get(`/api/transactions/${transfer.id}`);
  const originalDetail = await original.json();
  expect(originalDetail.transaction.reversal_id).toBeTruthy();
  expect((await history(sender)).filter((item) => item.reversal_of === transfer.id)).toHaveLength(1);
  await admin.page.keyboard.press('Escape');
  await admin.page.getByRole('navigation', { name: 'Menu workspace' }).getByRole('button', { name: 'Audit', exact: true }).click();
  await expect(admin.page.getByRole('heading', { name: 'Audit', exact: true })).toBeVisible();
  const audit = await admin.context.request.get('/api/audit?limit=100');
  expect((await audit.json()).items.filter((item: { kind: string; entity_id: string }) => item.kind === 'REVERSAL' && item.entity_id === originalDetail.transaction.reversal_id)).toHaveLength(1);
});

test('Member has no administrator controls and server hides another wallet and receipt', async ({ team }) => {
  const admin = await team.administrator();
  const member = await team.member('Private member');
  const other = await team.member('Unrelated member');
  const allocation = await fund(admin, other, '500');
  await openApp(member);
  for (const name of ['Alokasi saldo', 'Akun pengguna', 'Rekonsiliasi', 'Audit']) await expect(member.page.getByRole('button', { name, exact: true })).toHaveCount(0);
  expect((await wallets(member))).toHaveLength(1);
  const foreignWallet = (await wallets(other))[0];
  expect((await member.context.request.get(`/api/wallets/${foreignWallet.id}`)).status()).toBe(404);
  expect((await member.context.request.get(`/api/transactions/${allocation.transaction!.transaction.id}`)).status()).toBe(404);
  expect((await member.context.request.get('/api/reconciliation')).status()).toBe(403);
  const denied = await member.context.request.post('/api/allocations', { data: { recipient_id: member.auth.user.id, amount_minor: '1', note: 'Actual forbidden member command' }, headers: headers(member.auth) });
  expect(denied.status()).toBe(403);
  expect(await balance(member)).toBe('0');
});

test('confirmation is keyboard accessible, Escape restores focus and never commits', async ({ team }, testInfo) => {
  const admin = await team.administrator();
  const sender = await team.member('Keyboard sender');
  const receiver = await team.member('Keyboard receiver');
  await fund(admin, sender, '100');
  await openApp(sender);
  const dialog = await transferForm(sender, receiver, '10', 'Keyboard cancellation test');
  await expect(dialog).toBeVisible();
  const trace: { step: number; action: string; inside: boolean; tag: string | null; page_focused: boolean; modal: boolean }[] = [];
  const focusState = () => dialog.evaluate((element) => ({
    inside: element.contains(document.activeElement),
    tag: document.activeElement?.tagName ?? null,
    page_focused: document.hasFocus(),
    modal: element.matches(':modal'),
  }));
  let browserChromeTransitions = 0;
  try {
    for (let i = 0; i < 7; i++) {
      await sender.page.keyboard.press('Tab');
      const focus = await focusState();
      trace.push({ step: i, action: 'Tab', ...focus });
      expect(focus.modal, 'Confirmation remains a native modal dialog').toBe(true);
      if (focus.inside) continue;
      expect(focus.tag, 'Focus must never reach a background app control').toBe('BODY');
      expect(focus.page_focused, 'Only an actual browser chrome transition may leave the dialog').toBe(false);
      await sender.page.keyboard.press('Tab');
      const returned = await focusState();
      trace.push({ step: i, action: 'Tab from browser chrome', ...returned });
      expect(returned.inside, 'The immediate next Tab returns focus inside the dialog').toBe(true);
      expect(returned.page_focused).toBe(true);
      expect(returned.modal).toBe(true);
      browserChromeTransitions += 1;
    }
  } finally {
    await testInfo.attach('native-dialog-focus-trace', {
      body: JSON.stringify({ browser_chrome_transitions: browserChromeTransitions, trace }, null, 2),
      contentType: 'application/json',
    });
  }
  await sender.page.keyboard.press('Escape');
  await expect(dialog).not.toBeVisible();
  await expect(sender.page.getByRole('button', { name: 'Tinjau transfer', exact: true })).toBeFocused();
  expect(await balance(sender)).toBe('100');
  expect(await balance(receiver)).toBe('0');
});

test('phone layouts and text at 200 percent keep actual financial controls reachable', async ({ team }) => {
  const admin = await team.administrator();
  const sender = await team.member('Mobile sender');
  const receiver = await team.member('Mobile receiver');
  await fund(admin, sender, '100');
  for (const width of [320, 390, 1440]) {
    await sender.page.setViewportSize({ width, height: 900 });
    await openApp(sender);
    expect(await sender.page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true);
    const dialog = await transferForm(sender, receiver, '10', `Responsive cancellation ${width}`);
    await expect(dialog.getByRole('button', { name: 'Kirim transfer', exact: true })).toBeVisible();
    expect(await sender.page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true);
    await sender.page.keyboard.press('Escape');
  }
  await sender.page.setViewportSize({ width: 390, height: 900 });
  await sender.page.addStyleTag({ content: 'html { font-size: 200% !important; }' });
  await sender.page.getByRole('navigation', { name: 'Menu workspace' }).getByRole('button', { name: 'Kirim saldo', exact: true }).click();
  await expect(sender.page.getByLabel('Penerima', { exact: true })).toBeVisible();
  expect(await sender.page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true);
  expect(await balance(sender)).toBe('100');
});

test('late response from logged-out identity cannot restore old wallet after the next login', async ({ team }) => {
  const admin = await team.administrator();
  const first = await team.member('Late response first');
  const next = await team.member('Late response next');
  await fund(admin, first, '654321');
  await openApp(first);
  let release: () => void = () => undefined;
  let reached: () => void = () => undefined;
  const held = new Promise<void>((resolve) => { release = resolve; });
  const requested = new Promise<void>((resolve) => { reached = resolve; });
  let intercepted = false;
  await first.page.route('**/api/wallets', async (route) => {
    if (intercepted || route.request().method() !== 'GET') return route.continue();
    intercepted = true;
    const upstream = await route.fetch();
    reached();
    await held;
    try { await route.fulfill({ response: upstream }); } catch { /* The previous identity request may already be aborted. */ }
  });
  await first.page.getByRole('button', { name: 'Perbarui data', exact: true }).click();
  await requested;
  await first.page.getByRole('button', { name: 'Keluar', exact: true }).click();
  await expect(first.page.getByRole('button', { name: 'Masuk', exact: true })).toBeVisible();
  await first.page.getByLabel('Email', { exact: true }).fill(next.auth.user.email);
  await first.page.getByLabel('Kata sandi', { exact: true }).fill(PASSWORD);
  await first.page.getByRole('button', { name: 'Masuk', exact: true }).click();
  await expect(first.page.getByRole('button', { name: 'Keluar', exact: true })).toBeVisible();
  release();
  await expect(first.page.getByRole('heading', { name: 'Register dompet', exact: true })).toBeVisible();
  await expect(first.page.locator('main')).not.toContainText('654.321');
  expect((await (await first.context.request.get('/api/auth/me')).json()).user.id).toBe(next.auth.user.id);
  await first.page.unroute('**/api/wallets');
});
