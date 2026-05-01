// Package version contains product-wide build identity.
package version

const (
	// ProductVersion is the SemVer product release shared by every shipped binary.
	ProductVersion = "1.3.0"

	// RPCContractVersion is bumped when daemon RPC wire contracts become incompatible.
	RPCContractVersion = "1"
)
