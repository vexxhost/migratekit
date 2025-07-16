package target

import (
	"context"

	"github.com/francoisovh/migratekit/internal/vmware"
	"github.com/vmware/govmomi/vim25/types"
)

type Target interface {
	GetDisk() *types.VirtualDisk
	Connect(context.Context) error
	GetPath(context.Context) (string, error)
	Disconnect(context.Context) error
	Exists(context.Context) (bool, error)
	GetCurrentChangeID(context.Context) (*vmware.ChangeID, error)
	WriteChangeID(context.Context, *vmware.ChangeID) error
}
