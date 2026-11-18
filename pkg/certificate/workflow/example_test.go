package workflow_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/zc310/ofd/pkg/certificate/workflow"
)

func ExampleVerifyLock() {
	// 应用必须同时提供信任、吊销证据和机构角色授权策略。
	// 即使指定了签名ID，也不会因省略授权回调而默认接受。
	err := workflow.VerifyLock(context.Background(), nil, workflow.VerifyOptions{
		LockID: "issuer-lock", Role: workflow.Original,
	})
	fmt.Println(errors.Is(err, workflow.ErrEvidenceRequired))
	// Output: true
}
