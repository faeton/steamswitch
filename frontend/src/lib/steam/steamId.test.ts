import { describe, expect, it } from "vitest";
import { extractSteamId64, isValidSteamId64 } from "./steamId";

describe("isValidSteamId64", () => {
  it("accepts a real individual SteamID64", () => {
    expect(isValidSteamId64("76561198044219337")).toBe(true);
    expect(isValidSteamId64("  76561198044219337  ")).toBe(true);
  });

  it("rejects the wrong length", () => {
    expect(isValidSteamId64("7656119804421933")).toBe(false);
    expect(isValidSteamId64("765611980442193377")).toBe(false);
  });

  /*
    The prefix check is what separates a SteamID64 from any other 17-digit number — a pasted
    order number or a Discord snowflake would otherwise be accepted and become an entry keyed
    on an account that does not exist.
  */
  it("rejects a 17-digit number that is not a SteamID64", () => {
    expect(isValidSteamId64("12345678901234567")).toBe(false);
    expect(isValidSteamId64("99999999999999999")).toBe(false);
  });

  it("rejects non-digits and empty input", () => {
    expect(isValidSteamId64("")).toBe(false);
    expect(isValidSteamId64("7656119804421933x")).toBe(false);
    expect(isValidSteamId64("STEAM_0:1:42109804")).toBe(false);
  });
});

describe("extractSteamId64", () => {
  it("passes a bare id straight through", () => {
    expect(extractSteamId64("76561198044219337")).toBe("76561198044219337");
  });

  it("pulls the id out of a profile URL", () => {
    expect(extractSteamId64("https://steamcommunity.com/profiles/76561198044219337")).toBe(
      "76561198044219337",
    );
    expect(extractSteamId64("steamcommunity.com/profiles/76561198044219337/")).toBe(
      "76561198044219337",
    );
  });

  // Vanity URLs need Valve's API to resolve. Returning nothing is honest; guessing is not.
  it("returns nothing for a vanity URL rather than guessing", () => {
    expect(extractSteamId64("https://steamcommunity.com/id/somename")).toBe("");
  });

  it("returns nothing for junk", () => {
    expect(extractSteamId64("not an id")).toBe("");
    expect(extractSteamId64("")).toBe("");
  });
});
