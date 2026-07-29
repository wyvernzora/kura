package executor

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
)

const defaultProcMountsPath = "/proc/mounts"

// DriveAvailabilityReason is the service-visible drive-state taxonomy.
type DriveAvailabilityReason string

const (
	DriveReady              DriveAvailabilityReason = "ready"
	DriveOffline            DriveAvailabilityReason = "drive_offline"
	DriveInUse              DriveAvailabilityReason = "drive_in_use"
	DriveWaitingForTape     DriveAvailabilityReason = "waiting_for_tape"
	DriveEncryptionInactive DriveAvailabilityReason = "encryption_inactive"
)

// DriveAvailability is one classified drive observation. CloseRequired means
// Open succeeded and the caller owns custody until it calls Close.
type DriveAvailability struct {
	Reason        DriveAvailabilityReason
	Message       string
	CloseRequired bool
}

// DriveAvailabilityOptions supplies the LTFS mount observation used only for
// the EBUSY ready-state carve-out. ProcMountsPath defaults to /proc/mounts.
type DriveAvailabilityOptions struct {
	LTFSRoot       string
	ProcMountsPath string
}

// ClassifyDriveAvailability opens the drive and classifies the normal runtime
// states from the design's refusal taxonomy. On successful Open, custody
// remains held for the caller to close at the end of its session.
func ClassifyDriveAvailability(
	drive Drive,
	options DriveAvailabilityOptions,
) (DriveAvailability, error) {
	if drive == nil {
		return DriveAvailability{}, errors.New("executor: drive is required")
	}

	closeRequired := false
	if err := drive.Open(); err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			return DriveAvailability{
				Reason:  DriveOffline,
				Message: "drive offline",
			}, nil
		case errors.Is(err, syscall.EBUSY):
			if !ltfsMounted(options) {
				return DriveAvailability{
					Reason: DriveInUse,
					Message: "in use by another process — run " +
						"`fuser /dev/nst0` on the node.",
				}, nil
			}
		default:
			return DriveAvailability{}, fmt.Errorf("executor: open drive: %w", err)
		}
	} else {
		closeRequired = true
	}

	identity, err := drive.LoadedIdentity()
	if err != nil {
		return DriveAvailability{}, closeAfterClassificationError(
			drive,
			closeRequired,
			fmt.Errorf("executor: read loaded identity: %w", err),
		)
	}
	if identity.State == LoadedIdentityNone {
		return DriveAvailability{
			Reason:        DriveWaitingForTape,
			Message:       "waiting for tape",
			CloseRequired: closeRequired,
		}, nil
	}

	active, err := drive.EncryptionActive()
	if err != nil {
		return DriveAvailability{}, closeAfterClassificationError(
			drive,
			closeRequired,
			fmt.Errorf("executor: check drive encryption: %w", err),
		)
	}
	if !active {
		return DriveAvailability{
			Reason:        DriveEncryptionInactive,
			Message:       "drive encryption is inactive",
			CloseRequired: closeRequired,
		}, nil
	}
	return DriveAvailability{
		Reason:        DriveReady,
		Message:       "drive ready",
		CloseRequired: closeRequired,
	}, nil
}

func closeAfterClassificationError(drive Drive, closeRequired bool, err error) error {
	if !closeRequired {
		return err
	}
	if closeErr := drive.Close(); closeErr != nil {
		return errors.Join(err, fmt.Errorf("executor: close drive: %w", closeErr))
	}
	return err
}

func ltfsMounted(options DriveAvailabilityOptions) bool {
	if options.LTFSRoot == "" {
		return false
	}
	path := options.ProcMountsPath
	if path == "" {
		path = defaultProcMountsPath
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		if decodeProcMountField(fields[1]) == options.LTFSRoot &&
			strings.Contains(strings.ToLower(fields[2]), "ltfs") {
			return true
		}
	}
	return false
}

func decodeProcMountField(value string) string {
	replacer := strings.NewReplacer(
		`\040`, " ",
		`\011`, "\t",
		`\012`, "\n",
		`\134`, `\`,
	)
	return replacer.Replace(value)
}
