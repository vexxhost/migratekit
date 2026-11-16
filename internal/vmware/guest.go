package vmware

import "strings"

// IsWindowsGuest recognizes the Windows identifiers used by vSphere, including
// legacy identifiers such as winLonghorn64Guest and winNetEnterprise64Guest.
func IsWindowsGuest(guestID string) bool {
	id := strings.ToLower(guestID)
	return strings.HasPrefix(id, "win")
}
