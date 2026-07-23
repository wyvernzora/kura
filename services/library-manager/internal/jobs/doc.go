// Package jobs provides workflow-layer job machinery shared by all
// surfaces (CLI, MCP, REST). Workflows submit jobs through a Registry;
// callers either Wait for the typed result or hand the job's ID off to a client
// that polls.
package jobs
