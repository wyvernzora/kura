//go:build conformance

package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestConfiguredSchemaOwnsMigrationObjects(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	container, err := tcpostgres.Run(ctx,
		"postgres:18-alpine",
		tcpostgres.WithDatabase("kura"),
		tcpostgres.WithUsername("release_indexer"),
		tcpostgres.WithPassword("secret"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCancel()
		_ = container.Terminate(stopCtx)
	})

	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres connection string: %v", err)
	}
	if err := runMigrations(ctx, databaseURL, "releases"); err != nil {
		t.Fatalf("first migration run: %v", err)
	}
	if err := runMigrations(ctx, databaseURL, "releases"); err != nil {
		t.Fatalf("second migration run: %v", err)
	}

	poolConfig, err := runtimePoolConfig(databaseURL, "releases")
	if err != nil {
		t.Fatalf("runtime pool config: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatalf("runtime pool: %v", err)
	}
	defer pool.Close()

	var searchPath string
	if err := pool.QueryRow(ctx, "SELECT current_setting('search_path')").Scan(&searchPath); err != nil {
		t.Fatalf("read runtime search_path: %v", err)
	}
	if searchPath != "releases" {
		t.Fatalf("runtime search_path = %q, want releases", searchPath)
	}

	for _, name := range []string{"releases", "raw_items", "match_events", "goose_db_version"} {
		assertRelationSchema(t, ctx, pool, name, "releases", true)
		assertRelationSchema(t, ctx, pool, name, "public", false)
	}
	assertTypeSchema(t, ctx, pool, "match_status", "releases", true)
	assertTypeSchema(t, ctx, pool, "match_status", "public", false)
}

func assertRelationSchema(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	name, schema string,
	want bool,
) {
	t.Helper()
	var got bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_catalog.pg_class AS c
			JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relname = $2
		)
	`, schema, name).Scan(&got)
	if err != nil {
		t.Fatalf("query relation %s.%s: %v", schema, name, err)
	}
	if got != want {
		t.Fatalf("relation %s.%s exists = %v, want %v", schema, name, got, want)
	}
}

func assertTypeSchema(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	name, schema string,
	want bool,
) {
	t.Helper()
	var got bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_catalog.pg_type AS t
			JOIN pg_catalog.pg_namespace AS n ON n.oid = t.typnamespace
			WHERE n.nspname = $1 AND t.typname = $2
		)
	`, schema, name).Scan(&got)
	if err != nil {
		t.Fatalf("query type %s.%s: %v", schema, name, err)
	}
	if got != want {
		t.Fatalf("type %s.%s exists = %v, want %v", schema, name, got, want)
	}
}
