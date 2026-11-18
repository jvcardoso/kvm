package kvm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

var (
	audioCmd     *exec.Cmd
	audioCmdLock = &sync.Mutex{}
)

var audioSocketConn net.Conn

var (
	audioOngoingRequests     = make(map[int32]chan *CtrlResponse)
	audioOngoingRequestsLock = &sync.Mutex{}
	audioConnWriteLock       = make(nativeControlLock, 1)
)

var audioActionGate = make(chan struct{}, 1)

const audioCtrlTimeout = 5 * time.Second

func CallAudioCtrlAction(action string, params map[string]interface{}) (*CtrlResponse, error) {
	deadline := time.Now().Add(audioCtrlTimeout)
	release, err := acquireNativeAction(audioActionGate, deadline)
	if err != nil {
		return nil, err
	}
	defer release()

	responseChan := make(chan *CtrlResponse, 1)
	ctrlSeq := nextSeq()
	audioOngoingRequestsLock.Lock()
	audioOngoingRequests[ctrlSeq] = responseChan
	audioOngoingRequestsLock.Unlock()

	ctrlAction := CtrlAction{
		Action: action,
		Seq:    ctrlSeq,
		Params: params,
	}

	jsonData, err := json.Marshal(ctrlAction)
	if err != nil {
		dropAudioResponseChan(ctrlSeq)
		return nil, fmt.Errorf("error marshaling ctrl action: %w", err)
	}

	scopedLogger := audioLogger.With().
		Str("action", ctrlAction.Action).
		Interface("params", ctrlAction.Params).Logger()

	scopedLogger.Debug().Msg("sending audio ctrl action")

	err = writeAudioCtrlMessageUntil(jsonData, deadline)
	if err != nil {
		dropAudioResponseChan(ctrlSeq)
		return nil, fmt.Errorf("writing native control message: %w", err)
	}

	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case response := <-responseChan:
		dropAudioResponseChan(ctrlSeq)
		if response.Error != "" {
			return nil, ErrorfL(
				&scopedLogger,
				"error audio response: %s",
				errors.New(response.Error),
			)
		}
		return response, nil
	case <-timer.C:
		dropAudioResponseChan(ctrlSeq)
		return nil, fmt.Errorf("waiting for native response: %w", os.ErrDeadlineExceeded)
	}
}

func dropAudioResponseChan(ctrlSeq int32) {
	audioOngoingRequestsLock.Lock()
	delete(audioOngoingRequests, ctrlSeq)
	audioOngoingRequestsLock.Unlock()
}

func deliverAudioResponse(ctrlSeq int32, response *CtrlResponse) {
	audioOngoingRequestsLock.Lock()
	defer audioOngoingRequestsLock.Unlock()

	responseChan, ok := audioOngoingRequests[ctrlSeq]
	if !ok {
		return
	}

	select {
	case responseChan <- response:
	default:
	}
}

func WriteAudioCtrlMessage(message []byte) error {
	return writeAudioCtrlMessageUntil(message, time.Now().Add(audioCtrlTimeout))
}

func writeAudioCtrlMessageUntil(message []byte, deadline time.Time) error {
	release, err := acquireNativeAction(audioConnWriteLock, deadline)
	if err != nil {
		return err
	}
	defer release()
	return writeNativeControl(audioSocketConn, message, deadline)
}

var audioCtrlSocketListener net.Listener

var audioCtrlClientConnected = make(chan struct{})

func waitAudioCtrlClientConnected() {
	<-audioCtrlClientConnected
}

func StartAudioSocketServer(socketPath string, handleClient func(net.Conn), isCtrl bool) net.Listener {
	scopedLogger := audioLogger.With().
		Str("socket_path", socketPath).
		Logger()

	// Remove the socket file if it already exists
	if _, err := os.Stat(socketPath); err == nil {
		if err := os.Remove(socketPath); err != nil {
			scopedLogger.Warn().Err(err).Msg("failed to remove existing socket file")
			os.Exit(1)
		}
	}

	listener, err := net.Listen("unixpacket", socketPath)
	if err != nil {
		scopedLogger.Warn().Err(err).Msg("failed to start server")
		os.Exit(1)
	}

	scopedLogger.Info().Msg("server listening")

	go func() {
		for {
			conn, err := listener.Accept()

			if err != nil {
				scopedLogger.Warn().Err(err).Msg("failed to accept socket")
				continue
			}
			if isCtrl {
				// check if the channel is closed
				select {
				case <-audioCtrlClientConnected:
					scopedLogger.Debug().Msg("audio ctrl client reconnected")
				default:
					close(audioCtrlClientConnected)
					scopedLogger.Debug().Msg("first audio ctrl socket client connected")
				}
			}

			//conn.Write([]byte("[handleAudioCtrlClient]audio sock test"))
			go handleClient(conn)
		}
	}()

	return listener
}

func StartAudioCtrlSocketServer() {
	audioCtrlSocketListener = StartAudioSocketServer("/var/run/kvm_audio.sock", handleAudioCtrlClient, true)
	audioLogger.Debug().Msg("audio ctrl sock started")
}

func handleAudioCtrlClient(conn net.Conn) {
	defer conn.Close()

	scopedLogger := audioLogger.With().
		Str("addr", conn.RemoteAddr().String()).
		Str("type", "audio_ctrl").
		Logger()

	scopedLogger.Info().Msg("audio socket client connected")
	audioConnWriteLock.Lock()
	if audioSocketConn != nil {
		scopedLogger.Debug().Msg("closing existing audio socket connection")
		audioSocketConn.Close()
	}
	audioSocketConn = conn
	audioConnWriteLock.Unlock()

	readBuf := make([]byte, 4096)
	for {
		n, err := conn.Read(readBuf)
		if err != nil {
			scopedLogger.Warn().Err(err).Msg("error reading from audio sock")
			break
		}
		readMsg := string(readBuf[:n])

		audioResp := CtrlResponse{}
		err = json.Unmarshal([]byte(readMsg), &audioResp)
		if err != nil {
			scopedLogger.Warn().Err(err).Str("data", readMsg).Msg("error parsing audio sock msg")
			continue
		}
		scopedLogger.Trace().Interface("data", audioResp).Msg("audio sock msg")

		if audioResp.Seq != 0 {
			response := audioResp
			deliverAudioResponse(response.Seq, &response)
		}
		switch audioResp.Event {
		case "audio_input_state":
			HandleAudioStateMessage(audioResp)
		}
	}

	scopedLogger.Debug().Msg("audio sock disconnected")
}

func startAudioBinaryWithLock(binaryPath string) (*exec.Cmd, error) {
	audioCmdLock.Lock()
	defer audioCmdLock.Unlock()

	cmd, err := startAudioBinary(binaryPath)
	if err != nil {
		return nil, err
	}
	audioCmd = cmd
	return cmd, nil
}

func restartAudioBinary(binaryPath string) error {
	time.Sleep(10 * time.Second)
	// restart the binary
	audioLogger.Info().Msg("restarting audio_video binary")
	cmd, err := startAudioBinary(binaryPath)
	if err != nil {
		audioLogger.Warn().Err(err).Msg("failed to restart binary")
	}
	audioCmd = cmd
	return err
}

func superviseAudioBinary(binaryPath string) error {
	audioCmdLock.Lock()
	defer audioCmdLock.Unlock()

	if audioCmd == nil || audioCmd.Process == nil {
		return restartAudioBinary(binaryPath)
	}

	err := audioCmd.Wait()

	if err == nil {
		audioLogger.Info().Err(err).Msg("kvm_audio binary exited with no error")
	} else if exiterr, ok := err.(*exec.ExitError); ok {
		audioLogger.Warn().Int("exit_code", exiterr.ExitCode()).Msg("kvm_audio binary exited with error")
	} else {
		audioLogger.Warn().Err(err).Msg("kvm_audio binary exited with unknown error")
	}

	return restartAudioBinary(binaryPath)
}

func ExtractAndRunAudioBin() error {
	binaryPath := "/userdata/picokvm/bin/kvm_audio"

	// Make the binary executable
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}
	// Run the binary in the background
	cmd, err := startAudioBinaryWithLock(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to start binary: %w", err)
	}

	// check if the binary is still running every 10 seconds
	go func() {
		for {
			select {
			case <-appCtx.Done():
				audioLogger.Info().Msg("stopping audio binary supervisor")
				return
			default:
				err := superviseAudioBinary(binaryPath)
				if err != nil {
					audioLogger.Warn().Err(err).Msg("failed to supervise audio binary")
					time.Sleep(1 * time.Second) // Add a short delay to prevent rapid successive calls
				}
			}
		}
	}()

	go func() {
		<-appCtx.Done()
		audioLogger.Info().Int("pid", cmd.Process.Pid).Msg("killing process")
		err := cmd.Process.Kill()
		if err != nil {
			audioLogger.Warn().Err(err).Msg("failed to kill process")
			return
		}
	}()

	audioLogger.Info().Int("pid", cmd.Process.Pid).Msg("kvm_audio binary started")

	return nil
}
