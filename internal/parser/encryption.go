package parser

import (
	"context"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/crypt"
)

func checkEncryption(pkg *core.Package) error {
	if !crypt.Present(pkg) {
		return nil
	}
	info, err := crypt.InspectPackage(context.Background(), pkg, crypt.InspectOptions{Mode: crypt.ModeCompat})
	return &crypt.LockedError{Info: info, Cause: err}
}
