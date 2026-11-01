"""Populate an empty local WalletLedger with synthetic IDR transactions."""

import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import urllib.error
import urllib.request
from uuid import uuid4

ROOT = Path(__file__).resolve().parent.parent
BASE = 'http://127.0.0.1:8170'
STATE = ROOT / '.local/demo-state.json'


class Client:
    def __init__(self):
        self.http = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        self.auth = None

    def call(self, path, body=None, method='GET', key=None):
        headers = {'Accept': 'application/json'}
        if method != 'GET':
            csrf = self.auth['csrf_token'] if self.auth else self.call('/api/auth/csrf')['csrf_token']
            headers.update({'Origin': BASE, 'Content-Type': 'application/json', 'X-CSRF-Token': csrf})
            if self.auth:
                headers.update({'X-Wallet-Actor': self.auth['user']['id'], 'X-Wallet-Workspace': self.auth['workspace']['id']})
        if key:
            headers['Idempotency-Key'] = key
        request = urllib.request.Request(BASE + path, data=None if body is None else json.dumps(body).encode(), headers=headers, method=method)
        try:
            with self.http.open(request, timeout=30) as response:
                result = response.read()
                return json.loads(result) if result else None
        except urllib.error.HTTPError as error:
            raise RuntimeError(f'HTTP {error.code} at {path}. The private checkpoint remains available; inspect the uncertain step before repeating it.') from None

    def login(self, account):
        self.auth = self.call('/api/auth/login', {'email': account['email'], 'password': account['password']}, 'POST')
        return self.auth


def save(state):
    if STATE.is_symlink() or STATE.parent.is_symlink():
        raise RuntimeError('Refusing a symbolic checkpoint path.')
    STATE.parent.mkdir(exist_ok=True)
    temporary = STATE.with_suffix('.tmp')
    if temporary.is_symlink():
        raise RuntimeError('Refusing a symbolic temporary file.')
    temporary.write_text(json.dumps(state, indent=2) + '\n', encoding='utf-8')
    temporary.chmod(0o600)
    os.replace(temporary, STATE)


def seed():
    admin = Client()
    schema = admin.call('/api/openapi.json')
    if schema.get('info', {}).get('title') != 'WalletLedger API' or '/transfers' not in schema.get('paths', {}) or not any(server.get('url') == '/api' for server in schema.get('servers', [])):
        raise RuntimeError('Port8170 is not the expected WalletLedger API.')
    status = admin.call('/api/auth/status')
    if STATE.is_symlink():
        raise RuntimeError('Refusing a symbolic checkpoint.')
    if STATE.exists():
        state = json.loads(STATE.read_text(encoding='utf-8'))
        if state.get('scope') != 'walletledger-training-v1':
            raise RuntimeError('Unrecognized checkpoint.')
        if state.get('inflight'):
            raise RuntimeError('Unresolved step: ' + state['inflight'] + '. Inspect its saved request and server result; no writes attempted.')
        if status['setup_required']:
            raise RuntimeError('Checkpoint exists but server is empty; refusing replacement setup.')
        identity = admin.login(state['accounts']['admin'])
        if identity['workspace']['id'] != state.get('workspace_id'):
            raise RuntimeError('Checkpoint belongs to another workspace.')
        if state.get('complete'):
            print('Training already complete; no financial/account writes. Credentials: .local/LOCAL-CREDENTIALS.txt')
            return
    else:
        if not status['setup_required']:
            raise RuntimeError('Workspace already exists without this checkpoint; no records changed.')
        state = {'scope': 'walletledger-training-v1', 'accounts': {}, 'requests': {}, 'results': {}, 'inflight': 'initial-setup', 'complete': False}
        for role, name in [('admin', 'Administrator'), ('ayu', 'Ayu'), ('dimas', 'Dimas'), ('surya', 'Surya')]:
            state['accounts'][role] = {'full_name': 'Training ' + name, 'email': role + '@walletledger.test', 'password': 'Aa7!' + secrets.token_urlsafe(28)}
        save(state)
        admin.auth = admin.call('/api/auth/setup', {'workspace_name': 'WalletLedger Training', **state['accounts']['admin']}, 'POST')
        state['workspace_id'] = admin.auth['workspace']['id']
        state['inflight'] = None
        save(state)

    def step(name, client, path, body):
        if name in state['results']:
            return state['results'][name]
        command = state['requests'].setdefault(name, {'key': str(uuid4()), 'method': 'POST', 'path': path, 'body': body})
        state['inflight'] = name
        save(state)
        result = client.call(command['path'], command['body'], command['method'], command['key'])
        state['results'][name] = result
        state['inflight'] = None
        save(state)
        return result

    people = {'admin': admin.auth['user']}
    clients = {'admin': admin}
    for role in ['ayu', 'dimas', 'surya']:
        people[role] = step('account-' + role, admin, '/api/people', state['accounts'][role])['user']
        clients[role] = Client()
        clients[role].login(state['accounts'][role])
    for role, amount in [('admin', '200000'), ('ayu', '500000'), ('dimas', '500000'), ('surya', '500000')]:
        step('allocation-' + role, admin, '/api/allocations', {'recipient_id': people[role]['id'], 'amount_minor': amount, 'note': 'Training: alokasi saldo simulasi awal'})
    transfers = []
    for index, (sender, recipient, amount, note) in enumerate([
        ('ayu', 'dimas', '125000', 'Training: pembagian biaya perlengkapan'),
        ('dimas', 'surya', '50000', 'Training: penggantian biaya transport'),
        ('surya', 'ayu', '75000', 'Training: pembagian biaya kegiatan'),
        ('ayu', 'surya', '90000', 'Training: penggantian biaya konsumsi'),
        ('admin', 'ayu', '25000', 'Training: contoh transfer administrator'),
    ]):
        transfers.append(step('transfer-' + str(index), clients[sender], '/api/transfers', {'recipient_id': people[recipient]['id'], 'amount_minor': amount, 'note': note})['transaction'])
    original = transfers[0]['transaction']['id']
    reversal = step('reversal-example', admin, '/api/transactions/' + original + '/reverse', {'reason': 'Training: koreksi transfer contoh melalui jurnal pembalik'})['transaction']
    reconciliation = admin.call('/api/reconciliation')
    assert reconciliation['balanced'] is True
    assert reconciliation['wallet_count'] == 5 and reconciliation['transaction_count'] == 10
    assert reconciliation['total_wallet_balance_minor'] == '0'
    assert reconciliation['total_debit_minor'] == reconciliation['total_credit_minor']
    assert all(item['difference_minor'] == '0' for item in reconciliation['items'])
    expected = {'admin': '175000', 'ayu': '510000', 'dimas': '450000', 'surya': '565000'}
    for role, balance in expected.items():
        own = next(wallet for wallet in clients[role].call('/api/wallets')['items'] if wallet['user_id'] == people[role]['id'])
        assert own['balance_minor'] == balance
    state.update({'complete': True, 'main_transaction_id': original, 'reversal_id': reversal['transaction']['id'], 'reconciliation': {'wallet_count': 5, 'transaction_count': 10, 'balanced': True}})
    credentials = ROOT / '.local/LOCAL-CREDENTIALS.txt'
    if credentials.is_symlink():
        raise RuntimeError('Refusing a symbolic credentials file.')
    credentials.write_text('WalletLedger synthetic IDR training\n' + BASE + '\n\n' + '\n\n'.join(f"{role}\nEmail: {account['email']}\nPassword: {account['password']}" for role, account in state['accounts'].items()) + '\n', encoding='utf-8')
    credentials.chmod(0o600)
    save(state)
    print('Training ready:4 accounts,5 wallets,10 balanced synthetic transactions. Cached and journal balances agree; private credentials:.local/LOCAL-CREDENTIALS.txt. No passwords printed.')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--confirm-synthetic', action='store_true')
    args = parser.parse_args()
    if not args.confirm_synthetic:
        parser.error('Add --confirm-synthetic to populate an empty training workspace.')
    try:
        seed()
    except (OSError, ValueError, AssertionError, RuntimeError, urllib.error.URLError) as error:
        raise SystemExit(str(error)) from None
