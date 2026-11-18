package certificate_test

import (
	"fmt"

	"github.com/zc310/ofd/pkg/certificate"
	"github.com/zc310/ofd/pkg/spec"
)

func ExampleParseIdentifier() {
	// 附录A载荷按正文算法复算的格式示例，不证明实际登记或唯一性。
	id, err := certificate.ParseIdentifier("1.2.156.3005.2.11100000000013338W002.11320482014138860P.201807250201.001.M")
	fmt.Println(id.Version, err)
	// Output: 001 <nil>
}

func ExampleParseMetadata() {
	// 实际条目取自 OFD DocInfo.CustomDatas.CustomData。
	m := certificate.ParseMetadata([]spec.CustomData{{Name: "ZZMC", Value: "示例证照"}})
	fmt.Println(m.Values("certificateName"))
	fmt.Println(len(m.Validate()) > 0) // 其他必选项未提供，仍可读取原始值。
	// Output:
	// [示例证照]
	// true
}
