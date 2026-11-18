import { useCallback, useEffect, useRef } from "react";

import {
  BUTTON_LEFT, BUTTON_RIGHT, LONG_PRESS_MS, LONG_PRESS_VIBRATION_MS,
  createTouchGestureState, isBeyondDeadZone, isWithinDoubleTapWindow,
} from "@/utils/touchGesture";

export type TouchEmit = (clientX: number, clientY: number, buttons: number) => void;

interface Options {
  emit: TouchEmit;
  tapToClick: boolean;
}

export interface TouchGestureHandlers {
  handleDown: (event: PointerEvent) => void;
  handleMove: (event: PointerEvent) => void;
  handleUp: (event: PointerEvent) => void;
  reset: () => void;
  cancel: () => void;
}

// One finger moves the cursor; a completed tap clicks. Double-tap-and-hold
// drags, and a long press is exclusively a right click. Deciding at pointerup
// prevents the first finger of a pinch or scroll from clicking the host.
export const useTouchGestures = ({ emit, tapToClick }: Options): TouchGestureHandlers => {
  const gesture = useRef(createTouchGestureState());
  const longPressTimer = useRef<number | null>(null);
  const lastTapUpAt = useRef(0);
  const lastTapPoint = useRef<{ x: number; y: number } | null>(null);
  const emitRef = useRef(emit);
  const tapToClickRef = useRef(tapToClick);
  useEffect(() => { emitRef.current = emit; }, [emit]);
  useEffect(() => { tapToClickRef.current = tapToClick; }, [tapToClick]);

  const clearLongPress = useCallback(() => {
    if (longPressTimer.current !== null) window.clearTimeout(longPressTimer.current);
    longPressTimer.current = null;
  }, []);

  const release = useCallback((x: number, y: number) => {
    const state = gesture.current;
    if (!state.leftButtonActive && !state.rightButtonActive) return;
    state.leftButtonActive = false;
    state.rightButtonActive = false;
    emitRef.current(x, y, 0);
  }, []);

  const reset = useCallback(() => {
    // A multi-touch takeover has already released the mouse in the caller.
    clearLongPress();
    gesture.current = createTouchGestureState();
    lastTapUpAt.current = 0;
    lastTapPoint.current = null;
  }, [clearLongPress]);

  const cancel = useCallback(() => {
    const state = gesture.current;
    release(state.x, state.y);
    reset();
  }, [release, reset]);

  useEffect(() => {
    const onVisibility = () => { if (document.hidden) cancel(); };
    window.addEventListener("blur", cancel);
    window.addEventListener("resize", cancel);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      window.removeEventListener("blur", cancel);
      window.removeEventListener("resize", cancel);
      document.removeEventListener("visibilitychange", onVisibility);
      cancel();
    };
  }, [cancel]);

  const handleDown = useCallback((event: PointerEvent) => {
    clearLongPress();
    release(gesture.current.x, gesture.current.y);
    const previous = lastTapPoint.current;
    const startedAt = event.timeStamp;
    gesture.current = {
      ...createTouchGestureState(),
      anchorX: event.clientX, anchorY: event.clientY,
      x: event.clientX, y: event.clientY, startedAt,
      doubleTap: previous !== null && isWithinDoubleTapWindow(lastTapUpAt.current, startedAt) &&
        !isBeyondDeadZone(previous.x, previous.y, event.clientX, event.clientY),
    };
    longPressTimer.current = window.setTimeout(() => {
      longPressTimer.current = null;
      const state = gesture.current;
      if (state.startedAt !== startedAt || state.moved) return;
      state.rightButtonActive = true;
      emitRef.current(state.anchorX, state.anchorY, BUTTON_RIGHT);
      navigator.vibrate?.(LONG_PRESS_VIBRATION_MS);
    }, LONG_PRESS_MS);
  }, [clearLongPress, release]);

  const handleMove = useCallback((event: PointerEvent) => {
    const state = gesture.current;
    if (state.startedAt <= 0) return;
    if (!state.moved) {
      if (!isBeyondDeadZone(state.anchorX, state.anchorY, event.clientX, event.clientY)) return;
      state.moved = true;
      clearLongPress();
      if (state.doubleTap && !state.rightButtonActive) {
        state.leftButtonActive = true;
        emitRef.current(state.anchorX, state.anchorY, BUTTON_LEFT);
      }
    }
    state.x = event.clientX;
    state.y = event.clientY;
    const buttons = state.rightButtonActive ? BUTTON_RIGHT : state.leftButtonActive ? BUTTON_LEFT : 0;
    emitRef.current(state.x, state.y, buttons);
  }, [clearLongPress]);

  const handleUp = useCallback((event: PointerEvent) => {
    const state = gesture.current;
    if (state.startedAt <= 0) return;
    clearLongPress();
    const cancelled = event.type === "pointercancel";
    const wasRightClick = state.rightButtonActive;
    const x = cancelled ? state.x : event.clientX;
    const y = cancelled ? state.y : event.clientY;
    if (state.leftButtonActive || state.rightButtonActive) {
      release(x, y);
    } else if (!cancelled && !state.moved && tapToClickRef.current) {
      emitRef.current(x, y, BUTTON_LEFT);
      emitRef.current(x, y, 0);
    } else if (state.moved) {
      emitRef.current(x, y, 0);
    }
    if (!cancelled && !state.moved && !wasRightClick && tapToClickRef.current) {
      lastTapUpAt.current = event.timeStamp;
      lastTapPoint.current = { x, y };
    } else {
      lastTapUpAt.current = 0;
      lastTapPoint.current = null;
    }
    gesture.current = createTouchGestureState();
  }, [clearLongPress, release]);

  return { handleDown, handleMove, handleUp, reset, cancel };
};
