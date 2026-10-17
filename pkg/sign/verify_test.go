package sign

import (
	"context"
	"errors"
	"testing"
)

func TestVerifyPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Verify(ctx, "missing.ofd", VerificationOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误未保留: %v", err)
	}
}
