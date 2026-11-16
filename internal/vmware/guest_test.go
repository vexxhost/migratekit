package vmware

import "testing"

func TestWindowsGuestIdentifiers(t *testing.T) {
	for _, id := range []string{
		"winXPProGuest", "winNetEnterprise64Guest", "winLonghorn64Guest",
		"windows8Server64Guest", "windows2019srvNext_64Guest", "windowsHyperVGuest",
		"WINDOWS2022SRVNEXT_64GUEST",
	} {
		if !IsWindowsGuest(id) {
			t.Errorf("Windows guest %q was not recognized", id)
		}
	}
	for _, id := range []string{"ubuntu64Guest", "otherLinux64Guest", "darwin23_64Guest", "vmkernel8Guest", "otherGuest", ""} {
		if IsWindowsGuest(id) {
			t.Errorf("non-Windows guest %q was recognized as Windows", id)
		}
	}
}
