export type Role = "Admin" | "Member";
export interface AuthUser {
  id: string;
  email: string;
  full_name: string;
  role: Role;
}
export interface AuthView {
  user: AuthUser;
  workspace: { id: string; name: string };
  csrf_token: string;
}
export interface Owner {
  user_id: string;
  workspace_id: string;
}
export interface Person {
  id: string;
  full_name: string;
  wallet_id: string;
}
export interface Wallet {
  id: string;
  user_id: string | null;
  owner_name: string;
  is_system: boolean;
  currency: "IDR";
  balance_minor: string;
  created_at: string;
}
export type TransactionKind = "TRANSFER" | "ALLOCATION" | "REVERSAL";
export interface Transaction {
  id: string;
  seq: number;
  kind: TransactionKind;
  amount_minor: string;
  note: string;
  created_by: string;
  created_by_name: string;
  from_wallet_id: string;
  to_wallet_id: string;
  from_name: string;
  to_name: string;
  created_at: string;
  reversal_of: string | null;
  reversal_id: string | null;
}
export interface JournalEntry {
  wallet_id: string;
  wallet_name: string;
  direction: "DEBIT" | "CREDIT";
  amount_minor: string;
}
export interface TransactionDetail {
  transaction: Transaction;
  entries: JournalEntry[];
  debit_total_minor: string;
  credit_total_minor: string;
}
export interface Page<T> {
  items: T[];
  next_before: number | null;
}
export interface Reconciliation {
  balanced: boolean;
  wallet_count: number;
  transaction_count: number;
  total_debit_minor: string;
  total_credit_minor: string;
  total_wallet_balance_minor: string;
  items: {
    wallet_id: string;
    owner_name: string;
    is_system: boolean;
    cached_balance_minor: string;
    ledger_balance_minor: string;
    difference_minor: string;
  }[];
}
export interface AuditEvent {
  id: string;
  seq: number;
  actor_name: string;
  kind: string;
  entity_id: string;
  created_at: string;
}
export interface MutationResult {
  request_id: string;
  replayed: boolean;
  transaction: TransactionDetail | null;
  user: AuthUser | null;
}
export interface TransferBody {
  recipient_id: string;
  amount_minor: string;
  note: string;
}
export interface PersonBody {
  full_name: string;
  email: string;
  password: string;
}
export type CommandBody = TransferBody | PersonBody | { reason: string };
export interface Command {
  readonly owner: Readonly<Owner>;
  readonly key: string;
  readonly method: "POST";
  readonly path: string;
  readonly body: string;
  readonly title: string;
  readonly persistent: boolean;
}
