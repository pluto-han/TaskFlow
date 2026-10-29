import { expect, it } from "vitest";
import { toLocalInput, toUTC } from "./dates";

it("round-trips local calendar input through UTC and preserves null", () => {
  const local = "2026-07-15T14:30:20";
  const utc = toUTC(local)!;
  expect(toLocalInput(utc)).toBe(local);
  expect(utc).toBe(new Date(2026, 6, 15, 14, 30, 20).toISOString());
  expect(toUTC("")).toBeNull();
  expect(toLocalInput(null)).toBe("");
  expect(() => toUTC("not-a-date")).toThrow();
  expect(() => toUTC("2026-02-31T12:00")).toThrow();
});
