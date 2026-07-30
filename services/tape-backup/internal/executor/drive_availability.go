package executor

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"syscall"
)

const defaultProcMountsPath = "/proc/mounts"

// DriveAvailabilityReason is the service-visible drive-state taxonomy.
type DriveAvailabilityReason string

const (
	DriveReady                   DriveAvailabilityReason = "ready"
	DriveOffline                 DriveAvailabilityReason = "drive_offline"
	DriveInUse                   DriveAvailabilityReason = "drive_in_use"
	DriveWaitingForTape          DriveAvailabilityReason = "waiting_for_tape"
	DriveEncryptionInactive      DriveAvailabilityReason = "encryption_inactive"
	DriveMountStaleUnrecoverable DriveAvailabilityReason = "mount_stale_unrecoverable"
)

// DriveAvailability is one classified drive observation. CloseRequired means
// Open succeeded and the caller owns custody until it calls Close.
type DriveAvailability struct {
	Reason        DriveAvailabilityReason
	Message       string
	CloseRequired bool
}

// DriveAvailabilityOptions supplies the mount observation used to distinguish
// a crashed Kura mount from foreign custody. ProcMountsPath defaults to
// /proc/mounts.
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
			ownMount, foreignMount := ltfsMounts(options)
			if ownMount != "" {
				if err := drive.Unmount(); err != nil {
					return DriveAvailability{
						Reason: DriveMountStaleUnrecoverable,
						Message: fmt.Sprintf(
							"stale Kura mount at %s could not be unmounted",
							ownMount,
						),
					}, nil
				}
				slog.Info("cleaned stale kura tape mount", "mountpoint", ownMount)
				if err := drive.Open(); err == nil {
					closeRequired = true
					break
				} else if !errors.Is(err, syscall.EBUSY) {
					return DriveAvailability{}, fmt.Errorf(
						"executor: reopen drive after stale mount cleanup: %w",
						err,
					)
				}
			}
			message := "in use by unidentified process"
			if foreignMount != "" {
				message = "in use by foreign mount at " + foreignMount
			}
			if !closeRequired {
				return DriveAvailability{
					Reason:  DriveInUse,
					Message: message,
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

func ltfsMounts(options DriveAvailabilityOptions) (own, foreign string) {
	path := options.ProcMountsPath
	if path == "" {
		path = defaultProcMountsPath
	}
	file, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		if !strings.Contains(strings.ToLower(fields[2]), "ltfs") {
			continue
		}
		mountpoint := decodeProcMountField(fields[1])
		if options.LTFSRoot != "" && mountpoint == options.LTFSRoot {
			own = mountpoint
			continue
		}
		if foreign == "" {
			foreign = mountpoint
		}
	}
	return own, foreign
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
