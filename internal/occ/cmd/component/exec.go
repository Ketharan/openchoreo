// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package component

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/websocket"
	"golang.org/x/term"

	"github.com/openchoreo/openchoreo/internal/occ/auth"
	"github.com/openchoreo/openchoreo/internal/occ/cmd/config"
)

// Stream type prefixes for the exec WebSocket protocol.
// The first byte of each binary WebSocket message identifies the stream.
const (
	streamStdin  = byte(0)
	streamStdout = byte(1)
	streamStderr = byte(2)
	streamResize = byte(3)
)

type terminalSize struct {
	Width  uint16 `json:"width"`
	Height uint16 `json:"height"`
}

// Exec opens an interactive exec session to a component's running pod.
func (cp *Component) Exec(params ExecParams) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	controlPlane, err := config.GetCurrentControlPlane()
	if err != nil {
		return fmt.Errorf("failed to get control plane: %w", err)
	}

	credential, err := config.GetCurrentCredential()
	if err != nil {
		return fmt.Errorf("failed to get credential: %w", err)
	}

	// Default command to /bin/sh
	if len(params.Command) == 0 {
		params.Command = []string{"/bin/sh"}
	}

	wsURL, err := buildExecWebSocketURL(controlPlane.URL, params)
	if err != nil {
		return fmt.Errorf("failed to build exec URL: %w", err)
	}

	headers := http.Header{}
	if credential != nil && credential.Token != "" {
		currentToken := credential.Token
		if auth.IsTokenExpired(currentToken) {
			newToken, refreshErr := auth.RefreshToken()
			if refreshErr != nil {
				return fmt.Errorf("failed to refresh token: %w", refreshErr)
			}
			currentToken = newToken
		}
		headers.Set("Authorization", "Bearer "+currentToken)
	}

	dialer := websocket.Dialer{}
	conn, resp, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			return fmt.Errorf("exec connection failed (HTTP %d): %w", resp.StatusCode, err)
		}
		return fmt.Errorf("failed to connect to exec endpoint: %w", err)
	}
	defer conn.Close()

	// Put terminal in raw mode for interactive TTY sessions
	fd := int(os.Stdin.Fd())
	isTerminal := term.IsTerminal(fd)
	if isTerminal && params.TTY {
		oldState, rawErr := term.MakeRaw(fd)
		if rawErr != nil {
			return fmt.Errorf("failed to set raw terminal mode: %w", rawErr)
		}
		defer func() { _ = term.Restore(fd, oldState) }()

		// Send initial terminal size
		if w, h, sizeErr := term.GetSize(fd); sizeErr == nil {
			sendResize(conn, safeUint16(w), safeUint16(h))
		}

		// Watch for terminal resize signals
		go watchResize(ctx, conn, fd)
	}

	errCh := make(chan error, 2)

	// Pump stdin → WebSocket
	if params.Stdin {
		go func() {
			buf := make([]byte, 32*1024)
			for {
				n, readErr := os.Stdin.Read(buf)
				if n > 0 {
					msg := make([]byte, 1+n)
					msg[0] = streamStdin
					copy(msg[1:], buf[:n])
					if writeErr := conn.WriteMessage(websocket.BinaryMessage, msg); writeErr != nil {
						errCh <- writeErr
						return
					}
				}
				if readErr != nil {
					// stdin closed; signal EOF to remote
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte{streamStdin})
					errCh <- nil
					return
				}
			}
		}()
	}

	// Pump WebSocket → stdout/stderr
	go func() {
		for {
			_, msg, readErr := conn.ReadMessage()
			if readErr != nil {
				errCh <- readErr
				return
			}
			if len(msg) < 2 {
				continue
			}
			switch msg[0] {
			case streamStdout:
				_, _ = os.Stdout.Write(msg[1:])
			case streamStderr:
				_, _ = os.Stderr.Write(msg[1:])
			}
		}
	}()

	select {
	case err := <-errCh:
		if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			return nil
		}
		if err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		return nil
	}
}

func buildExecWebSocketURL(controlPlaneURL string, params ExecParams) (string, error) {
	u, err := url.Parse(controlPlaneURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}

	u.Path = fmt.Sprintf("/exec/namespaces/%s/components/%s", params.Namespace, params.Component)

	q := u.Query()
	if params.Project != "" {
		q.Set("project", params.Project)
	}
	if params.Environment != "" {
		q.Set("env", params.Environment)
	}
	if params.Container != "" {
		q.Set("container", params.Container)
	}
	if params.TTY {
		q.Set("tty", "true")
	}
	if params.Stdin {
		q.Set("stdin", "true")
	}
	for _, cmd := range params.Command {
		q.Add("command", cmd)
	}
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func sendResize(conn *websocket.Conn, width, height uint16) {
	data, err := json.Marshal(terminalSize{Width: width, Height: height})
	if err != nil {
		return
	}
	msg := make([]byte, 1+len(data))
	msg[0] = streamResize
	copy(msg[1:], data)
	_ = conn.WriteMessage(websocket.BinaryMessage, msg)
}

func watchResize(ctx context.Context, conn *websocket.Conn, fd int) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	defer signal.Stop(ch)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			if w, h, err := term.GetSize(fd); err == nil {
				sendResize(conn, safeUint16(w), safeUint16(h))
			}
		}
	}
}

func safeUint16(v int) uint16 {
	if v < 0 {
		return 0
	}
	if v > 0xFFFF {
		return 0xFFFF
	}
	return uint16(v) //nolint:gosec // bounds checked above
}
