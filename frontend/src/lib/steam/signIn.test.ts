import { describe, expect, it } from "vitest";
import { asSignInStatus, dockResult, heroLoginCopy, signInMark } from "./signIn";

describe("dockResult", () => {
  it("claims a sign-in only when Steam confirmed it", () => {
    expect(dockResult("confirmed", "", true).titleKey).toBe("Switch_Done_Title");
    for (const state of ["none", "waiting", "needsSignIn", "unconfirmed", "steamExited"] as const) {
      expect(dockResult(state, "", true).titleKey).not.toBe("Switch_Done_Title");
    }
  });

  it("says why when Steam rejected the login", () => {
    const expired = dockResult("needsSignIn", "License expired", true);
    expect(expired.tone).toBe("attention");
    expect(expired.subKey).toBe("Switch_SignIn_RejectedReason_Sub");
    expect(expired.reason).toBe("License expired");
    expect(dockResult("needsSignIn", "noStoredLogin", true).subKey).toBe("Switch_SignIn_NoStored_Sub");
    expect(dockResult("needsSignIn", "tokenRejected", true).subKey).toBe("Switch_SignIn_Rejected_Sub");
  });

  it("does not claim a sign-in when Steam was left closed", () => {
    expect(dockResult("none", "", false).titleKey).toBe("Switch_Done_TitleNoLaunch");
    expect(dockResult("none", "", false).subKey).toBe("Switch_Done_SubNoLaunch");
  });

  it("admits it cannot tell when Steam was launched but nothing is watching", () => {
    expect(dockResult("none", "", true).titleKey).toBe("Switch_SignIn_Unconfirmed_Title");
  });
});

describe("signInMark", () => {
  it("is never done before Steam says so", () => {
    expect(signInMark("none", false)).toBe("pending");
    expect(signInMark("waiting", true)).toBe("active");
    expect(signInMark("confirmed", true)).toBe("done");
    expect(signInMark("needsSignIn", true)).toBe("attention");
    expect(signInMark("unconfirmed", true)).toBe("attention");
  });
});

describe("heroLoginCopy", () => {
  it("stops saying 'signed in' while Steam is asking for a password", () => {
    expect(heroLoginCopy({ steamId64: "1", state: "needsSignIn" }, "1")).toEqual({ key: "Hero_NeedsPassword", warn: true });
    expect(heroLoginCopy({ steamId64: "1", state: "waiting" }, "1").key).toBe("Hero_SigningInAs");
    expect(heroLoginCopy({ steamId64: "1", state: "steamExited" }, "1")).toEqual({ key: "Hero_SignInUnconfirmed", warn: true });
    expect(heroLoginCopy({ steamId64: "1", state: "confirmed" }, "1").key).toBe("Hero_SignedInAs");
  });

  it("ignores an answer about another account", () => {
    expect(heroLoginCopy({ steamId64: "2", state: "needsSignIn" }, "1").key).toBe("Hero_SignedInAs");
  });
});

describe("asSignInStatus", () => {
  it("does not read an unknown state as success", () => {
    expect(asSignInStatus({ steamId64: "1", state: "somethingNew" }).state).toBe("unconfirmed");
    expect(asSignInStatus(null).state).toBe("none");
  });
});

describe("what the dock and hero may claim", () => {
  it("does not call an account with no saved login ready to switch back to", () => {
    expect(dockResult("confirmed", "", true).subKey).toBe("Switch_Done_Sub");
    expect(dockResult("confirmed", "", true, false).subKey).toBe("Switch_Done_SubNoSavedLogin");
    expect(dockResult("none", "", false, false).subKey).toBe("Switch_Done_SubNoLaunchNoSavedLogin");
  });

  it("says nothing about a previous login when no account was being left", () => {
    expect(dockResult("confirmed", "", true, null).subKey).toBe("Switch_Done_SubNoFrom");
    expect(dockResult("none", "", false, null).subKey).toBe("Switch_Done_SubNoLaunchNoFrom");
  });

  it("does not say Steam is asking for a password once Steam has closed", () => {
    expect(heroLoginCopy({ steamId64: "1", state: "needsSignIn" }, "1", true).key).toBe("Hero_NeedsPassword");
    expect(heroLoginCopy({ steamId64: "1", state: "needsSignIn" }, "1", false)).toEqual({ key: "Hero_SignInUnconfirmed", warn: true });
  });
});
