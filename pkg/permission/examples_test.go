package permission_test

import (
	"encoding/xml"
	"errors"
	"fmt"

	"github.com/zc310/ofd/pkg/permission"
	"github.com/zc310/ofd/pkg/spec"
)

func ExamplePolicy_Check() {
	var declaration spec.Permissions
	if err := xml.Unmarshal([]byte(`<Permissions><Export>false</Export></Permissions>`), &declaration); err != nil {
		panic(err)
	}
	policy := permission.Policy{}
	fmt.Println(policy.Check(nil, permission.Export, 0))
	fmt.Println(errors.Is(policy.Check(&declaration, permission.Export, 0), permission.ErrDenied))
	fmt.Println(policy.Check(&declaration, permission.Access, 0))
	// Output:
	// <nil>
	// true
	// <nil>
}
