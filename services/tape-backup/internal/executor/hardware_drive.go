package executor

import (
	"errors"

	"github.com/wyvernzora/kura/services/tape-backup/internal/encryptionkey"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

// ErrHardwareDriveNotImplemented marks the real-drive boundary deferred until
// the section 19 hardware pass.
var ErrHardwareDriveNotImplemented = errors.New(
	"executor: hardware drive binding is not implemented pending the section 19 hardware pass",
)

// HardwareDrive is the deliberately unbound real-device implementation.
//
// Section 19 defers the st/sg node split and exclusive-open behavior, harmless
// O_NONBLOCK probing, medium-serial support, SCSI AME key programming,
// mkltfs/MAM, LTFS mount/recovery/unmount, eject, force-index-sync behavior
// and cost, and post-format capacity reads.
type HardwareDrive struct{}

var _ Drive = (*HardwareDrive)(nil)

// NewHardwareDrive refuses construction until the section 19 hardware pass
// determines the real st, sg, mkltfs, MAM, and LTFS bindings.
func NewHardwareDrive() (*HardwareDrive, error) {
	return nil, ErrHardwareDriveNotImplemented
}

func (*HardwareDrive) Open() error {
	return ErrHardwareDriveNotImplemented
}

func (*HardwareDrive) Close() error {
	return ErrHardwareDriveNotImplemented
}

func (*HardwareDrive) LoadedIdentity() (LoadedIdentity, error) {
	return LoadedIdentity{}, ErrHardwareDriveNotImplemented
}

func (*HardwareDrive) SetEncryptionKey(encryptionkey.Key) error {
	return ErrHardwareDriveNotImplemented
}

func (*HardwareDrive) EncryptionActive() (bool, error) {
	return false, ErrHardwareDriveNotImplemented
}

func (*HardwareDrive) Mount() error {
	return ErrHardwareDriveNotImplemented
}

func (*HardwareDrive) Unmount() error {
	return ErrHardwareDriveNotImplemented
}

func (*HardwareDrive) Eject() error {
	return ErrHardwareDriveNotImplemented
}

func (*HardwareDrive) Capacity() (total, free int64, err error) {
	return 0, 0, ErrHardwareDriveNotImplemented
}

func (*HardwareDrive) Sync() error {
	return ErrHardwareDriveNotImplemented
}

func (*HardwareDrive) Format(tape.ID) error {
	return ErrHardwareDriveNotImplemented
}

func (*HardwareDrive) StampIdentity(volume.ID, tape.ID) error {
	return ErrHardwareDriveNotImplemented
}
