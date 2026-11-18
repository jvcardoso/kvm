import { KeyboardLayout, KeyCombo } from "../keyboardLayouts";

import { en_US } from "./en_US";

const name = "Português (Brasil - ABNT2)";
const isoCode = "pt-BR";

// Dead keys in ABNT2
const keyAcute: KeyCombo = { key: "BracketLeft" }; // ´ (dead) on SC 1A base
const keyGrave: KeyCombo = { key: "BracketLeft", shift: true }; // ` (dead) on SC 1A shift
const keyTrema: KeyCombo = { key: "Digit6", shift: true }; // ¨ (dead) on Digit6 shift
const keyTilde: KeyCombo = { key: "Quote" }; // ~ (dead) on SC 28 base
const keyHat: KeyCombo = { key: "Quote", shift: true }; // ^ (dead) on SC 28 shift

const chars: Record<string, KeyCombo> = {
  // Uppercase letters
  A: { key: "KeyA", shift: true },
  Á: { key: "KeyA", shift: true, accentKey: keyAcute },
  À: { key: "KeyA", shift: true, accentKey: keyGrave },
  Ä: { key: "KeyA", shift: true, accentKey: keyTrema },
  Ã: { key: "KeyA", shift: true, accentKey: keyTilde },
  Â: { key: "KeyA", shift: true, accentKey: keyHat },
  B: { key: "KeyB", shift: true },
  C: { key: "KeyC", shift: true },
  D: { key: "KeyD", shift: true },
  E: { key: "KeyE", shift: true },
  É: { key: "KeyE", shift: true, accentKey: keyAcute },
  È: { key: "KeyE", shift: true, accentKey: keyGrave },
  Ë: { key: "KeyE", shift: true, accentKey: keyTrema },
  Ê: { key: "KeyE", shift: true, accentKey: keyHat },
  F: { key: "KeyF", shift: true },
  G: { key: "KeyG", shift: true },
  H: { key: "KeyH", shift: true },
  I: { key: "KeyI", shift: true },
  Í: { key: "KeyI", shift: true, accentKey: keyAcute },
  Ì: { key: "KeyI", shift: true, accentKey: keyGrave },
  Ï: { key: "KeyI", shift: true, accentKey: keyTrema },
  Î: { key: "KeyI", shift: true, accentKey: keyHat },
  J: { key: "KeyJ", shift: true },
  K: { key: "KeyK", shift: true },
  L: { key: "KeyL", shift: true },
  M: { key: "KeyM", shift: true },
  N: { key: "KeyN", shift: true },
  Ñ: { key: "KeyN", shift: true, accentKey: keyTilde },
  O: { key: "KeyO", shift: true },
  Ó: { key: "KeyO", shift: true, accentKey: keyAcute },
  Ò: { key: "KeyO", shift: true, accentKey: keyGrave },
  Ö: { key: "KeyO", shift: true, accentKey: keyTrema },
  Õ: { key: "KeyO", shift: true, accentKey: keyTilde },
  Ô: { key: "KeyO", shift: true, accentKey: keyHat },
  P: { key: "KeyP", shift: true },
  Q: { key: "KeyQ", shift: true },
  R: { key: "KeyR", shift: true },
  S: { key: "KeyS", shift: true },
  T: { key: "KeyT", shift: true },
  U: { key: "KeyU", shift: true },
  Ú: { key: "KeyU", shift: true, accentKey: keyAcute },
  Ù: { key: "KeyU", shift: true, accentKey: keyGrave },
  Ü: { key: "KeyU", shift: true, accentKey: keyTrema },
  Û: { key: "KeyU", shift: true, accentKey: keyHat },
  V: { key: "KeyV", shift: true },
  W: { key: "KeyW", shift: true },
  X: { key: "KeyX", shift: true },
  Y: { key: "KeyY", shift: true },
  Z: { key: "KeyZ", shift: true },

  // Lowercase letters
  a: { key: "KeyA" },
  á: { key: "KeyA", accentKey: keyAcute },
  à: { key: "KeyA", accentKey: keyGrave },
  ä: { key: "KeyA", accentKey: keyTrema },
  ã: { key: "KeyA", accentKey: keyTilde },
  â: { key: "KeyA", accentKey: keyHat },
  b: { key: "KeyB" },
  c: { key: "KeyC" },
  d: { key: "KeyD" },
  e: { key: "KeyE" },
  é: { key: "KeyE", accentKey: keyAcute },
  è: { key: "KeyE", accentKey: keyGrave },
  ë: { key: "KeyE", accentKey: keyTrema },
  ê: { key: "KeyE", accentKey: keyHat },
  f: { key: "KeyF" },
  g: { key: "KeyG" },
  h: { key: "KeyH" },
  i: { key: "KeyI" },
  í: { key: "KeyI", accentKey: keyAcute },
  ì: { key: "KeyI", accentKey: keyGrave },
  ï: { key: "KeyI", accentKey: keyTrema },
  î: { key: "KeyI", accentKey: keyHat },
  j: { key: "KeyJ" },
  k: { key: "KeyK" },
  l: { key: "KeyL" },
  m: { key: "KeyM" },
  n: { key: "KeyN" },
  ñ: { key: "KeyN", accentKey: keyTilde },
  o: { key: "KeyO" },
  ó: { key: "KeyO", accentKey: keyAcute },
  ò: { key: "KeyO", accentKey: keyGrave },
  ö: { key: "KeyO", accentKey: keyTrema },
  õ: { key: "KeyO", accentKey: keyTilde },
  ô: { key: "KeyO", accentKey: keyHat },
  p: { key: "KeyP" },
  q: { key: "KeyQ" },
  r: { key: "KeyR" },
  s: { key: "KeyS" },
  t: { key: "KeyT" },
  u: { key: "KeyU" },
  ú: { key: "KeyU", accentKey: keyAcute },
  ù: { key: "KeyU", accentKey: keyGrave },
  ü: { key: "KeyU", accentKey: keyTrema },
  û: { key: "KeyU", accentKey: keyHat },
  v: { key: "KeyV" },
  w: { key: "KeyW" },
  x: { key: "KeyX" },
  y: { key: "KeyY" },
  z: { key: "KeyZ" },

  // Backquote (top-left): ' "
  "'": { key: "Backquote" },
  '"': { key: "Backquote", shift: true },

  // Number row
  1: { key: "Digit1" },
  "!": { key: "Digit1", shift: true },
  "¹": { key: "Digit1", altRight: true },
  2: { key: "Digit2" },
  "@": { key: "Digit2", shift: true },
  "²": { key: "Digit2", altRight: true },
  3: { key: "Digit3" },
  "#": { key: "Digit3", shift: true },
  "³": { key: "Digit3", altRight: true },
  4: { key: "Digit4" },
  $: { key: "Digit4", shift: true },
  "£": { key: "Digit4", altRight: true },
  5: { key: "Digit5" },
  "%": { key: "Digit5", shift: true },
  "¢": { key: "Digit5", altRight: true },
  6: { key: "Digit6" },
  "¨": { key: "Digit6", shift: true, deadKey: true },
  "¬": { key: "Digit6", altRight: true },
  7: { key: "Digit7" },
  "&": { key: "Digit7", shift: true },
  8: { key: "Digit8" },
  "*": { key: "Digit8", shift: true },
  9: { key: "Digit9" },
  "(": { key: "Digit9", shift: true },
  0: { key: "Digit0" },
  ")": { key: "Digit0", shift: true },

  // Minus / Equal
  "-": { key: "Minus" },
  _: { key: "Minus", shift: true },
  "=": { key: "Equal" },
  "+": { key: "Equal", shift: true },
  "§": { key: "Equal", altRight: true },

  // BracketLeft: ´ `
  "´": { key: "BracketLeft", deadKey: true },
  "`": { key: "BracketLeft", shift: true, deadKey: true },

  // BracketRight: [ { ª
  "[": { key: "BracketRight" },
  "{": { key: "BracketRight", shift: true },
  ª: { key: "BracketRight", altRight: true },

  // Semicolon: ç Ç
  ç: { key: "Semicolon" },
  Ç: { key: "Semicolon", shift: true },

  // Quote: ~ ^
  "~": { key: "Quote", deadKey: true },
  "^": { key: "Quote", shift: true, deadKey: true },

  // Backslash: ] } º
  "]": { key: "Backslash" },
  "}": { key: "Backslash", shift: true },
  º: { key: "Backslash", altRight: true },

  // IntlBackslash: \ |
  "\\": { key: "IntlBackslash" },
  "|": { key: "IntlBackslash", shift: true },

  // Comma, Period, Slash (; :)
  ",": { key: "Comma" },
  "<": { key: "Comma", shift: true },
  ".": { key: "Period" },
  ">": { key: "Period", shift: true },
  ";": { key: "Slash" },
  ":": { key: "Slash", shift: true },

  // Slash and Question mark via AltGr+Q / AltGr+W (standard on ABNT2 across Windows/Linux)
  "/": { key: "KeyQ", altRight: true },
  "?": { key: "KeyW", altRight: true },

  " ": { key: "Space" },
  "\n": { key: "Enter" },
  Enter: { key: "Enter" },
  Tab: { key: "Tab" },
};

export const pt_BR: KeyboardLayout = {
  isoCode: isoCode,
  name: name,
  chars: chars,
  keyDisplayMap: {
    ...en_US.keyDisplayMap,
    Backquote: "'", "(Backquote)": '"',
    "(Digit6)": "¨",
    BracketLeft: "´", "(BracketLeft)": "`",
    BracketRight: "[", "(BracketRight)": "{",
    Backslash: "]", "(Backslash)": "}",
    Semicolon: "ç", "(Semicolon)": "Ç",
    Quote: "~", "(Quote)": "^",
    Slash: ";", "(Slash)": ":",
    IntlBackslash: "\\", "(IntlBackslash)": "|",
  },
  modifierDisplayMap: en_US.modifierDisplayMap,
  virtualKeyboard: en_US.virtualKeyboard,
};
