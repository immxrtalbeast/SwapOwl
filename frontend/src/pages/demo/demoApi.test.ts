import { describe, expect, it } from "vitest";
import { describeBurst, parseBurstOrders, parseJumpHours, parseSpeed, tokenActionsVisible } from "./demoApi";

describe("demo input parsing", () => {
  it("speed is 0..3600", () => {
    expect(parseSpeed("600")).toBe(600);
    expect(parseSpeed("0")).toBe(0);
    expect(parseSpeed("3601")).toBeNull();
    expect(parseSpeed("-1")).toBeNull();
    expect(parseSpeed("abc")).toBeNull();
    expect(parseSpeed("")).toBeNull();
  });
  it("jump hours are in (0, 720]", () => {
    expect(parseJumpHours("6,5")).toBe(6.5);
    expect(parseJumpHours("0")).toBeNull();
    expect(parseJumpHours("720")).toBe(720);
    expect(parseJumpHours("721")).toBeNull();
  });
  it("burst is an integer 1..50", () => {
    expect(parseBurstOrders("10")).toBe(10);
    expect(parseBurstOrders("50")).toBe(50);
    expect(parseBurstOrders("51")).toBeNull();
    expect(parseBurstOrders("0")).toBeNull();
    expect(parseBurstOrders("2.5")).toBeNull();
  });
  it("token actions need a token", () => {
    expect(tokenActionsVisible("")).toBe(false);
    expect(tokenActionsVisible("  ")).toBe(false);
    expect(tokenActionsVisible("tok")).toBe(true);
  });
  it("describes burst result", () => {
    expect(describeBurst({ dish_id: "d", orders: 10, accepted: 4, rejected: 6 })).toBe("Принято 4, отклонено 6 из 10");
  });
});
