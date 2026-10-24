package parser

import (
	"context"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/crypt"
)

func checkEncryption(pkg *container.Package) error {
	if !crypt.Present(pkg) {
		return nil
	}
	info, err := crypt.InspectPackage(context.Background(), pkg, crypt.InspectOptions{Mode: crypt.ModeCompat})
	return &crypt.LockedError{Info: info, Cause: err}
}
