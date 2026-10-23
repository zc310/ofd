package crypt

import (
	"context"

	protocol "github.com/zc310/ofd/internal/crypt"
)

// Open builds a read-only logical view, retaining ciphertext and recovered keys.
// It does not create or cache a plaintext ZIP. The logical index is established
// and padding-checked on open; resource reads subsequently decrypt afresh.
// Close the view after use. Consumers passed a fully unlocked view acquire
// independent leases, so closing a converter/parser does not close the owner.
// AllowPartial exposes only known unlocked entries, never placeholder fallback.
func Open(ctx context.Context, input any, opt OpenOptions) (*View, error) {
	configured, err := protocol.NormalizeDecryptOptions(opt.DecryptOptions)
	if err != nil {
		return nil, err
	}
	opt.DecryptOptions = configured
	pkg, closeInput, err := openInput(ctx, input, configured.Limits.MaxInputBytes)
	if err != nil {
		return nil, err
	}
	view, err := protocol.OpenView(ctx, pkg, opt, func() error { closeInput(); return nil })
	if err != nil {
		closeInput()
		return nil, err
	}
	return view, nil
}
