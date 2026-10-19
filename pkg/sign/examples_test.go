package sign_test

import (
	"context"
	"fmt"

	"github.com/zc310/ofd/pkg/sign"
)

func ExampleVerify() {
	results, err := sign.Verify(context.Background(), "../../testdata/ofdrw/999.ofd", sign.VerificationOptions{})
	if err != nil {
		panic(err)
	}
	fmt.Println(len(results))
	// 省略证书与状态证据时不能据两层签名结果认定完整验证通过。
	fmt.Println(results[0].Result.FullyValid())
	// Output:
	// 1
	// false
}
