//go:build windows

package ipc

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestCurrentUserPipeSecurityDescriptor(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("token user: %v", err)
	}
	sid := user.User.Sid.String()
	if sid == "" {
		t.Fatal("token user SID is empty")
	}
	descriptor, err := currentUserPipeSecurityDescriptor()
	if err != nil {
		t.Fatalf("security descriptor: %v", err)
	}
	for _, part := range []string{"D:P", "(A;;GA;;;" + sid + ")", "(A;;GA;;;SY)"} {
		if !strings.Contains(descriptor, part) {
			t.Fatalf("security descriptor %q missing %q", descriptor, part)
		}
	}
	if strings.Contains(descriptor, ";;;WD") || strings.Contains(descriptor, ";;;AN") {
		t.Fatalf("security descriptor grants machine-wide access: %q", descriptor)
	}
}
