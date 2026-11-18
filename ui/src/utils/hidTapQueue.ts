// A tap is a complete press/release pair. Keep pairs in order even when the
// transport needs an acknowledgement or a second tap arrives before release.
export function createHidTapQueue<T>({
  send,
  onError,
  holdMs = 35,
  gapMs = 15,
}: {
  send: (report: T) => void | Promise<void>;
  onError: (error: unknown) => void;
  holdMs?: number;
  gapMs?: number;
}) {
  const pending: { press: T; release: T; generation: number }[] = [];
  let generation = 0;
  let running = false;
  let interruptWait: (() => void) | undefined;

  const wait = (ms: number) => new Promise<void>(resolve => {
    const finish = () => {
      clearTimeout(timer);
      interruptWait = undefined;
      resolve();
    };
    const timer = setTimeout(finish, ms);
    interruptWait = finish;
  });

  const pump = async () => {
    if (running) return;
    running = true;
    try {
      while (pending.length > 0) {
        const tap = pending.shift()!;
        if (tap.generation !== generation) continue;
        let failure: unknown;
        try {
          await send(tap.press);
          if (tap.generation === generation) await wait(holdMs);
        } catch (error) {
          failure = error;
        } finally {
          // Also release after cancellation or a failed acknowledgement. A new
          // tap cannot start until this release has finished on the same sender.
          try { await send(tap.release); } catch (error) { failure = error; }
        }
        if (failure !== undefined) {
          if (tap.generation !== generation) continue;
          pending.length = 0;
          onError(failure);
          break;
        }
        if (tap.generation === generation) await wait(gapMs);
      }
    } finally {
      running = false;
    }
  };

  return {
    enqueue(press: T, release: T) {
      if (pending.length >= 256) {
        onError(new Error("Keyboard input queue is full"));
        return;
      }
      pending.push({ press, release, generation });
      void pump();
    },
    cancel() {
      generation++;
      pending.length = 0;
      interruptWait?.();
    },
  };
}
