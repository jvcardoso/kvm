import { LuHardDrive, LuKeyboard, LuMonitor, LuMouse, LuNetwork, LuUsb, LuVideo } from "react-icons/lu";
import { useReactAt } from "i18n-auto-extractor/react";

import { AvailableSidebarViews, useHidStore, useUiStore, useVideoStore, useVpnStore } from "@/hooks/stores";
import { remoteIconColor } from "@/layout/core/bar_bottom/RemoteStatusPanel";
import { dark_bd_style, dark_bg_style, dark_font_style, text_primary_color } from "@/layout/theme_color";

export default function BottomBarMobile() {
  const { $at } = useReactAt();
  const sidebarView = useUiStore(state => state.sidebarView);
  const toggleSidebarView = useUiStore(state => state.toggleSidebarView);
  const setDisableFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const virtualKeyboard = useHidStore(state => state.isVirtualKeyboardEnabled);
  const usbState = useHidStore(state => state.usbState);
  const emulation = useHidStore(state => state.usbEmulationEnabled);
  const hdmi = useVideoStore(state => state.hdmiState);
  const tailscale = useVpnStore(state => state.tailScaleConnectionState);
  const zerotier = useVpnStore(state => state.zeroTierConnectionState);
  const tunnels = useVpnStore(state => state.tunnelEntries);

  const tabs: { view: AvailableSidebarViews; label: string; icon: typeof LuKeyboard; color?: string }[] = [
    { view: "KeyboardPanel", label: $at("Keyboard"), icon: LuKeyboard },
    { view: "MousePanel", label: $at("Mouse"), icon: LuMouse },
    { view: "VirtualMedia", label: $at("Media"), icon: LuHardDrive },
    { view: "SettingsVideo", label: $at("Video"), icon: LuVideo },
    { view: "HdmiStatus", label: "HDMI", icon: LuMonitor, color: hdmi === "ready" ? "#16a34a" : undefined },
    { view: "UsbEpModeSelect", label: "USB", icon: LuUsb, color: !emulation ? "#d97706" : usbState === "configured" ? "#16a34a" : undefined },
    { view: "RemoteStatus", label: $at("Remote"), icon: LuNetwork, color: remoteIconColor(tailscale, zerotier, tunnels) },
  ];

  if (virtualKeyboard) return null;
  return (
    <nav aria-label={$at("KVM controls")} className={`grid w-full grid-cols-7 border-t ${dark_bd_style} ${dark_bg_style}`}>
      {tabs.map(({ view, label, icon: Icon, color }) => (
        <button key={view} type="button" title={label} aria-label={label} aria-pressed={sidebarView === view}
          onClick={() => { setDisableFocusTrap(true); toggleSidebarView(view); }}
          className={`flex min-h-14 min-w-0 flex-col items-center justify-center gap-1 px-0.5 active:bg-black/10 dark:active:bg-white/10 ${sidebarView === view ? text_primary_color : dark_font_style}`}>
          <Icon size={20} style={{ color }} />
          <span className="max-w-full truncate text-[10px] leading-tight">{label}</span>
        </button>
      ))}
    </nav>
  );
}
