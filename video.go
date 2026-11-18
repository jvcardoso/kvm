package kvm

import (
	"encoding/json"
	"sync/atomic"
)

// max frame size for 1080p video, specified in mpp venc setting
const maxFrameSize = 1920 * 1080 / 2

func writeCtrlAction(action string) error {
	actionMessage := map[string]string{
		"action": action,
	}
	jsonMessage, err := json.Marshal(actionMessage)
	if err != nil {
		return err
	}
	err = WriteCtrlMessage(jsonMessage)
	return err
}

type VideoInputState struct {
	Ready          bool    `json:"ready"`
	Error          string  `json:"error,omitempty"` // no_signal, no_lock, out_of_range
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	FramePerSecond float64 `json:"frame_per_second"`
}

var lastVideoState atomic.Pointer[VideoInputState]

func triggerVideoStateUpdate() {
	go func() {
		videoState, _ := rpcGetVideoState()
		writeJSONRPCEvent("videoInputState", videoState, currentSession.Load())
	}()
}

func HandleVideoStateMessage(event CtrlResponse) {
	videoState := VideoInputState{}
	err := json.Unmarshal(event.Data, &videoState)
	if err != nil {
		logger.Warn().Err(err).Msg("Error parsing video state json")
		return
	}
	lastVideoState.Store(&videoState)
	triggerVideoStateUpdate()
	requestDisplayUpdate(true)
}

func rpcGetVideoState() (VideoInputState, error) {
	if state := lastVideoState.Load(); state != nil {
		return *state, nil
	}
	return VideoInputState{}, nil
}

func setForceHpd() error {
	err := rpcSetForceHpd(configSnapshot().ForceHpd)
	return err
}

func setNpuAppStatus() error {
	err := rpcSetNpuAppStatus(configSnapshot().NpuAppEnabled)
	return err
}
