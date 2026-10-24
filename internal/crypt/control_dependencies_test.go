package crypt

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func reorderControlTestOperations(t *testing.T, data []byte, outerFirst, relative bool) []byte {
	t.Helper()
	root := string(data)
	start := strings.Index(root, "<ofd:EncryptInfo ")
	end := strings.Index(root, "</ofd:EncryptInfo>")
	if start < 0 || end < 0 {
		t.Fatal("缺少首层操作")
	}
	end += len("</ofd:EncryptInfo>")
	outerStart := strings.Index(root[end:], "<ofd:EncryptInfo ")
	outerEnd := strings.Index(root[end:], "</ofd:EncryptInfo>")
	if outerStart < 0 || outerEnd < 0 {
		t.Fatal("缺少追加操作")
	}
	outerStart += end
	outerEnd += end + len("</ofd:EncryptInfo>")
	inner, outer := root[start:end], root[outerStart:outerEnd]
	if !strings.Contains(outer, ` Relative="enc1"`) {
		t.Fatal("外层未声明预期依赖")
	}
	if !relative {
		outer = strings.ReplaceAll(outer, ` Relative="enc1"`, "")
	}
	if outerFirst {
		inner, outer = outer, inner
	}
	return []byte(root[:start] + inner + outer + root[outerEnd:])
}

func TestControlFileDependenciesRequireRelativeRegardlessOfOrder(t *testing.T) {
	// GM/T 0099 C.3 表 C.2：跨层控制文件的依赖也应由 Relative 声明。
	original := testPlainFiles()
	innerOptions := testPasswordOptions()
	innerOptions.IndependentKeys = true
	inner, _ := encryptFiles(t, original, innerOptions)
	for _, test := range []struct {
		name    string
		entries []string
	}{
		{"common seed", []string{"Encrypt_1/decryptseed.dat"}},
		{"map", []string{"Encrypt_1/entriesmap.dat"}},
		{"independent seed", []string{"Encrypt_1/entries/000000.seed"}},
		{"common seed and map", []string{"Encrypt_1/decryptseed.dat", "Encrypt_1/entriesmap.dat"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := testPasswordOptions()
			options.Append = &DecryptOptions{Password: []byte("secret")}
			options.Entries = test.entries
			outer, _ := encryptFiles(t, inner, options)
			root := bytes.Clone(outer[EntryPoint])
			for _, outerFirst := range []bool{false, true} {
				for _, relative := range []bool{false, true} {
					for _, maximum := range []int{1, 2} {
						for _, partial := range []bool{false, true} {
							name := fmt.Sprintf("outer-first=%t/relative=%t/layers=%d/partial=%t", outerFirst, relative, maximum, partial)
							t.Run(name, func(t *testing.T) {
								outer[EntryPoint] = reorderControlTestOperations(t, root, outerFirst, relative)
								view, err := OpenView(t.Context(), testPackage(t, outer), OpenOptions{DecryptOptions: DecryptOptions{Password: []byte("secret"), Limits: Limits{MaxLayers: maximum}}, AllowPartial: partial}, nil)
								if view != nil {
									defer view.Close()
								}
								if !relative || maximum == 1 {
									want := ErrInvalidMetadata
									if relative {
										want = ErrLimitExceeded
									}
									if !errors.Is(err, want) || view != nil {
										t.Fatalf("未声明的依赖或深度超限必须失败: view=%v err=%v want=%v", view, err, want)
									}
									return
								}
								if err != nil {
									t.Fatal(err)
								}
								output := map[string][]byte{}
								report, err := view.Export(t.Context(), func(name string, data []byte) error { output[name] = bytes.Clone(data); return nil })
								if err != nil || report.State != "unlocked" || len(report.Operations) != 2 || !reflect.DeepEqual(output, original) {
									t.Fatalf("正确声明依赖后应按任意 XML 顺序完整恢复: report=%+v err=%v", report, err)
								}
							})
						}
					}
				}
			}
		})
	}
}
