import { describe, expect, it } from "vitest";
import { amountPerspective, parseAmount, rupiah } from "./money";
import type { Transaction } from "./types";

describe("integer rupiah", () => {
  it("accepts the smallest and largest command without numeric conversion", () => {
    expect(parseAmount("1")).toBe("1");
    expect(parseAmount("1000000000")).toBe("1000000000");
  });
  it("rejects fractions, grouping, signs, exponent, whitespace and leading zeros", () => {
    for (const value of ["", "0", "01", "1.00", "1,000", " 1", "1 ", "+1", "-1", "1e3", "NaN", "Infinity", "١٢"]) {
      expect(() => parseAmount(value), value).toThrow();
    }
  });
  it("rejects amounts above the command cap including arbitrarily long input", () => {
    expect(() => parseAmount("1000000001")).toThrow();
    expect(() => parseAmount("9".repeat(400))).toThrow();
  });
  it("formats the system funding range exactly without floating point", () => {
    expect(rupiah("-100000000000000")).toBe("−Rp100.000.000.000.000");
    expect(rupiah("0")).toBe("Rp0");
    expect(rupiah("1000000000000")).toBe("Rp1.000.000.000.000");
    expect(rupiah("12.5")).toBe("Jumlah tidak valid");
  });
  it("marks money by the signed-in wallet while unrelated admin rows stay neutral", () => {
    const transaction = { from_wallet_id: "sender", to_wallet_id: "recipient", amount_minor: "25000" } as Transaction;
    expect(amountPerspective(transaction, "sender")).toBe("−Rp25.000");
    expect(amountPerspective(transaction, "recipient")).toBe("+Rp25.000");
    expect(amountPerspective(transaction, "admin")).toBe("Rp25.000");
  });
});
