package rest

// serverVersion is the version string the rest test suite asserts
// against. Production code drives the real version through Deps.Version
// (defaulted to defaultServerVersion inside NewServer); tests construct
// the server with an empty Deps.Version, so the same default must
// appear here for the assertions to align.
const serverVersion = defaultServerVersion
