import { describe, expect, it } from "vitest";
import {
  canMarkShared,
  EMPTY_ROLES,
  kitTravelsTo,
  orderAccountIds,
  roleOf,
  type AccountRoleMap,
} from "./accountRoles";

const roles: AccountRoleMap = { homeSteamId64: "1", sharedIds: ["2", "3"] };

describe("roleOf", () => {
  it("classifies home, shared and plain accounts", () => {
    expect(roleOf(roles, "1")).toBe("home");
    expect(roleOf(roles, "2")).toBe("shared");
    expect(roleOf(roles, "9")).toBe("plain");
  });

  it("tolerates whitespace and empty ids", () => {
    expect(roleOf(roles, " 1 ")).toBe("home");
    expect(roleOf(roles, "")).toBe("plain");
    expect(roleOf({ homeSteamId64: "  ", sharedIds: [] }, "")).toBe("plain");
  });
});

describe("kitTravelsTo", () => {
  it("is true only for shared accounts once a home account exists", () => {
    expect(kitTravelsTo(roles, "2")).toBe(true);
    expect(kitTravelsTo(roles, "1")).toBe(false);
    expect(kitTravelsTo(roles, "9")).toBe(false);
  });

  it("is false with no home account, since there is no kit to carry", () => {
    expect(kitTravelsTo({ homeSteamId64: "", sharedIds: ["2"] }, "2")).toBe(false);
  });
});

describe("orderAccountIds", () => {
  it("puts home first, then shared, then the rest", () => {
    expect(orderAccountIds(roles, ["9", "2", "1", "8", "3"])).toEqual(["1", "2", "3", "9", "8"]);
  });

  it("keeps the saved order inside each group", () => {
    expect(orderAccountIds(roles, ["3", "2"])).toEqual(["3", "2"]);
    expect(orderAccountIds(EMPTY_ROLES, ["c", "a", "b"])).toEqual(["c", "a", "b"]);
  });

  it("does not mutate its input", () => {
    const ids = ["9", "1"];
    orderAccountIds(roles, ids);
    expect(ids).toEqual(["9", "1"]);
  });
});

describe("canMarkShared", () => {
  it("refuses to make the home account shared", () => {
    expect(canMarkShared(roles, "1")).toBe(false);
    expect(canMarkShared(roles, "9")).toBe(true);
  });
});
