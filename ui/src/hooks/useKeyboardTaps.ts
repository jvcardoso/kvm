import { useCallback, useEffect, useRef } from "react";
import { useReactAt } from "i18n-auto-extractor/react";

import { useHidStore, useRTCStore, useSettingsStore } from "@/hooks/stores";
import { createHidTapQueue } from "@/utils/hidTapQueue";
import notifications from "@/notifications";

type Sender = (keys: number[], modifiers: number[], forceRelease?: boolean,
  onComplete?: (error?: Error) => void) => boolean;

export function useKeyboardTaps(send: Sender, enabled: boolean) {
  const { $at } = useReactAt();
  const errorTextRef = useRef($at("Failed to send keyboard input"));
  errorTextRef.current = $at("Failed to send keyboard input");
  const usbReady = useHidStore(state => state.usbState === "configured" &&
    state.usbEmulationEnabled && state.usbKeyboardEnabled && !state.isReinitializingGadget);
  const forceHttp = useSettingsStore(state => state.forceHttp);
  const rpcChannel = useRTCStore(state => state.rpcDataChannel);
  const hidChannel = useRTCStore(state => state.rpcHidChannel);
  const available = enabled && usbReady && (forceHttp || rpcChannel?.readyState === "open");
  const queue = useRef<ReturnType<typeof createHidTapQueue<() => Promise<void>>> | null>(null);
  if (!queue.current) {
    queue.current = createHidTapQueue({
      send: report => report(),
      onError: () => notifications.error(errorTextRef.current),
    });
  }

  useEffect(() => {
    const cancel = () => queue.current?.cancel();
    const onVisibility = () => { if (document.hidden) cancel(); };
    window.addEventListener("blur", cancel);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      window.removeEventListener("blur", cancel);
      document.removeEventListener("visibilitychange", onVisibility);
      cancel();
    };
  }, []);

  useEffect(() => {
    if (!available) queue.current?.cancel();
    return () => queue.current?.cancel();
  }, [available, forceHttp, rpcChannel, hidChannel]);

  return useCallback((keys: number[], modifiers: number[]) => {
    if (!available) return;
    // Capture this sender for both halves. Reconnecting must never redirect a
    // late release from an old session into a new session's keyboard state.
    const report = (reportKeys: number[], reportModifiers: number[], release: boolean) => () =>
      new Promise<void>((resolve, reject) => {
        const finish = (error?: Error) => {
          clearTimeout(timer);
          rpcChannel?.removeEventListener("close", onClose);
          if (error) reject(error);
          else resolve();
        };
        const onClose = () => finish(new Error("Keyboard connection closed"));
        const timer = window.setTimeout(() => finish(new Error("Keyboard response timed out")), 5000);
        rpcChannel?.addEventListener("close", onClose);
        send(reportKeys, reportModifiers, release, finish);
      });
    queue.current?.enqueue(report([...keys], [...modifiers], false), report([], [], true));
  }, [available, send, rpcChannel]);
}
