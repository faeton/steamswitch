import { describe, expect, it } from "vitest";
import { canCheckEmail, emailCheckView } from "./emailCheck";

describe("emailCheckView", () => {
  it("has nothing to say about an unchecked account", () => {
    expect(emailCheckView(null)).toBeNull();
    expect(emailCheckView({})).toBeNull();
  });

  it("carries the inbox count, including an empty inbox", () => {
    expect(emailCheckView({ status: "ok", messages: 12 })?.params.count).toBe(12);
    expect(emailCheckView({ status: "ok", messages: 0 })?.params.count).toBe(0);
  });

  it("names the provider that refused the password", () => {
    const view = emailCheckView({ status: "app-password", provider: "Gmail" });
    expect(view?.tone).toBe("fail");
    expect(view?.detailKey).toBe("Vault_EmailCheck_AppPassword");
    expect(view?.params.provider).toBe("Gmail");
  });

  it("does not paint an unreachable server as a bad password", () => {
    expect(emailCheckView({ status: "connect" })?.tone).toBe("warn");
  });

  it("ignores a status from a newer build", () => {
    expect(emailCheckView({ status: "something-new" })).toBeNull();
  });
});

describe("canCheckEmail", () => {
  it("only offers a check where there is something to sign in to", () => {
    expect(canCheckEmail("imap")).toBe(true);
    expect(canCheckEmail("mailbox-api")).toBe(true);
    expect(canCheckEmail("manual")).toBe(false);
    expect(canCheckEmail("none")).toBe(false);
    expect(canCheckEmail(undefined)).toBe(false);
  });
});
