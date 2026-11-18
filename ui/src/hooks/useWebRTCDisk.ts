import { useEffect } from "react";
import { useMountMediaStore, useRTCStore } from "@/hooks/stores";

export function useWebRTCDisk() {
  const channel = useRTCStore(state => state.diskChannel);
  useEffect(() => {
    if (!channel) return;
    let disposed = false;

    const handleRequest = async (event: MessageEvent) => {
      const media = useMountMediaStore.getState().browserMedia;
      if (!media || typeof event.data !== "string") return;
      try {
        const { id, generation, start, end } = JSON.parse(event.data);
        if (generation !== media.generation || typeof id !== "string" || !/^\d{1,20}$/.test(id) ||
            !Number.isSafeInteger(start) || !Number.isSafeInteger(end) ||
            start < 0 || end <= start || end > media.file.size) return;

        const payload = await media.file.slice(start, end).arrayBuffer();
        if (disposed || channel.readyState !== "open" ||
            useMountMediaStore.getState().browserMedia !== media) return;

        const packet = new Uint8Array(40 + payload.byteLength);
        const header = new DataView(packet.buffer);
        header.setBigUint64(0, BigInt(id));
        for (let i = 0; i < 16; i++) {
          packet[8 + i] = parseInt(media.generation.slice(i * 2, i * 2 + 2), 16);
        }
        header.setBigUint64(24, BigInt(start));
        header.setBigUint64(32, BigInt(payload.byteLength));
        packet.set(new Uint8Array(payload), 40);
        channel.send(packet);
      } catch (error) {
        console.warn("WebRTC disk request failed", error);
      }
    };

    channel.addEventListener("message", handleRequest);
    return () => {
      disposed = true;
      channel.removeEventListener("message", handleRequest);
    };
  }, [channel]);
}
