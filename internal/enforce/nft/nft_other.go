//go:build !linux

package nft

import (
	"context"
	"errors"
	"log/slog"

	"github.com/MNCloudwerksTechnology/obie/internal/enforce"
)

var errUnsupported = errors.New("the nftables backend requires Linux")

// Backend is the nftables backend; it is unavailable on this platform.
type Backend struct{}

// New returns a backend whose every call fails.
func New(Options, *slog.Logger) *Backend { return &Backend{} }

// Setup fails: nftables needs Linux.
func (*Backend) Setup(context.Context) error { return errUnsupported }

// List fails: nftables needs Linux.
func (*Backend) List(context.Context) ([]enforce.Entry, error) { return nil, errUnsupported }

// Apply fails: nftables needs Linux.
func (*Backend) Apply(context.Context, []enforce.Entry, []enforce.Entry) error {
	return errUnsupported
}

// Teardown fails: nftables needs Linux.
func (*Backend) Teardown(context.Context) error { return errUnsupported }

// Probe reports that nftables needs Linux.
func Probe(context.Context) error { return errUnsupported }
