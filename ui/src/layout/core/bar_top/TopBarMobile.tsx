import { LuClipboard, LuKeyboard, LuMaximize, LuPower, LuSettings, LuTerminal } from "react-icons/lu";
import { BsWrench } from "react-icons/bs";
import { useReactAt } from "i18n-auto-extractor/react";

import LogoLuckfox from "@assets/logo-luckfox.png";
import { useHidStore, useUiStore } from "@/hooks/stores";
import MacroTopBar from "@/layout/components_side/Macros/MacroTopBar";
import { BarsPinButton } from "@/layout/core/bar_autohide";
import { dark_bd_style, dark_bg_style_fun, dark_font_style, text_primary_color } from "@/layout/theme_color";
import { useTheme } from "@/layout/contexts/ThemeContext";

export default function MobileTopBar({ requestFullscreen }: { requestFullscreen: () => Promise<void> }) {
  const { $at } = useReactAt();
  const { isDark } = useTheme();
  const virtualKeyboard = useHidStore(state => state.isVirtualKeyboardEnabled);
  const setVirtualKeyboard = useHidStore(state => state.setVirtualKeyboardEnabled);
  const setDisableFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const toggleTopBarView = useUiStore(state => state.toggleTopBarView);
  const toggleSidebarView = useUiStore(state => state.toggleSidebarView);
  const setSidebarView = useUiStore(state => state.setSidebarView);
  const setTopBarView = useUiStore(state => state.setTopBarView);
  const sidebarView = useUiStore(state => state.sidebarView);
  const topBarView = useUiStore(state => state.topBarView);

  const buttons = [
    { label: $at("Settings"), icon: LuSettings, selected: topBarView === "SettingsModal", onClick: () => {
      setDisableFocusTrap(true);
      setVirtualKeyboard(false);
      toggleTopBarView("SettingsModal");
    } },
    { label: $at("Clipboard"), icon: LuClipboard, selected: topBarView === "ClipboardMobile", onClick: () => {
      setDisableFocusTrap(true);
      setVirtualKeyboard(false);
      toggleTopBarView("ClipboardMobile");
    } },
    { label: $at("Power"), icon: LuPower, selected: sidebarView === "PowerControl", onClick: () => {
      setDisableFocusTrap(true);
      setVirtualKeyboard(false);
      toggleSidebarView("PowerControl");
    } },
    { label: $at("Terminal"), icon: LuTerminal, selected: sidebarView === "TerminalTabsMobile", onClick: () => {
      setDisableFocusTrap(true);
      setVirtualKeyboard(false);
      toggleSidebarView("TerminalTabsMobile");
    } },
    { label: $at("Virtual keyboard"), icon: LuKeyboard, selected: virtualKeyboard, onClick: () => {
      setSidebarView(null);
      setTopBarView(null);
      setVirtualKeyboard(!virtualKeyboard);
    } },
    { label: $at("Fullscreen"), icon: LuMaximize, selected: false, onClick: () => {
      setSidebarView(null);
      setTopBarView(null);
      setVirtualKeyboard(false);
      void requestFullscreen();
    } },
  ];

  return (
    <div className={`w-full ${dark_bg_style_fun(isDark)}`}>
      <div className={`grid min-h-11 grid-cols-7 border-y ${dark_bd_style}`}>
        <a href="https://wiki.luckfox.com/intro/" target="_blank" rel="noopener noreferrer"
          className="flex min-h-11 items-center justify-center" aria-label="Luckfox">
          <img src={LogoLuckfox} alt="Luckfox" className="h-5" />
        </a>
        {buttons.map(({ label, icon: Icon, selected, onClick }) => (
          <button key={label} type="button" title={label} aria-label={label} aria-pressed={selected}
            onClick={onClick}
            className={`flex min-h-11 items-center justify-center active:bg-black/10 dark:active:bg-white/10 ${selected ? text_primary_color : dark_font_style}`}>
            <Icon size={21} />
          </button>
        ))}
      </div>
      {topBarView === null && sidebarView === null && !virtualKeyboard && (
        <div className={`flex h-11 w-full items-center border-b ${dark_bd_style}`}>
          <div className="min-w-0 flex-1"><MacroTopBar /></div>
          <ToolsToggleButton />
          <BarsPinButton fontSize={17} />
        </div>
      )}
    </div>
  );
}

function ToolsToggleButton() {
  const { $at } = useReactAt();
  const hidden = useUiStore(state => state.mobileToolsHidden);
  const setHidden = useUiStore(state => state.setMobileToolsHidden);
  const label = hidden ? $at("Show tools") : $at("Hide tools");
  return (
    <button type="button" title={label} aria-label={label} aria-pressed={!hidden}
      onClick={() => setHidden(!hidden)}
      className={`flex h-11 w-11 shrink-0 items-center justify-center ${hidden ? dark_font_style : text_primary_color}`}>
      <BsWrench size={18} />
    </button>
  );
}
