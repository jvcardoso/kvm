import { useCallback, useEffect, useState } from "react";
import { useResizeObserver } from "usehooks-ts";
import JMuxer from "jmuxer";
import { useReactAt } from "i18n-auto-extractor/react";
import notifications from "@/notifications";

import { useRTCStore, useVideoStore, useSettingsStore } from "@/hooks/stores";

export const useVideoStream = (
  videoElm: React.RefObject<HTMLVideoElement>,
  audioElm: React.RefObject<HTMLAudioElement>
) => {
  const [isPlaying, setIsPlaying] = useState(false);
  const mediaStream = useRTCStore(state => state.mediaStream);
  const peerConnectionState = useRTCStore(state => state.peerConnectionState);
  const setPeerConnectionState = useRTCStore(state => state.setPeerConnectionState);
  const forceHttp = useSettingsStore(state => state.forceHttp);
  const { setClientSize: setVideoClientSize, setSize: setVideoSize } = useVideoStore();
  const { $at } = useReactAt();
  const unsupportedHevc = $at("This browser cannot play H.265. Select H.264 in Video settings.");

  const updateVideoSizeStore = useCallback((videoElm: HTMLVideoElement) => {
    setVideoClientSize(videoElm.clientWidth, videoElm.clientHeight);
    setVideoSize(videoElm.videoWidth, videoElm.videoHeight);
  }, [setVideoClientSize, setVideoSize]);

  const markAsPlaying = useCallback(() => {
    setIsPlaying(true);
    if (videoElm.current) {
      updateVideoSizeStore(videoElm.current);
    }
  }, [updateVideoSizeStore, videoElm]);

  const onVideoPlaying = useCallback(() => {
    markAsPlaying();
  }, [markAsPlaying]);

  const handlePlayClick = useCallback(() => {
    videoElm.current?.play();
  }, [videoElm]);

  const videoKeyUpHandler = useCallback((e: KeyboardEvent) => {
    if (!videoElm.current) return;
    if (e.code === "Space" && videoElm.current.paused) {
      videoElm.current.play();
    }
  }, [videoElm]);

  useResizeObserver({
    ref: videoElm as React.RefObject<HTMLElement>,
    onResize: ({ width, height }) => {
      if (width && height && videoElm.current) {
        updateVideoSizeStore(videoElm.current);
      }
    },
  });

  const addStreamToVideoElm = useCallback((mediaStream: MediaStream) => {
    if (!videoElm.current) return;
    videoElm.current.srcObject = mediaStream;
    updateVideoSizeStore(videoElm.current);
  }, [updateVideoSizeStore, videoElm]);

  const addStreamToAudioElm = useCallback((mediaStream: MediaStream) => {
    if (!audioElm.current) return;
    audioElm.current.srcObject = mediaStream;
  }, [audioElm]);

  const setupVideoEventListeners = useCallback(() => {
    const videoElmRefValue = videoElm.current;
    if (!videoElmRefValue) return;

    const abortController = new AbortController();
    const signal = abortController.signal;

    videoElmRefValue.addEventListener("keyup", videoKeyUpHandler, { signal });
    videoElmRefValue.addEventListener("playing", onVideoPlaying, { signal });
    videoElmRefValue.addEventListener("play", onVideoPlaying, { signal });

    return () => abortController.abort();
  }, [onVideoPlaying, videoKeyUpHandler, videoElm]);

  useEffect(() => {
    if (videoElm.current) updateVideoSizeStore(videoElm.current);
  }, [updateVideoSizeStore]);

  useEffect(() => {
    if (!mediaStream || forceHttp) {
      if (forceHttp) {
        if (videoElm.current?.srcObject) {
          videoElm.current.srcObject = null;
        }
        if (audioElm.current?.srcObject) {
          audioElm.current.srcObject = null;
        }
      }
      return;
    }
    addStreamToVideoElm(mediaStream);
    addStreamToAudioElm(mediaStream);
  }, [mediaStream, addStreamToVideoElm, addStreamToAudioElm, forceHttp, videoElm, audioElm]);

  useEffect(() => {
    const video = videoElm.current;
    if (!forceHttp || !video) return;

    const controller = new AbortController();
    let watchdog: ReturnType<typeof setTimeout>;
    const fail = () => {
      setIsPlaying(false);
      setPeerConnectionState("failed");
      controller.abort();
    };
    const restartWatchdog = () => {
      clearTimeout(watchdog);
      watchdog = setTimeout(fail, 10_000);
    };

    const fetchStream = async () => {
      while (!controller.signal.aborted) {
        setIsPlaying(false);
        setPeerConnectionState("connecting");
        restartWatchdog();
        const response = await fetch("/video/stream", { signal: controller.signal });
        if (!response.ok || !response.body) {
          throw new Error(`HTTP video stream returned ${response.status}`);
        }
        if (controller.signal.aborted) return;

        const codec = response.headers.get("X-Video-Codec");
        const isHEVC = codec === "h265" || (!codec && response.headers.get("Content-Type") === "video/x-h265");
        const fps = Number(response.headers.get("X-Video-FPS"));
        // JMuxer 2.1 supports these options at runtime, but its type package omits them.
        const options: ConstructorParameters<typeof JMuxer>[0] & {
          videoCodec: "H264" | "H265";
          onUnsupportedCodec: () => void;
        } = {
          node: video,
          mode: "video",
          videoCodec: isHEVC ? "H265" : "H264",
          flushingTime: 10,
          fps: fps > 0 && Number.isFinite(fps) ? fps : 60,
          onUnsupportedCodec: () => {
            if (controller.signal.aborted) return;
            if (isHEVC) notifications.error(unsupportedHevc);
            fail();
          },
        };
        const muxer = new JMuxer(options);

        const reader = response.body.getReader();
        let receivedData = false;
        try {
          while (!controller.signal.aborted) {
            const { done, value } = await reader.read();
            if (done || controller.signal.aborted) break;
            if (value.length === 0) continue;
            restartWatchdog();
            if (!receivedData) {
              receivedData = true;
              setPeerConnectionState("connected");
            }
            muxer.feed({ video: value });
          }
        } finally {
          reader.releaseLock();
          muxer.destroy();
        }
        // The server ends the current response when the device changes codec.
        // Fetch fresh headers and create a muxer for the new response.
        if (!receivedData && !controller.signal.aborted) {
          throw new Error("HTTP video stream closed before sending data");
        }
      }
    };

    void fetchStream().catch(error => {
      if (controller.signal.aborted) return;
      console.error("HTTP video stream failed", error);
      fail();
    }).finally(() => clearTimeout(watchdog));

    return () => {
      clearTimeout(watchdog);
      controller.abort();
    };
  }, [forceHttp, videoElm, setPeerConnectionState, unsupportedHevc]);

  return {
    isPlaying,
    peerConnectionState,
    onVideoPlaying,
    handlePlayClick,
    setupVideoEventListeners,
  };
};
