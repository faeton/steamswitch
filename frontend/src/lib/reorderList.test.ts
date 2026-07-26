import { describe, expect, it } from "vitest";
import { reorderItemByCommand } from "./reorderList";

class FakeElement {
  readonly children: FakeElement[] = [];
  parent: FakeElement | null = null;
  removed = false;
  style: Record<string, string> = {};
  private readonly attrs = new Map<string, string>();
  readonly classList = {
    remove: (name: string) => {
      const classes = new Set((this.attrs.get("class") ?? "").split(/\s+/).filter(Boolean));
      classes.delete(name);
      this.attrs.set("class", Array.from(classes).join(" "));
    },
  };

  constructor(readonly tagName: string, attrs: Record<string, string> = {}) {
    for (const [key, value] of Object.entries(attrs)) {
      this.attrs.set(key, value);
    }
  }

  append(...children: FakeElement[]): this {
    for (const child of children) {
      child.parent = this;
      this.children.push(child);
    }
    return this;
  }

  remove(): void {
    this.removed = true;
    if (this.parent) {
      const index = this.parent.children.indexOf(this);
      if (index >= 0) this.parent.children.splice(index, 1);
    }
  }

  removeAttribute(name: string): void {
    this.attrs.delete(name);
  }

  getAttribute(name: string): string | null {
    return this.attrs.get(name) ?? null;
  }

  querySelectorAll(selector: string): FakeElement[] {
    const selectors = selector.split(",").map((item) => item.trim());
    return descendants(this).filter((element) => selectors.some((item) => matchesSelector(element, item)));
  }
}

function descendants(root: FakeElement): FakeElement[] {
  return root.children.flatMap((child) => [child, ...descendants(child)]);
}

function matchesSelector(element: FakeElement, selector: string): boolean {
  if (selector === "input") return element.tagName === "INPUT";
  if (selector === "button") return element.tagName === "BUTTON";
  if (selector === "img") return element.tagName === "IMG";
  if (selector === "svg") return element.tagName === "SVG";
  if (selector === ".button") return (element.getAttribute("class") ?? "").split(/\s+/).includes("button");
  if (selector === "[role='button']") return element.getAttribute("role") === "button";
  if (selector === "[id]") return element.getAttribute("id") !== null;
  if (selector === "label[for]") return element.tagName === "LABEL" && element.getAttribute("for") !== null;
  return false;
}

describe("reorderItemByCommand", () => {
  it("moves an item one slot left and right", () => {
    expect(reorderItemByCommand(["a", "b", "c"], "b", "left")).toMatchObject({
      items: ["b", "a", "c"],
      moved: true,
      position: 1,
      total: 3,
    });
    expect(reorderItemByCommand(["a", "b", "c"], "b", "right")).toMatchObject({
      items: ["a", "c", "b"],
      moved: true,
      position: 3,
      total: 3,
    });
  });

  it("moves an item to the start or end", () => {
    expect(reorderItemByCommand(["a", "b", "c", "d"], "c", "start")).toMatchObject({
      items: ["c", "a", "b", "d"],
      moved: true,
      position: 1,
    });
    expect(reorderItemByCommand(["a", "b", "c", "d"], "b", "end")).toMatchObject({
      items: ["a", "c", "d", "b"],
      moved: true,
      position: 4,
    });
  });

  it("reports boundary and missing-item no-ops", () => {
    expect(reorderItemByCommand(["a", "b"], "a", "left")).toMatchObject({
      items: ["a", "b"],
      moved: false,
      position: 1,
    });
    expect(reorderItemByCommand(["a", "b"], "x", "right")).toMatchObject({
      items: ["a", "b"],
      moved: false,
      position: 0,
      total: 2,
    });
  });
});
