package main

import "github.com/wyvernzora/kura/cli/internal/cli/client"

// clientFromRT constructs a REST client from runContext env vars. Used
// by every migrated verb so KURA_SERVER_URL discovery is consistent.
func clientFromRT(rt *runContext) *client.Client {
	return client.FromEnv(rt.Getenv)
}
