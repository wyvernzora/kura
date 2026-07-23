package main

import "github.com/wyvernzora/kura/services/library-manager/internal/cli/client"

// clientFromRT constructs a REST client from runContext env vars. Used
// by every migrated verb so KURA_SERVER_URL discovery is consistent.
func clientFromRT(rt *runContext) *client.Client {
	return client.FromEnv(rt.Getenv)
}

// operatorClientFromRT is the operator-flag variant for verbs that hit
// operator-only endpoints (remove --purge, trash empty/restore,
// reindex, reconcile recover).
func operatorClientFromRT(rt *runContext) *client.Client {
	return client.FromEnv(rt.Getenv).AsOperator()
}
