//go:build windows

package session

import (
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func probe() (*Capability, error) {
	cap := &Capability{Platform: "windows", ExistingSession: -1, ForeignChildSession: -1}

	edition, err := editionID()
	if err != nil {
		cap.Reasons = append(cap.Reasons, fmt.Sprintf("edition probe failed: %v", err))
	} else {
		cap.Edition = edition
		if isHomeEdition(edition) {
			cap.Reasons = append(cap.Reasons,
				fmt.Sprintf("edition %q cannot host child sessions; Pro/Enterprise required", edition))
		} else {
			cap.ChildSessionOK = true
		}
	}

	up, err := termServiceRunning()
	switch {
	case err != nil:
		cap.Reasons = append(cap.Reasons, fmt.Sprintf("TermService query failed: %v", err))
	case !up:
		cap.Reasons = append(cap.Reasons, "TermService is not running")
	default:
		cap.TermServiceUp = true
	}

	cap.MarkerHeld = MarkerHeld()
	if child := probeChildSession(); child.Exists {
		if child.Owned {
			cap.ExistingSession = child.ID
		} else {
			cap.ForeignChildSession = child.ID
			cap.Reasons = append(cap.Reasons,
				fmt.Sprintf("child session %d exists but was not created by ad", child.ID))
		}
	}

	cap.Supported = cap.ChildSessionOK && cap.TermServiceUp
	if cap.Supported && len(cap.Reasons) == 0 {
		cap.Reasons = nil
	}
	return cap, nil
}

func editionID() (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	edition, _, err := k.GetStringValue("EditionID")
	return edition, err
}

func isHomeEdition(edition string) bool {
	switch edition {
	case "Core", "CoreSingleLanguage", "CoreCountrySpecific", "Home", "HomeSingleLanguage":
		return true
	default:
		return false
	}
}

func termServiceRunning() (bool, error) {
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false, err
	}
	defer windows.CloseServiceHandle(manager)
	name, err := windows.UTF16PtrFromString("TermService")
	if err != nil {
		return false, err
	}
	service, err := windows.OpenService(manager, name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return false, err
	}
	defer windows.CloseServiceHandle(service)
	var status windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(service, &status); err != nil {
		return false, err
	}
	return status.CurrentState == windows.SERVICE_RUNNING, nil
}
