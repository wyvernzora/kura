// Package executor prepares mounted cartridges for backup-plan execution.
package executor

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
)

var (
	// ErrNoCartridge reports that the drive has no selected cartridge.
	ErrNoCartridge = errors.New("executor: no cartridge loaded")
	// ErrNotMounted reports that the loaded cartridge has no usable LTFS mount.
	ErrNotMounted = errors.New("executor: loaded cartridge is not mounted")
)

// Cartridge describes the cartridge selected in the drive and its fake mount.
type Cartridge struct {
	TapeID tape.ID
	Root   string
}

// Drive is the executor's boundary across the drive device and LTFS mount.
type Drive interface {
	Loaded() (Cartridge, error)
	EncryptionActive() (bool, error)
	Capacity() (total, free int64, err error)
	Sync() error
}

// DirectoryCartridge configures one cartridge in a DirectoryDrive.
type DirectoryCartridge struct {
	TapeID           tape.ID
	Root             string
	Mounted          bool
	EncryptionActive bool
	Capacity         int64
}

// DriveFaults injects failures at each Drive method boundary.
type DriveFaults struct {
	Loaded           error
	EncryptionActive error
	Capacity         error
	Sync             error
}

// DirectoryDrive is a directory-backed fake Drive with a mutable loaded
// selector. Its capacity accounting is monotonic because LTFS does not reclaim
// bytes when files are deleted.
type DirectoryDrive struct {
	mu         sync.Mutex
	cartridges map[tape.ID]*directoryCartridge
	loaded     tape.ID
	faults     DriveFaults
}

type directoryCartridge struct {
	root             string
	mounted          bool
	encryptionActive bool
	capacity         int64
	consumed         int64
	synced           bool
}

var (
	_ Drive            = (*DirectoryDrive)(nil)
	_ CapacityRecorder = (*DirectoryDrive)(nil)
)

// NewDirectoryDrive creates a fake containing the supplied cartridge
// directories.
func NewDirectoryDrive(cartridges []DirectoryCartridge) (*DirectoryDrive, error) {
	drive := &DirectoryDrive{
		cartridges: make(map[tape.ID]*directoryCartridge, len(cartridges)),
	}
	for _, cartridge := range cartridges {
		if err := drive.add(cartridge); err != nil {
			return nil, err
		}
	}
	return drive, nil
}

func (d *DirectoryDrive) add(cartridge DirectoryCartridge) error {
	parsedTapeID, err := tape.ParseID(string(cartridge.TapeID))
	if err != nil {
		return fmt.Errorf("executor: directory cartridge: %w", err)
	}
	if cartridge.Root == "" {
		return fmt.Errorf("executor: directory cartridge %s root is required", parsedTapeID)
	}
	if cartridge.Capacity < 0 {
		return fmt.Errorf(
			"executor: directory cartridge %s capacity must not be negative",
			parsedTapeID,
		)
	}
	if _, exists := d.cartridges[parsedTapeID]; exists {
		return fmt.Errorf("executor: duplicate directory cartridge %s", parsedTapeID)
	}
	info, err := os.Lstat(cartridge.Root)
	if err != nil {
		return fmt.Errorf("executor: inspect directory cartridge %s: %w", parsedTapeID, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf(
			"executor: directory cartridge %s root must be a real directory",
			parsedTapeID,
		)
	}
	if err := os.MkdirAll(tapevolume.ArchiveDir(cartridge.Root), 0o775); err != nil {
		return fmt.Errorf(
			"executor: create directory cartridge %s archive: %w",
			parsedTapeID,
			err,
		)
	}
	consumed, err := directoryBytes(cartridge.Root)
	if err != nil {
		return fmt.Errorf("executor: measure directory cartridge %s: %w", parsedTapeID, err)
	}
	d.cartridges[parsedTapeID] = &directoryCartridge{
		root:             cartridge.Root,
		mounted:          cartridge.Mounted,
		encryptionActive: cartridge.EncryptionActive,
		capacity:         cartridge.Capacity,
		consumed:         consumed,
	}
	return nil
}

// SelectLoaded changes the fake drive's loaded cartridge.
func (d *DirectoryDrive) SelectLoaded(tapeID tape.ID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.cartridges[tapeID]; !exists {
		return fmt.Errorf("executor: directory cartridge %s is not configured", tapeID)
	}
	d.loaded = tapeID
	return nil
}

// ClearLoaded makes the fake drive report no loaded cartridge.
func (d *DirectoryDrive) ClearLoaded() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.loaded = ""
}

// SetMounted changes whether one fake cartridge has a usable LTFS mount.
func (d *DirectoryDrive) SetMounted(tapeID tape.ID, mounted bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	cartridge, exists := d.cartridges[tapeID]
	if !exists {
		return fmt.Errorf("executor: directory cartridge %s is not configured", tapeID)
	}
	cartridge.mounted = mounted
	return nil
}

// SetFaults replaces all injected Drive failures.
func (d *DirectoryDrive) SetFaults(faults DriveFaults) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.faults = faults
}

// RecordWrite adds bytes written to a cartridge even when a failed copy removes
// its destination.
func (d *DirectoryDrive) RecordWrite(tapeID tape.ID, bytes int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if bytes < 0 {
		return errors.New("executor: recorded bytes must not be negative")
	}
	cartridge, exists := d.cartridges[tapeID]
	if !exists {
		return fmt.Errorf("executor: directory cartridge %s is not configured", tapeID)
	}
	if bytes > math.MaxInt64-cartridge.consumed {
		return errors.New("executor: consumed capacity overflow")
	}
	cartridge.consumed += bytes
	return nil
}

// WasSynced reports whether Sync completed for a cartridge.
func (d *DirectoryDrive) WasSynced(tapeID tape.ID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	cartridge, exists := d.cartridges[tapeID]
	return exists && cartridge.synced
}

// Loaded reports the selected cartridge's drive-device identity.
func (d *DirectoryDrive) Loaded() (Cartridge, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.faults.Loaded != nil {
		return Cartridge{}, d.faults.Loaded
	}
	cartridge, err := d.loadedCartridge()
	if err != nil {
		return Cartridge{}, err
	}
	return Cartridge{
		TapeID: d.loaded,
		Root:   cartridge.root,
	}, nil
}

// EncryptionActive reports the fake drive-device encryption state.
func (d *DirectoryDrive) EncryptionActive() (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.faults.EncryptionActive != nil {
		return false, d.faults.EncryptionActive
	}
	cartridge, err := d.loadedCartridge()
	if err != nil {
		return false, err
	}
	return cartridge.encryptionActive, nil
}

// Capacity reports total and monotonic free capacity for the mounted fake.
func (d *DirectoryDrive) Capacity() (total, free int64, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.faults.Capacity != nil {
		return 0, 0, d.faults.Capacity
	}
	cartridge, err := d.loadedMountedCartridge()
	if err != nil {
		return 0, 0, err
	}
	observed, err := directoryBytes(cartridge.root)
	if err != nil {
		return 0, 0, fmt.Errorf("executor: measure mounted cartridge: %w", err)
	}
	// Deletion changes observed directory usage but never reclaims LTFS media.
	cartridge.consumed = max(cartridge.consumed, observed)
	return cartridge.capacity, max(int64(0), cartridge.capacity-cartridge.consumed), nil
}

// Sync records a successful fake LTFS force-index-sync.
func (d *DirectoryDrive) Sync() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.faults.Sync != nil {
		return d.faults.Sync
	}
	cartridge, err := d.loadedMountedCartridge()
	if err != nil {
		return err
	}
	cartridge.synced = true
	return nil
}

func (d *DirectoryDrive) loadedCartridge() (*directoryCartridge, error) {
	if d.loaded == "" {
		return nil, ErrNoCartridge
	}
	cartridge, exists := d.cartridges[d.loaded]
	if !exists {
		return nil, ErrNoCartridge
	}
	return cartridge, nil
}

func (d *DirectoryDrive) loadedMountedCartridge() (*directoryCartridge, error) {
	cartridge, err := d.loadedCartridge()
	if err != nil {
		return nil, err
	}
	if !cartridge.mounted {
		return nil, ErrNotMounted
	}
	return cartridge, nil
}

func directoryBytes(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Size() > math.MaxInt64-total {
				return errors.New("directory capacity overflow")
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}
