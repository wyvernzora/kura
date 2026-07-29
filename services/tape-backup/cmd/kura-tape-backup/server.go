package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/config"
	"github.com/wyvernzora/kura/services/tape-backup/internal/executor"
	"github.com/wyvernzora/kura/services/tape-backup/internal/server/auth"
	restserver "github.com/wyvernzora/kura/services/tape-backup/internal/server/rest"
	"github.com/wyvernzora/kura/services/tape-backup/internal/service"
	"github.com/wyvernzora/kura/services/tape-backup/internal/storage/backupplan"
)

const drivePollInterval = time.Second

func runServer(
	parent context.Context,
	cfg config.Config,
	getenv func(string) string,
	stderr io.Writer,
) error {
	logger := newLogger(stderr, cfg.Server.LogLevel)
	slog.SetDefault(logger)

	tokenResult, err := auth.Load(auth.Options{
		Disabled:  cfg.Auth.Disabled,
		Token:     getenv(auth.EnvLiteral),
		TokenPath: cfg.Auth.TokenPath,
	})
	if err != nil {
		return err
	}
	logTokenStatus(logger, tokenResult, cfg.Auth.TokenPath)

	drive, err := executor.NewHardwareDrive()
	if err != nil {
		return err
	}
	backup, err := executor.NewBackupActionHandler(nil, executor.OpenDestination, nil)
	if err != nil {
		return err
	}
	host, err := os.Hostname()
	if err != nil {
		return err
	}
	tapeService, err := service.New(service.Deps{
		StateRoot:       cfg.State.Root,
		LibraryRoot:     cfg.Library.Root,
		LTFSRoot:        cfg.Tape.LTFSRoot,
		Drive:           drive,
		Backup:          backup,
		Creator:         backupplan.Creator{Version: version, Host: host},
		FreeSpaceMargin: cfg.Tape.FreeSpaceMargin,
		FlushCadence:    cfg.Tape.FlushCadence,
		PollInterval:    drivePollInterval,
		IdleTimeout:     cfg.Tape.IdleTimeout,
	})
	if err != nil {
		return err
	}
	server, err := restserver.NewServer(restserver.Deps{
		Service:     tapeService,
		Logger:      logger,
		BearerToken: tokenResult.Token,
		Version:     version,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger.Info(
		"kura tape backup serve starting",
		"service", "kura-tape-backup",
		"version", version,
		"commit", commit,
		"addr", cfg.Server.Addr,
		"ltfs_root", cfg.Tape.LTFSRoot,
		"drive_device", cfg.Tape.DriveDevice,
	)
	err = restserver.Serve(ctx, cfg.Server.Addr, server)
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info("kura tape backup serve stopped cleanly")
	return nil
}

func newLogger(stderr io.Writer, levelName string) *slog.Logger {
	var level slog.Level
	_ = level.UnmarshalText([]byte(levelName))
	return slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: level}))
}

func logTokenStatus(logger *slog.Logger, result auth.Result, tokenPath string) {
	switch {
	case result.Disabled:
		logger.Warn(
			"kura tape backup auth disabled by config",
			"hint", "front tape-backup with an authenticating proxy or set auth.disabled=false",
		)
	case result.Generated:
		logger.Info(
			"kura tape backup generated bearer token",
			"path", tokenPath,
			"hint", "read the token file and set KURA_TOKEN on clients",
		)
	default:
		logger.Info("kura tape backup bearer token loaded", "source", result.Source)
	}
}
