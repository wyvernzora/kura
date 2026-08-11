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
	"syscall"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/encryptionkey"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
	"github.com/wyvernzora/kura/services/tape-backup/internal/tape"
	"github.com/wyvernzora/kura/services/tape-backup/internal/volume"
)

var (
	// ErrNoCartridge reports that the drive has no selected cartridge.
	ErrNoCartridge = errors.New("executor: no cartridge loaded")
	// ErrNotMounted reports that the loaded cartridge has no usable LTFS mount.
	ErrNotMounted = errors.New("executor: loaded cartridge is not mounted")
	// ErrStillMounted reports that an operation requiring the raw device found
	// the loaded cartridge still mounted.
	ErrStillMounted = errors.New("executor: loaded cartridge is still mounted")
	// ErrEncryptionKeyUnset reports an identity stamp attempted without the
	// session key being programmed.
	ErrEncryptionKeyUnset = errors.New("executor: encryption key is not set")
	// ErrDriveBusySession reports an eject attempted while session custody is
	// held.
	ErrDriveBusySession = errors.New("executor: drive is busy with a session")
)

// Cartridge describes the cartridge selected in the drive and its fake mount.
type Cartridge struct {
	TapeID tape.ID
	Root   string
}

// LoadedIdentityState is the three-valued cartridge identity observation.
type LoadedIdentityState string

const (
	// LoadedIdentityNone means no cartridge is present.
	LoadedIdentityNone LoadedIdentityState = "none"
	// LoadedIdentityUnidentified means media is present but has no readable Kura
	// identity. MediumSerial still identifies the physical cartridge.
	LoadedIdentityUnidentified LoadedIdentityState = "unidentified"
	// LoadedIdentityIdentified means media, its Kura volume header, and its MAM
	// medium serial are all readable.
	LoadedIdentityIdentified LoadedIdentityState = "identified"
)

// LoadedIdentity is one read of cartridge presence and identity.
type LoadedIdentity struct {
	State        LoadedIdentityState
	MediumSerial string
	Cartridge    Cartridge
	Volume       tapevolume.Volume
}

// Drive is the executor's boundary across the drive device and LTFS mount.
type Drive interface {
	Open() error
	Close() error
	LoadedIdentity() (LoadedIdentity, error)
	SetEncryptionKey(key encryptionkey.Key) error
	EncryptionActive() (bool, error)
	Mount() error
	Unmount() error
	Eject() error
	Capacity() (total, free int64, err error)
	Sync() error
	Format(tapeID tape.ID) error
	StampIdentity(volumeID volume.ID, tapeID tape.ID) error
}

// DirectoryCartridge configures one cartridge in a DirectoryDrive.
type DirectoryCartridge struct {
	TapeID           tape.ID
	Root             string
	Mounted          bool
	IdentityState    LoadedIdentityState
	MediumSerial     string
	EncryptionActive bool
	KeySet           bool
	Capacity         int64
}

// DriveFaults injects failures at each Drive method boundary.
type DriveFaults struct {
	Open                          error
	Close                         error
	LoadedIdentity                error
	SetEncryptionKey              error
	EncryptionActive              error
	EncryptionInactiveAfterFormat bool
	Mount                         error
	Unmount                       error
	BusyUnmount                   bool
	Eject                         error
	Capacity                      error
	Sync                          error
	Format                        error
	StampIdentity                 error
}

// DirectoryDrive is a directory-backed fake Drive with a mutable loaded
// selector. Its capacity accounting is monotonic because LTFS does not reclaim
// bytes when files are deleted.
type DirectoryDrive struct {
	mu         sync.Mutex
	cartridges map[tape.ID]*directoryCartridge
	loaded     tape.ID
	open       bool
	faults     DriveFaults
}

type directoryCartridge struct {
	root             string
	mounted          bool
	identityState    LoadedIdentityState
	mediumSerial     string
	encryptionActive bool
	keySet           bool
	keyFingerprint   string
	ejected          bool
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
	if cartridge.IdentityState != LoadedIdentityUnidentified &&
		cartridge.IdentityState != LoadedIdentityIdentified {
		return fmt.Errorf(
			"executor: directory cartridge %s identity state must be unidentified or identified",
			parsedTapeID,
		)
	}
	if cartridge.MediumSerial == "" {
		return fmt.Errorf(
			"executor: directory cartridge %s medium serial is required",
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
		identityState:    cartridge.IdentityState,
		mediumSerial:     cartridge.MediumSerial,
		encryptionActive: cartridge.EncryptionActive,
		keySet:           cartridge.KeySet,
		capacity:         cartridge.Capacity,
		consumed:         consumed,
	}
	return nil
}

// KeySet reports whether the fake has a programmed key for one cartridge.
func (d *DirectoryDrive) KeySet(tapeID tape.ID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	cartridge, exists := d.cartridges[tapeID]
	return exists && cartridge.keySet
}

// SetEncryptionActive changes the fake AME observation for one cartridge.
func (d *DirectoryDrive) SetEncryptionActive(tapeID tape.ID, active bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	cartridge, exists := d.cartridges[tapeID]
	if !exists {
		return fmt.Errorf("executor: directory cartridge %s is not configured", tapeID)
	}
	cartridge.encryptionActive = active
	return nil
}

// IsMounted reports the fake mount state for one cartridge.
func (d *DirectoryDrive) IsMounted(tapeID tape.ID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	cartridge, exists := d.cartridges[tapeID]
	return exists && cartridge.mounted
}

// IsEjected reports whether the fake cartridge was unloaded through Eject.
func (d *DirectoryDrive) IsEjected(tapeID tape.ID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	cartridge, exists := d.cartridges[tapeID]
	return exists && cartridge.ejected
}

// SelectLoaded changes the fake drive's loaded cartridge.
func (d *DirectoryDrive) SelectLoaded(tapeID tape.ID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.cartridges[tapeID]; !exists {
		return fmt.Errorf("executor: directory cartridge %s is not configured", tapeID)
	}
	d.loaded = tapeID
	d.cartridges[tapeID].ejected = false
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

// IsOpen reports whether the fake drive currently has session custody.
func (d *DirectoryDrive) IsOpen() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.open
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

// Open acquires exclusive fake drive custody.
func (d *DirectoryDrive) Open() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.faults.Open != nil {
		return d.faults.Open
	}
	if d.open {
		return syscall.EBUSY
	}
	d.open = true
	return nil
}

// Close releases fake drive custody. A close fault models process death: the
// kernel still releases custody even though the call did not report success.
func (d *DirectoryDrive) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.open {
		return nil
	}
	d.open = false
	return d.faults.Close
}

// LoadedIdentity reports cartridge presence, MAM serial, and readable Kura
// identity as three distinct states.
func (d *DirectoryDrive) LoadedIdentity() (LoadedIdentity, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.faults.LoadedIdentity != nil {
		return LoadedIdentity{}, d.faults.LoadedIdentity
	}
	if d.loaded == "" {
		return LoadedIdentity{State: LoadedIdentityNone}, nil
	}
	cartridge, err := d.loadedCartridge()
	if err != nil {
		return LoadedIdentity{}, err
	}
	identity := LoadedIdentity{
		State:        cartridge.identityState,
		MediumSerial: cartridge.mediumSerial,
		Cartridge: Cartridge{
			Root: cartridge.root,
		},
	}
	if cartridge.identityState == LoadedIdentityUnidentified {
		return identity, nil
	}
	header, err := tapevolume.Read(cartridge.root)
	if err != nil {
		return LoadedIdentity{}, fmt.Errorf("executor: read directory cartridge identity: %w", err)
	}
	identity.Cartridge.TapeID = header.TapeID
	identity.Volume = header
	return identity, nil
}

// SetEncryptionKey programs the fake AME key for the selected cartridge.
func (d *DirectoryDrive) SetEncryptionKey(key encryptionkey.Key) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.faults.SetEncryptionKey != nil {
		return d.faults.SetEncryptionKey
	}
	cartridge, err := d.loadedCartridge()
	if err != nil {
		return err
	}
	cartridge.keySet = true
	cartridge.keyFingerprint = key.Fingerprint()
	cartridge.encryptionActive = true
	return nil
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

// Mount models the service-owned LTFS mount and its automatic no-op recovery
// hook.
func (d *DirectoryDrive) Mount() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.faults.Mount != nil {
		return d.faults.Mount
	}
	cartridge, err := d.loadedCartridge()
	if err != nil {
		return err
	}
	cartridge.mounted = true
	return nil
}

// Unmount tears down the fake LTFS mount. It is idempotent.
func (d *DirectoryDrive) Unmount() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.faults.Unmount != nil {
		return d.faults.Unmount
	}
	if d.faults.BusyUnmount {
		return syscall.EBUSY
	}
	cartridge, err := d.loadedCartridge()
	if err != nil {
		return err
	}
	cartridge.mounted = false
	return nil
}

// Eject unloads the selected fake cartridge after it has been unmounted.
func (d *DirectoryDrive) Eject() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.open {
		return ErrDriveBusySession
	}
	if d.faults.Eject != nil {
		return d.faults.Eject
	}
	cartridge, err := d.loadedCartridge()
	if err != nil {
		return err
	}
	if cartridge.mounted {
		return ErrStillMounted
	}
	cartridge.ejected = true
	d.loaded = ""
	return nil
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

// Format models mkltfs: it destroys the mounted contents, leaves the medium
// unmounted and unidentified, and preserves its immutable MAM serial. The
// injected fault fires after that crash-shaped state has been established.
func (d *DirectoryDrive) Format(tapeID tape.ID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	cartridge, err := d.loadedCartridgeForTape(tapeID)
	if err != nil {
		return err
	}
	if cartridge.mounted {
		return ErrStillMounted
	}
	cartridge.identityState = LoadedIdentityUnidentified
	cartridge.synced = false
	if d.faults.EncryptionInactiveAfterFormat {
		cartridge.encryptionActive = false
	}
	if err := clearDirectory(cartridge.root); err != nil {
		return fmt.Errorf("executor: format directory cartridge %s: %w", tapeID, err)
	}
	cartridge.consumed = 0
	return d.faults.Format
}

// StampIdentity writes the Kura volume header and establishes the empty
// snapshots namespace. The injected fault fires before the fake MAM identity
// becomes readable, leaving the row-7 header-without-MAM crash state.
func (d *DirectoryDrive) StampIdentity(volumeID volume.ID, tapeID tape.ID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	cartridge, err := d.loadedCartridgeForTape(tapeID)
	if err != nil {
		return err
	}
	if !cartridge.mounted {
		return ErrNotMounted
	}
	if !cartridge.keySet {
		return ErrEncryptionKeyUnset
	}
	if err := tapevolume.Write(cartridge.root, tapevolume.Volume{
		VolumeID:       volumeID,
		TapeID:         tapeID,
		KeyFingerprint: cartridge.keyFingerprint,
		CreatedAt:      time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("executor: stamp directory cartridge identity: %w", err)
	}
	if err := os.MkdirAll(tapevolume.SnapshotsDir(cartridge.root), 0o775); err != nil {
		return fmt.Errorf("executor: create stamped snapshots namespace: %w", err)
	}
	if d.faults.StampIdentity != nil {
		cartridge.identityState = LoadedIdentityUnidentified
		return d.faults.StampIdentity
	}
	cartridge.identityState = LoadedIdentityIdentified
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

func (d *DirectoryDrive) loadedCartridgeForTape(tapeID tape.ID) (*directoryCartridge, error) {
	cartridge, err := d.loadedCartridge()
	if err != nil {
		return nil, err
	}
	if d.loaded != tapeID {
		return nil, fmt.Errorf(
			"executor: loaded directory cartridge is %s, want %s",
			d.loaded,
			tapeID,
		)
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

func clearDirectory(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
