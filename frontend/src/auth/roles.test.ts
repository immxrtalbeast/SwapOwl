import { describe, expect, it } from "vitest";
import { hasRole } from "./roles";

describe("hasRole", () => {
  it("viewer не проходит на operator", () => expect(hasRole("viewer", "operator")).toBe(false));
  it("operator проходит на operator и viewer", () => {
    expect(hasRole("operator", "operator")).toBe(true);
    expect(hasRole("operator", "viewer")).toBe(true);
  });
  it("admin проходит везде", () => expect(hasRole("admin", "operator")).toBe(true));
  it("без роли доступа нет", () => expect(hasRole(null, "viewer")).toBe(false));
});
