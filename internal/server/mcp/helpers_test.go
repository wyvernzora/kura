package mcp

// serverVersion is the version string the test suite uses when
// constructing in-memory mcp servers directly via sdkmcp.NewServer.
// Production code drives the real version through Deps.Version, with
// defaultServerVersion as the fallback. Keeping this in a *_test.go
// file lets every existing test continue to reference `serverVersion`
// without leaking a hardcoded version into the production binary.
const serverVersion = defaultServerVersion
