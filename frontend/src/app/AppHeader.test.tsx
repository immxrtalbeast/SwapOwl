import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { UserBadge } from "./AppHeader";

describe("UserBadge", () => {
  it("на узкой ширине имя свёрнуто в иконку с подсказкой", () => {
    const out = renderToStaticMarkup(<UserBadge name="Гость" role="оператор" narrow />);
    expect(out).toContain("<svg");
    expect(out).toContain('title="Гость · оператор"');
    expect(out).not.toMatch(/>Гость · оператор</);
  });
  it("на широкой — текстом", () => {
    expect(renderToStaticMarkup(<UserBadge name="Гость" role="оператор" narrow={false} />)).toMatch(/>Гость · оператор</);
  });
});
