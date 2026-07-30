package executor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/wyvernzora/kura/services/tape-backup/internal/encryptionkey"
	"github.com/wyvernzora/kura/services/tape-backup/internal/executor"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/tapevolume"
)

const (
	firstMediumSerial  = "MAM-ABC123-0001"
	secondMediumSerial = "MAM-DEF456-0002"
)

func TestDirectoryDriveLoadedIdentityThreeStates(t *testing.T) {
	identifiedRoot := t.TempDir()
	unidentifiedRoot := t.TempDir()
	writeVolume(t, identifiedRoot, "ABC123L6", firstVolume)
	drive := newDrive(t, []executor.DirectoryCartridge{
		{
			TapeID:           "ABC123L6",
			Root:             identifiedRoot,
			Mounted:          true,
			IdentityState:    executor.LoadedIdentityIdentified,
			MediumSerial:     firstMediumSerial,
			EncryptionActive: true,
			Capacity:         100,
		},
		{
			TapeID:           "DEF456L6",
			Root:             unidentifiedRoot,
			Mounted:          false,
			IdentityState:    executor.LoadedIdentityUnidentified,
			MediumSerial:     secondMediumSerial,
			EncryptionActive: true,
			Capacity:         100,
		},
	}, "")

	identity, err := drive.LoadedIdentity()
	if err != nil {
		t.Fatalf("LoadedIdentity none: %v", err)
	}
	if identity != (executor.LoadedIdentity{State: executor.LoadedIdentityNone}) {
		t.Fatalf("none identity = %#v", identity)
	}

	if err := drive.SelectLoaded("DEF456L6"); err != nil {
		t.Fatalf("SelectLoaded unidentified: %v", err)
	}
	identity, err = drive.LoadedIdentity()
	if err != nil {
		t.Fatalf("LoadedIdentity unidentified: %v", err)
	}
	if identity.State != executor.LoadedIdentityUnidentified ||
		identity.MediumSerial != secondMediumSerial ||
		identity.Cartridge.Root != unidentifiedRoot ||
		identity.Cartridge.TapeID != "" ||
		identity.Volume != (tapevolume.Volume{}) {
		t.Fatalf("unidentified identity = %#v", identity)
	}

	if err := drive.SelectLoaded("ABC123L6"); err != nil {
		t.Fatalf("SelectLoaded identified: %v", err)
	}
	identity, err = drive.LoadedIdentity()
	if err != nil {
		t.Fatalf("LoadedIdentity identified: %v", err)
	}
	if identity.State != executor.LoadedIdentityIdentified ||
		identity.MediumSerial != firstMediumSerial ||
		identity.Cartridge != (executor.Cartridge{
			TapeID: "ABC123L6",
			Root:   identifiedRoot,
		}) ||
		identity.Volume.VolumeID != firstVolume ||
		identity.Volume.TapeID != "ABC123L6" {
		t.Fatalf("identified identity = %#v", identity)
	}
}

func TestDirectoryDriveLoadedIdentityFault(t *testing.T) {
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             t.TempDir(),
		Mounted:          false,
		IdentityState:    executor.LoadedIdentityUnidentified,
		MediumSerial:     firstMediumSerial,
		EncryptionActive: true,
		Capacity:         100,
	}}, "ABC123L6")
	drive.SetFaults(executor.DriveFaults{
		LoadedIdentity: errors.New("loaded identity fault"),
	})

	_, err := drive.LoadedIdentity()
	assertExactError(t, err, "loaded identity fault")
}

func TestDirectoryDriveOpenCloseExclusiveCustodyAndFaults(t *testing.T) {
	drive := newDrive(t, nil, "")

	if err := drive.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !drive.IsOpen() {
		t.Fatal("drive is closed after successful Open")
	}
	if err := drive.Open(); err != syscall.EBUSY {
		t.Fatalf("second Open error = %v, want exact EBUSY", err)
	}
	if err := drive.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if drive.IsOpen() {
		t.Fatal("drive remains open after Close")
	}

	drive.SetFaults(executor.DriveFaults{Open: errors.New("open fault")})
	assertExactError(t, drive.Open(), "open fault")
	if drive.IsOpen() {
		t.Fatal("drive opened after failed Open")
	}

	drive.SetFaults(executor.DriveFaults{})
	if err := drive.Open(); err != nil {
		t.Fatalf("Open before close fault: %v", err)
	}
	drive.SetFaults(executor.DriveFaults{Close: errors.New("close fault")})
	assertExactError(t, drive.Close(), "close fault")
	if drive.IsOpen() {
		t.Fatal("drive remains open after faulted Close")
	}
}

func TestDirectoryDriveKeyMountUnmountAndEjectState(t *testing.T) {
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID: "ABC123L6", Root: t.TempDir(), Mounted: false,
		IdentityState: executor.LoadedIdentityUnidentified,
		MediumSerial:  firstMediumSerial, Capacity: 100,
	}}, "ABC123L6")

	if drive.KeySet("ABC123L6") {
		t.Fatal("key is set before SetEncryptionKey")
	}
	if err := drive.SetEncryptionKey(encryptionkey.Key{}); err != nil {
		t.Fatalf("SetEncryptionKey() error = %v", err)
	}
	if !drive.KeySet("ABC123L6") {
		t.Fatal("key remains unset after SetEncryptionKey")
	}
	if err := drive.Mount(); err != nil {
		t.Fatalf("Mount() error = %v", err)
	}
	if !drive.IsMounted("ABC123L6") {
		t.Fatal("cartridge remains unmounted after Mount")
	}
	drive.SetFaults(executor.DriveFaults{BusyUnmount: true})
	if err := drive.Unmount(); err != syscall.EBUSY {
		t.Fatalf("busy Unmount() error = %v, want exact EBUSY", err)
	}
	drive.SetFaults(executor.DriveFaults{})
	if err := drive.Unmount(); err != nil {
		t.Fatalf("Unmount() error = %v", err)
	}
	if err := drive.Eject(); err != nil {
		t.Fatalf("Eject() error = %v", err)
	}
	if !drive.IsEjected("ABC123L6") {
		t.Fatal("cartridge is not marked ejected")
	}
	identity, err := drive.LoadedIdentity()
	if err != nil {
		t.Fatalf("LoadedIdentity() error = %v", err)
	}
	if identity.State != executor.LoadedIdentityNone {
		t.Fatalf("identity state = %q, want none", identity.State)
	}
}

func TestDirectoryDriveFormatCrashFaultLeavesUnidentifiedMedia(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "payload.bin"), []byte("destroyed by format"))
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             root,
		Mounted:          false,
		IdentityState:    executor.LoadedIdentityIdentified,
		MediumSerial:     firstMediumSerial,
		EncryptionActive: true,
		Capacity:         100,
	}}, "ABC123L6")
	drive.SetFaults(executor.DriveFaults{Format: errors.New("format fault")})

	assertExactError(t, drive.Format("ABC123L6"), "format fault")
	assertDirectoryEmpty(t, root)
	identity, err := drive.LoadedIdentity()
	if err != nil {
		t.Fatalf("LoadedIdentity after Format fault: %v", err)
	}
	if identity.State != executor.LoadedIdentityUnidentified ||
		identity.MediumSerial != firstMediumSerial {
		t.Fatalf("identity after Format fault = %#v", identity)
	}

	drive.SetFaults(executor.DriveFaults{})
	writeFile(t, filepath.Join(root, "retry.bin"), []byte("retry"))
	if err := drive.Format("ABC123L6"); err != nil {
		t.Fatalf("Format retry: %v", err)
	}
	assertDirectoryEmpty(t, root)
}

func TestDirectoryDriveStampIdentityCrashFaultLeavesHeaderWithoutReadableMAM(t *testing.T) {
	root := t.TempDir()
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             root,
		Mounted:          true,
		IdentityState:    executor.LoadedIdentityUnidentified,
		MediumSerial:     firstMediumSerial,
		EncryptionActive: true,
		Capacity:         100,
	}}, "ABC123L6")
	drive.SetFaults(executor.DriveFaults{
		StampIdentity: errors.New("stamp identity fault"),
	})
	if err := drive.SetEncryptionKey(encryptionkey.Key{}); err != nil {
		t.Fatalf("SetEncryptionKey() error = %v", err)
	}

	assertExactError(
		t,
		drive.StampIdentity(firstVolume, "ABC123L6"),
		"stamp identity fault",
	)
	header, err := tapevolume.Read(root)
	if err != nil {
		t.Fatalf("Read stamped header after fault: %v", err)
	}
	if header.VolumeID != firstVolume || header.TapeID != "ABC123L6" {
		t.Fatalf("stamped header after fault = %#v", header)
	}
	assertHealthyEmptyWitness(t, root)
	identity, err := drive.LoadedIdentity()
	if err != nil {
		t.Fatalf("LoadedIdentity after StampIdentity fault: %v", err)
	}
	if identity.State != executor.LoadedIdentityUnidentified ||
		identity.MediumSerial != firstMediumSerial {
		t.Fatalf("identity after StampIdentity fault = %#v", identity)
	}
}

func TestDirectoryDriveStampIdentityEstablishesHealthyEmptyWitness(t *testing.T) {
	root := t.TempDir()
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID:           "ABC123L6",
		Root:             root,
		Mounted:          true,
		IdentityState:    executor.LoadedIdentityUnidentified,
		MediumSerial:     firstMediumSerial,
		EncryptionActive: true,
		Capacity:         100,
	}}, "ABC123L6")
	if err := drive.SetEncryptionKey(encryptionkey.Key{}); err != nil {
		t.Fatalf("SetEncryptionKey() error = %v", err)
	}

	if err := drive.StampIdentity(firstVolume, "ABC123L6"); err != nil {
		t.Fatalf("StampIdentity: %v", err)
	}
	assertHealthyEmptyWitness(t, root)
	identity, err := drive.LoadedIdentity()
	if err != nil {
		t.Fatalf("LoadedIdentity after StampIdentity: %v", err)
	}
	if identity.State != executor.LoadedIdentityIdentified ||
		identity.MediumSerial != firstMediumSerial ||
		identity.Volume.VolumeID != firstVolume ||
		identity.Volume.TapeID != "ABC123L6" {
		t.Fatalf("identity after StampIdentity = %#v", identity)
	}
}

func TestClassifyDriveAvailabilityTaxonomy(t *testing.T) {
	tests := []struct {
		name         string
		openErr      error
		loaded       bool
		encryption   bool
		mountLine    string
		wantReason   executor.DriveAvailabilityReason
		wantMessage  string
		wantClose    bool
		wantFakeOpen bool
	}{
		{
			name:        "device absent",
			openErr:     os.ErrNotExist,
			wantReason:  executor.DriveOffline,
			wantMessage: "drive offline",
		},
		{
			name:        "device busy",
			openErr:     syscall.EBUSY,
			wantReason:  executor.DriveInUse,
			wantMessage: "in use by unidentified process",
		},
		{
			name:         "foreign LTFS mount is anomalous",
			openErr:      syscall.EBUSY,
			loaded:       true,
			encryption:   true,
			mountLine:    "/dev/nst0 /foreign fuse.ltfs rw 0 0\n",
			wantReason:   executor.DriveInUse,
			wantMessage:  "in use by foreign mount at /foreign",
			wantFakeOpen: false,
		},
		{
			name:         "no cartridge waits",
			wantReason:   executor.DriveWaitingForTape,
			wantMessage:  "waiting for tape",
			wantClose:    true,
			wantFakeOpen: true,
		},
		{
			name:         "cartridge presence is ready before session key setup",
			loaded:       true,
			wantReason:   executor.DriveReady,
			wantMessage:  "drive ready",
			wantClose:    true,
			wantFakeOpen: true,
		},
		{
			name:         "ready",
			loaded:       true,
			encryption:   true,
			wantReason:   executor.DriveReady,
			wantMessage:  "drive ready",
			wantClose:    true,
			wantFakeOpen: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeVolume(t, root, "ABC123L6", firstVolume)
			drive := newDrive(t, []executor.DirectoryCartridge{{
				TapeID:           "ABC123L6",
				Root:             root,
				Mounted:          true,
				IdentityState:    executor.LoadedIdentityIdentified,
				MediumSerial:     firstMediumSerial,
				EncryptionActive: test.encryption,
				Capacity:         100,
			}}, "")
			if test.loaded {
				if err := drive.SelectLoaded("ABC123L6"); err != nil {
					t.Fatalf("SelectLoaded: %v", err)
				}
			}
			drive.SetFaults(executor.DriveFaults{Open: test.openErr})
			mountsPath := filepath.Join(t.TempDir(), "mounts")
			mountLine := test.mountLine
			if mountLine != "" {
				mountLine = formatMountLine(mountLine, root)
			}
			if err := os.WriteFile(mountsPath, []byte(mountLine), 0o600); err != nil {
				t.Fatalf("Write mounts fixture: %v", err)
			}

			got, err := executor.ClassifyDriveAvailability(
				drive,
				executor.DriveAvailabilityOptions{
					LTFSRoot:       root,
					ProcMountsPath: mountsPath,
				},
			)
			if err != nil {
				t.Fatalf("ClassifyDriveAvailability: %v", err)
			}
			if got.Reason != test.wantReason ||
				got.Message != test.wantMessage ||
				got.CloseRequired != test.wantClose {
				t.Fatalf(
					"classification = %#v, want reason %q message %q close %t",
					got,
					test.wantReason,
					test.wantMessage,
					test.wantClose,
				)
			}
			if drive.IsOpen() != test.wantFakeOpen {
				t.Fatalf("fake open = %t, want %t", drive.IsOpen(), test.wantFakeOpen)
			}
			if got.CloseRequired {
				if err := drive.Close(); err != nil {
					t.Fatalf("Close classified custody: %v", err)
				}
			}
		})
	}
}

func TestClassifyDriveAvailabilityCleansStaleKuraMountAndProceeds(t *testing.T) {
	root := t.TempDir()
	writeVolume(t, root, "ABC123L6", firstVolume)
	base := newDrive(t, []executor.DirectoryCartridge{{
		TapeID: "ABC123L6", Root: root, Mounted: true,
		IdentityState: executor.LoadedIdentityIdentified,
		MediumSerial:  firstMediumSerial, Capacity: 100,
	}}, "ABC123L6")
	drive := &busyOnceDrive{DirectoryDrive: base}
	mountsPath := filepath.Join(t.TempDir(), "mounts")
	if err := os.WriteFile(
		mountsPath,
		[]byte(formatMountLine("/dev/nst0 %s fuse.ltfs rw 0 0\n", root)),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := executor.ClassifyDriveAvailability(
		drive,
		executor.DriveAvailabilityOptions{
			LTFSRoot: root, ProcMountsPath: mountsPath,
		},
	)
	if err != nil {
		t.Fatalf("ClassifyDriveAvailability() error = %v", err)
	}
	if got.Reason != executor.DriveReady || !got.CloseRequired {
		t.Fatalf("classification = %#v, want ready custody", got)
	}
	if base.IsMounted("ABC123L6") {
		t.Fatal("stale Kura mount remains mounted")
	}
	if err := base.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestClassifyDriveAvailabilityNamesUnrecoverableStaleKuraMount(t *testing.T) {
	root := t.TempDir()
	writeVolume(t, root, "ABC123L6", firstVolume)
	drive := newDrive(t, []executor.DirectoryCartridge{{
		TapeID: "ABC123L6", Root: root, Mounted: true,
		IdentityState: executor.LoadedIdentityIdentified,
		MediumSerial:  firstMediumSerial, Capacity: 100,
	}}, "ABC123L6")
	drive.SetFaults(executor.DriveFaults{
		Open:    syscall.EBUSY,
		Unmount: errors.New("injected stale unmount failure"),
	})
	mountsPath := filepath.Join(t.TempDir(), "mounts")
	if err := os.WriteFile(
		mountsPath,
		[]byte(formatMountLine("/dev/nst0 %s fuse.ltfs rw 0 0\n", root)),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := executor.ClassifyDriveAvailability(
		drive,
		executor.DriveAvailabilityOptions{
			LTFSRoot: root, ProcMountsPath: mountsPath,
		},
	)
	if err != nil {
		t.Fatalf("ClassifyDriveAvailability() error = %v", err)
	}
	want := executor.DriveAvailability{
		Reason:  executor.DriveMountStaleUnrecoverable,
		Message: "stale Kura mount at " + root + " could not be unmounted",
	}
	if got != want {
		t.Fatalf("classification = %#v, want %#v", got, want)
	}
}

type busyOnceDrive struct {
	*executor.DirectoryDrive
	openCalls int
}

func (d *busyOnceDrive) Open() error {
	d.openCalls++
	if d.openCalls == 1 {
		return syscall.EBUSY
	}
	return d.DirectoryDrive.Open()
}

func TestRunSessionHoldsCustodyOnlyForSessionDuration(t *testing.T) {
	fixture := newSessionFixture(t)
	action := writeSeries(t, fixture.libraryRoot, "Series", "tvdb:custody", 1)
	fixture.plan = fillPlan(
		firstPlanID,
		firstVolume,
		"ABC123L6",
		nil,
		[]backupplan.Action{action},
	)
	fixture.promote(t)
	normal := fixture.backupHandler(t)
	err := fixture.run(t, func(
		ctx context.Context,
		request executor.BackupActionRequest,
	) error {
		if !fixture.drive.IsOpen() {
			t.Fatal("drive custody was not held inside session")
		}
		return normal(ctx, request)
	})
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	if fixture.drive.IsOpen() {
		t.Fatal("drive custody remains held after session")
	}
	if fixture.drive.IsMounted("ABC123L6") {
		t.Fatal("fill session left the cartridge mounted")
	}
}

func TestNewHardwareDriveIsDeferredPendingHardwarePass(t *testing.T) {
	drive, err := executor.NewHardwareDrive()
	if drive != nil {
		t.Fatalf("NewHardwareDrive drive = %#v, want nil", drive)
	}
	assertExactError(
		t,
		err,
		"executor: hardware drive binding is not implemented pending the section 19 hardware pass",
	)
	if !errors.Is(err, executor.ErrHardwareDriveNotImplemented) {
		t.Fatalf("NewHardwareDrive error = %v, want ErrHardwareDriveNotImplemented", err)
	}
}

func assertDirectoryEmpty(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", path, err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory entries = %v, want empty", directoryEntryNames(entries))
	}
}

func assertHealthyEmptyWitness(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(tapevolume.SnapshotsDir(root))
	if err != nil {
		t.Fatalf("ReadDir snapshots witness: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("snapshots witness entries = %v, want empty", directoryEntryNames(entries))
	}
}

func formatMountLine(pattern, root string) string {
	return strings.Replace(pattern, "%s", root, 1)
}

func directoryEntryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
