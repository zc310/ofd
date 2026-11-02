package pdf_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/zc310/ofd/internal/testutil"
	"github.com/zc310/ofd/pkg/converter"
	pdfimport "github.com/zc310/ofd/pkg/converter/import/pdf"
)

func TestDirectPDFEntrypointsShareOptionsAndPreserveTargetOnFailure(t *testing.T) {
	input := testutil.MinimalPDF([]byte("/Missing Do"), 200, 200)
	dir := t.TempDir()
	source, target := filepath.Join(dir, "input.pdf"), filepath.Join(dir, "output.ofd")
	if err := os.WriteFile(source, input, 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		options converter.PDFImportOptions
		cancel  bool
		want    error
	}{
		{name: "默认配置"},
		{name: "输入预算", options: converter.PDFImportOptions{Limits: converter.PDFImportLimits{MaxInputBytes: 1}}, want: pdfimport.ErrLimitExceeded},
		{name: "严格模式", options: converter.PDFImportOptions{Strict: true}, want: pdfimport.ErrUnsupported},
		{name: "取消", cancel: true, want: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := []byte("原有目标文件")
			if err := os.WriteFile(target, original, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancel {
				cancel()
			}
			options := pdfimport.Options{PDFImportOptions: test.options}
			var output bytes.Buffer
			streamErr := pdfimport.Convert(ctx, input, &output, options)
			fileErr := pdfimport.ConvertFile(ctx, source, target, options)
			if !errors.Is(streamErr, test.want) || !errors.Is(fileErr, test.want) {
				t.Fatalf("两入口结果与预期不同：stream=%v file=%v want=%v", streamErr, fileErr, test.want)
			}
			if test.cancel && (streamErr != ctx.Err() || fileErr != ctx.Err()) { //nolint:errorlint // 取消契约要求原样返回 ctx.Err()。
				t.Fatal("取消错误没有原样返回")
			}
			got, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if test.want != nil {
				if !bytes.Equal(got, original) {
					t.Fatal("转换失败覆盖了原有目标文件")
				}
			} else if !bytes.HasPrefix(got, []byte("PK")) || !bytes.HasPrefix(output.Bytes(), []byte("PK")) {
				t.Fatal("默认选项没有生成 OFD 包")
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 2 {
				t.Fatalf("转换遗留临时文件：%v %v", files, err)
			}
			if options.Strict != test.options.Strict || options.Limits != test.options.Limits {
				t.Fatal("转换修改了调用方选项")
			}
		})
	}
}

func TestDirectPDFEntrypointsUsePasswordFromOptions(t *testing.T) {
	plain := testutil.MinimalPDF([]byte("0 0 1 1 re f"), 200, 200)
	conf, err := api.LoadConfiguration(api.ConfigurationOptions{Mode: api.ConfigurationModeStateless})
	if err != nil {
		t.Fatal(err)
	}
	conf.UserPW, conf.OwnerPW = "user", "owner"
	var encrypted bytes.Buffer
	if err := api.Encrypt(t.Context(), bytes.NewReader(plain), &encrypted, conf); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	source, target := filepath.Join(dir, "input.pdf"), filepath.Join(dir, "output.ofd")
	if err := os.WriteFile(source, encrypted.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for _, password := range []string{"", "wrong", "user"} {
		options := pdfimport.Options{Password: password}
		var output bytes.Buffer
		streamErr := pdfimport.Convert(t.Context(), encrypted.Bytes(), &output, options)
		fileErr := pdfimport.ConvertFile(t.Context(), source, target, options)
		if password == "user" {
			if streamErr != nil || fileErr != nil || !bytes.HasPrefix(output.Bytes(), []byte("PK")) {
				t.Fatalf("选项中的正确口令未生效：%v %v", streamErr, fileErr)
			}
		} else if streamErr == nil || fileErr == nil || output.Len() != 0 {
			t.Fatal("缺失或错误口令被忽略")
		}
	}
}
