package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/crypt"
	"github.com/zc310/ofd/pkg/spec"
)

// Validate 接受文件路径、字节切片、读取器或只读 crypt.View。
// name 用于标识非路径输入的报告。此调用取得视图的独立租约，不关闭源视图。
// 校验逻辑文档前，先检查原始物理条目。
func (v *Validator) Validate(ctx context.Context, input any, name string) Report {
	if ctx == nil {
		ctx = context.Background()
	}
	switch value := input.(type) {
	case string:
		return v.ValidatePath(ctx, value)
	case []byte:
		return v.ValidateReader(ctx, bytes.NewReader(value), name)
	case io.Reader:
		return v.ValidateReader(ctx, value, name)
	}
	report := newReport(name)
	start := report.StartedAt
	if source, ok := input.(interface{ InputSize() int64 }); ok {
		report.Input.Size = source.InputSize()
		if v.opts.MaxInputSize > 0 && report.Input.Size > v.opts.MaxInputSize {
			report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "input.view_size", Message: "原始 ZIP 超过输入大小上限"}, v.opts.MaxErrors)
			report.setCheck("zip", "failed")
			report.finish(start, false, v.opts.FailOnWarning)
			return report
		}
	}
	source, ok := input.(container.PackageSource)
	if !ok {
		report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "input.unsupported", Message: fmt.Sprintf("不支持的输入类型 %T", input)}, v.opts.MaxErrors)
		report.finish(start, false, v.opts.FailOnWarning)
		return report
	}
	var physicalIndex *packageIndex
	var physical *container.Package
	if origin, ok := input.(interface {
		EncryptionInfo() *crypt.Info
		OpenPhysicalPackage() (*container.Package, error)
	}); ok {
		report.Encryption = origin.EncryptionInfo()
		var err error
		physical, err = origin.OpenPhysicalPackage()
		if err == nil {
			defer physical.Close()
			physicalIndex, err = v.indexArchive(physical, &report)
		}
		if err != nil {
			report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "input.view_physical", Message: err.Error()}, v.opts.MaxErrors)
			report.setCheck("zip", "failed")
			report.finish(start, false, v.opts.FailOnWarning)
			return report
		}
		report.setCheck("zip", "passed")
	}
	pkg, err := source.OFDPackage()
	if err != nil {
		if errors.Is(err, crypt.ErrLocked) && physical != nil && report.Encryption != nil && report.Encryption.State != "unlocked" {
			info := report.Encryption
			v.validateEncryption(ctx, physical, physicalIndex, &report)
			report.Encryption = info
			if report.Profile == "" {
				if readable, ok := input.(interface{ Read(string) ([]byte, error) }); ok {
					if data, readErr := readable.Read(rootDocument); readErr == nil {
						doc, _, _ := v.parseXML(packageFile{name: rootDocument, data: data}, "OFD", "", "", &report)
						if doc != nil {
							docType := v.profileDocType(map[string]*xmlDocument{rootDocument: doc})
							if docType == spec.DocTypeOFDA || docType == spec.DocTypeOFDH {
								report.setProfile(docType)
								pc := &profileContext{docType: docType, archive: physicalIndex, report: &report, maxErrors: v.opts.MaxErrors}
								checkNoEncryption(pc.withRule("encrypted", []string{"GB/T 42133 6.16"}))
								report.setCheck("profile", "failed")
							}
						}
					}
				}
			}
		} else {
			report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "input.view", Message: err.Error()}, v.opts.MaxErrors)
		}
		report.finish(start, true, v.opts.FailOnWarning)
		return report
	}
	defer pkg.Close()
	if report.Encryption != nil && report.Encryption.Encrypted {
		report.setCheck("encryption", "passed")
	}
	if physical == nil || !v.validatePhysicalView(ctx, physical, physicalIndex, pkg, &report) {
		v.validatePackage(ctx, pkg, &report)
	}
	report.finish(start, !v.opts.CheckXSD || v.opts.Mode == ModeCompat, v.opts.FailOnWarning)
	return report
}
