import type { Transaction, TransactionKind } from "./types";

export const MAX_COMMAND = 1_000_000_000n;

export function parseAmount(value: string): string {
  if (!/^[1-9][0-9]*$/.test(value)) {
    throw new Error("Masukkan rupiah bulat tanpa titik, koma, spasi, atau nol di depan.");
  }
  if (value.length > 10 || BigInt(value) > MAX_COMMAND) {
    throw new Error("Jumlah paling besar Rp1.000.000.000 per permintaan.");
  }
  return value;
}

export function rupiah(value: string): string {
  if (!/^-?(0|[1-9][0-9]*)$/.test(value)) return "Jumlah tidak valid";
  const negative = value.startsWith("-");
  const digits = negative ? value.slice(1) : value;
  return `${negative ? "−" : ""}Rp${digits.replace(/\B(?=(\d{3})+(?!\d))/g, ".")}`;
}

export function amountPerspective(transaction: Transaction, walletId: string | undefined): string {
  if (!walletId) return rupiah(transaction.amount_minor);
  if (transaction.from_wallet_id === walletId) return `−${rupiah(transaction.amount_minor)}`;
  if (transaction.to_wallet_id === walletId) return `+${rupiah(transaction.amount_minor)}`;
  return rupiah(transaction.amount_minor);
}

export const kindLabel: Record<TransactionKind, string> = {
  TRANSFER: "Transfer",
  ALLOCATION: "Alokasi latihan",
  REVERSAL: "Pembalikan",
};

export function dateTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Waktu tidak tersedia";
  return new Intl.DateTimeFormat("id-ID", {
    day: "2-digit", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit",
  }).format(date);
}
