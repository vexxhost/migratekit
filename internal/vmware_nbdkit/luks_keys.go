package vmware_nbdkit

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
)

// LUKSKeyFileArgs validates file references without reading their secret values.
func LUKSKeyFileArgs(values []string) ([]string, error) {
	var keys []string
	for _, value := range values {
		selector, filename, ok := strings.Cut(value, "=")
		if !ok || filename == "" || selector == "" {
			return nil, errors.New("--luks-key-file requires SELECTOR=PATH")
		}
		// virt-v2v parses --key as colon-delimited fields without escaping.
		if strings.Contains(filename, ":") {
			return nil, errors.New("--luks-key-file path cannot contain a colon")
		}
		keyUUID, uuidErr := uuid.Parse(selector)
		device := strings.HasPrefix(selector, "/dev/") && len(selector) > len("/dev/") && !strings.ContainsAny(selector, ":\x00")
		if selector != "all" && uuidErr != nil && !device {
			return nil, errors.New("--luks-key-file selector must be all, a LUKS UUID, or a libguestfs device path")
		}
		if uuidErr == nil {
			selector = keyUUID.String()
		}
		info, err := os.Stat(filename)
		if err != nil {
			return nil, fmt.Errorf("checking LUKS key file for %s: %w", selector, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("LUKS key file for %s must be a regular file", selector)
		}
		file, err := os.Open(filename)
		if err != nil {
			return nil, fmt.Errorf("opening LUKS key file for %s: %w", selector, err)
		}
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("closing LUKS key file for %s: %w", selector, err)
		}
		keys = append(keys, selector+":file:"+filename)
	}
	return keys, nil
}
