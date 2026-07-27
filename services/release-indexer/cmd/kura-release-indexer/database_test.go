package main

import "testing"

func TestDatabaseConfigsSetExactSearchPath(t *testing.T) {
	const (
		databaseURL = "postgres://release_indexer:secret@localhost/kura?search_path=public"
		schema      = "releases"
	)

	migration, err := migrationConfig(databaseURL, schema)
	if err != nil {
		t.Fatalf("migrationConfig() error = %v", err)
	}
	if got := migration.RuntimeParams["search_path"]; got != schema {
		t.Fatalf("migration search_path = %q, want %q", got, schema)
	}

	runtime, err := runtimePoolConfig(databaseURL, schema)
	if err != nil {
		t.Fatalf("runtimePoolConfig() error = %v", err)
	}
	if got := runtime.ConnConfig.RuntimeParams["search_path"]; got != schema {
		t.Fatalf("runtime search_path = %q, want %q", got, schema)
	}
}
