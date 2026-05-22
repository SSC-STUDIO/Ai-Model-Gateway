// Package version contains product-wide build identity.
//
// ProductVersion can be overridden at build time via ldflags:
//
//	go build -ldflags "-X ai-model-gateway/internal/version.ProductVersion=$(cat VERSION)"
package version

// ProductVersion is the SemVer product release shared by every shipped binary.
// Overridden by -ldflags at release time; falls back to the VERSION-file value
// baked in at CI build time or the default below for `go install`.
var ProductVersion = "dev"

// BuildCommit is the git commit hash, injected at build time.
var BuildCommit = "unknown"

// BuildDate is the UTC build timestamp, injected at build time.
var BuildDate = "unknown"

// RPCContractVersion is bumped when daemon RPC wire contracts become incompatible.
const RPCContractVersion = "1"
