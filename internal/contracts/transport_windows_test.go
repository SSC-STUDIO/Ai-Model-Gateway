//go:build windows

package contracts

import "testing"

func TestPipeAddr(t *testing.T) {
	addr := &pipeAddr{name: "test-pipe"}

	if got := addr.Network(); got != "pipe" {
		t.Errorf("Network() = %v, want %v", got, "pipe")
	}

	want := `\\.\pipe\test-pipe`
	if got := addr.String(); got != want {
		t.Errorf("String() = %v, want %v", got, want)
	}
}
