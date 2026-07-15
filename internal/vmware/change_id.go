package vmware

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vmware/govmomi/vim25/types"
)

var ErrInvalidChangeID = errors.New("invalid change ID")
var ErrCBTNotEnabled = errors.New("CBT is not enabled")

type ChangeID struct {
	UUID   string
	Number string
	Value  string
}

func ParseChangeID(changeId string) (*ChangeID, error) {
	changeIdParts := strings.Split(changeId, "/")
	if len(changeIdParts) != 2 {
		return nil, ErrInvalidChangeID
	}

	return &ChangeID{
		UUID:   changeIdParts[0],
		Number: changeIdParts[1],
		Value:  changeId,
	}, nil
}

func GetChangeID(disk *types.VirtualDisk) (*ChangeID, error) {
	var changeId string

	if b, ok := disk.Backing.(*types.VirtualDiskFlatVer2BackingInfo); ok {
		changeId = b.ChangeId
	} else if b, ok := disk.Backing.(*types.VirtualDiskSparseVer2BackingInfo); ok {
		changeId = b.ChangeId
	} else if b, ok := disk.Backing.(*types.VirtualDiskRawDiskMappingVer1BackingInfo); ok {
		changeId = b.ChangeId
	} else if b, ok := disk.Backing.(*types.VirtualDiskRawDiskVer2BackingInfo); ok {
		changeId = b.ChangeId
	} else {
		return nil, errors.New("failed to get change ID")
	}

	if changeId == "" {
		return nil, fmt.Errorf("%w on disk %d", ErrCBTNotEnabled, disk.Key)
	}

	return ParseChangeID(changeId)
}
