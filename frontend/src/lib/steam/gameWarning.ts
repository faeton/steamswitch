type Translate = (key: string, vars?: Record<string, string>) => string;

/**
 * The question asked before a switch that would close a running game. Closing Steam closes the
 * game with it, and a double-click is an easy accident to make in the middle of a match.
 */
export function gameRunningConfirm(game: string, t: Translate) {
  return {
    title: t("Switch_GameRunning_Title", { game }),
    body: t("Switch_GameRunning_Body", { game }),
    positiveLabel: t("Switch_GameRunning_Confirm", { game }),
    negativeLabel: t("Button_Cancel"),
    // "yesno" is the style that draws the negative button; "okcancel" leaves only the X.
    style: "yesno" as const,
    destructive: true,
  };
}
