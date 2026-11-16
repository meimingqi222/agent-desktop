//go:build windows

package session

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func probe() (*Capability, error) {
	cap := &Capability{Platform: "windows", ExistingSession: -1}

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

	if id, ok := findShadowSession(); ok {
		cap.ExistingSession = id
		cap.MarkerHeld = MarkerHeld()
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
	m, err := mgr.Connect()
	if err != nil {
		return false, err
	}
	defer m.Disconnect()
	s, err := m.OpenService("TermService")
	if err != nil {
		return false, err
	}
	defer s.Close()
	st, err := s.Query()
	if err != nil {
		return false, err
	}
	return st.State == svc.Running, nil
}
