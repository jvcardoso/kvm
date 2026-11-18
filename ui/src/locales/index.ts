import enJSON from "./en.json";
import zhJSON from "./zh.json";
import ptJSON from "./pt.json";

export function getLocaleMessages(lang?: string): Record<string, string> {
  switch (lang) {
    case "zh":
      return zhJSON;
    case "pt":
      return ptJSON;
    case "en":
    default:
      return enJSON;
  }
}
