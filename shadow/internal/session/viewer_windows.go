//go:build windows

package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Microsoft/go-winio"
)

// rdphostPipeName is the control channel the ad-rdphost helper of this
// parent session listens on. It is keyed by the parent session id, not
// the child's, so any `ad` invocation in this session can reach the
// helper that anchors the shadow session.
func rdphostPipeName() (string, error) {
	sid, err := CurrentSessionID()
	if err != nil {
		return "", fmt.Errorf("resolve session id: %w", err)
	}
	return fmt.Sprintf(`\\.\pipe\ad-rdphost-%d`, sid), nil
}

// ViewerCommand sends one control command ("show", "hide", "shutdown")
// to the ad-rdphost helper and waits for its ack. It errors when no
// helper is listening, which means no shadow session is anchored from
// this session.
func ViewerCommand(cmd string) error {
	name, err := rdphostPipeName()
	if err != nil {
		return err
	}
	conn, err := winio.DialPipe(name, nil)
	if err != nil {
		return fmt.Errorf("dial %s: %w", name, err)
	}
	defer conn.Close()
	body, err := json.Marshal(map[string]string{"cmd": cmd})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(body, '\n')); err != nil {
		return fmt.Errorf("send %s: %w", cmd, err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return fmt.Errorf("read ack: %w", err)
	}
	var ack struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &ack); err != nil {
		return fmt.Errorf("bad ack %q: %w", line, err)
	}
	if !ack.OK {
		return fmt.Errorf("ad-rdphost rejected %q: %s", cmd, ack.Error)
	}
	return nil
}

// ViewerPipeSuffix documents the pipe name layout for tests.
func ViewerPipeSuffix(sessionID int) string {
	return `\\.\pipe\ad-rdphost-` + strconv.Itoa(sessionID)
}
