//go:build windows

package session

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows/registry"
)

func TestPipePresent(t *testing.T) {
	name := fmt.Sprintf("ad-pipe-present-test-%d", time.Now().UnixNano())
	if PipePresent(name) {
		t.Fatalf("pipe %s unexpectedly present before listen", name)
	}
	listener, err := winio.ListenPipe(`\\.\pipe\`+name, nil)
	if err != nil {
		t.Fatalf("listen pipe: %v", err)
	}
	defer listener.Close()
	if !PipePresent(name) {
		t.Fatalf("pipe %s not detected while listening", name)
	}
}

func TestOwnershipRecordSatisfied(t *testing.T) {
	rec := childOwnershipRecord{SessionID: 42, WinStationName: `ABCDEF0123456789#1`}
	if !ownershipRecordSatisfied(rec, 42, `abcdef0123456789#1`) {
		t.Fatal("matching session id and WinStation should satisfy ownership")
	}
	if ownershipRecordSatisfied(rec, 43, `ABCDEF0123456789#1`) {
		t.Fatal("different session id should not satisfy ownership")
	}
	if ownershipRecordSatisfied(rec, 42, `FEDCBA0123456789#1`) {
		t.Fatal("different WinStation should not satisfy ownership")
	}
	if !ownershipRecordSatisfied(rec, 42, "") {
		t.Fatal("empty current WinStation should fall back to the exact child session id")
	}
}

func TestChildOwnershipRecordRoundTrip(t *testing.T) {
	keyPath := fmt.Sprintf(`Software\AdShadowOwnershipTest-%d`, os.Getpid())
	_ = registry.DeleteKey(registry.CURRENT_USER, keyPath)
	defer registry.DeleteKey(registry.CURRENT_USER, keyPath)

	if err := writeChildOwnershipRecord(keyPath, 42, `ABCDEF0123456789#1`); err != nil {
		t.Fatalf("write ownership record: %v", err)
	}
	rec, err := readChildOwnershipRecord(keyPath)
	if err != nil {
		t.Fatalf("read ownership record: %v", err)
	}
	if rec.SessionID != 42 || rec.WinStationName != `ABCDEF0123456789#1` {
		t.Fatalf("ownership record = %+v", rec)
	}
}
