import { describe, expect, it } from "vitest";
import { validate } from "../../../src/auth/validation";

describe("form validation", () => {
  it("matches password character/byte limits without trimming passwords", () => {
    expect(
      validate(" learner@example.com ", "密".repeat(8), "密".repeat(8)),
    ).toEqual({});
    expect(
      validate("learner@example.com", "x".repeat(7), "x".repeat(7)),
    ).toHaveProperty("password");
    expect(validate("learner@example.com", "12345678", "12345678")).toEqual({});
    expect(
      validate("learner@example.com", "😀".repeat(7), "😀".repeat(7)),
    ).toHaveProperty("password");
    expect(
      validate("learner@example.com", "😀".repeat(128), "😀".repeat(128)),
    ).toEqual({});
    expect(
      validate("learner@example.com", "😀".repeat(129), "😀".repeat(129)),
    ).toHaveProperty("password");
    expect(
      validate("learner@example.com", " ".repeat(8), " ".repeat(8)),
    ).toHaveProperty("password");
  });
  it("checks confirmation, email and missing credentials", () => {
    expect(validate("invalid", "")).toEqual({
      email: expect.any(String),
      password: expect.any(String),
    });
    expect(
      validate("a@example.com", "long-enough-password", "different"),
    ).toHaveProperty("confirm");
    expect(validate("a@example.com", "short")).toEqual({});
  });
});
