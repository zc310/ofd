package main

import (
	"fmt"
	"io"
	"os"

	"github.com/zc310/ofd/pkg/archive"
	"github.com/zc310/ofd/pkg/preserve"
)

// runPreserve 执行 GB/T 42133 长期保存处理。转换只写 --output 指定的新文件，
// 输入 OFD 不受影响；--dry-run 时连新文件也不产生，只列出计划改动。
func runPreserve(opts *options, stdout, stderr io.Writer) int {
	preserveOptions := preserve.Options{
		DocType:          opts.docType,
		Validate:         true,
		DropUnreferenced: opts.dropUnreferenced,
		OnWarning: func(msg string) {
			_, _ = fmt.Fprintf(stderr, "ofd-archive: 警告：%s\n", msg)
		},
	}
	if opts.dryRun {
		result, err := preserve.Plan(opts.input, preserveOptions)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-archive:", err)
			return exitOutput
		}
		if err := writePreservePlan(opts, result, stdout); err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-archive:", err)
			return exitOutput
		}
		return exitOK
	}

	file, err := os.Create(opts.output)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-archive:", err)
		return exitOutput
	}
	result, applyErr := preserve.Apply(opts.input, file, preserveOptions)
	if closeErr := file.Close(); applyErr == nil {
		applyErr = closeErr
	}
	if applyErr != nil {
		// 转换或校验失败时不留下半成品，避免用户误当作合规文件归档。
		_ = os.Remove(opts.output)
		_, _ = fmt.Fprintln(stderr, "ofd-archive:", applyErr)
		return exitOutput
	}
	if err := writePreserveResult(opts, result, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-archive:", err)
		return exitOutput
	}
	return exitOK
}

// writePreservePlan 输出 --dry-run 的计划改动。
func writePreservePlan(opts *options, result preserve.Result, stdout io.Writer) error {
	if opts.format == "json" {
		return archive.RenderJSON(stdout, result, opts.pretty)
	}
	_, _ = fmt.Fprintf(stdout, "DocType\t%s\n", result.DocType)
	_, _ = fmt.Fprintf(stdout, "计划改动\t%d 处（--dry-run，未写出文件）\n", len(result.Changes))
	for _, change := range result.Changes {
		_, _ = fmt.Fprintln(stdout, change)
	}
	writeUnreferenced(opts, result, stdout)
	if opts.format != "json" && len(result.Unreferenced) > 0 {
		_, _ = fmt.Fprintf(stdout, "完整清单\t用 --format json 查看\n")
	}
	return nil
}

// writePreserveResult 输出实际执行的改动。
func writePreserveResult(opts *options, result preserve.Result, stdout io.Writer) error {
	if opts.format == "json" {
		return archive.RenderJSON(stdout, result, opts.pretty)
	}
	_, _ = fmt.Fprintf(stdout, "DocType\t%s\n", result.DocType)
	_, _ = fmt.Fprintf(stdout, "输出\t%s\n", opts.output)
	_, _ = fmt.Fprintf(stdout, "改动\t%d 处\n", len(result.Changes))
	for _, change := range result.Changes {
		_, _ = fmt.Fprintln(stdout, change)
	}
	writeUnreferenced(opts, result, stdout)
	return nil
}

// writeUnreferenced 展示 6.2.1 c) 的删除候选与实际删除结果。候选清单可能很长，
// 因此完整清单只在 JSON 输出里给出，文本输出只报数量与结论。
func writeUnreferenced(opts *options, result preserve.Result, stdout io.Writer) {
	if len(result.Unreferenced) == 0 {
		return
	}
	switch {
	case result.ClosureIncomplete:
		_, _ = fmt.Fprintf(stdout, "无人引用条目\t%d 个（引用闭包不完整，未删除）\n", len(result.Unreferenced))
		_, _ = fmt.Fprintf(stdout, "闭包不完整原因\t%s\n", result.ClosureReason)
	case opts.dryRun && opts.dropUnreferenced:
		// 预检不真删，UnreferencedDropped 保持 0；此时要说"将删除"，
		// 而不是落到 default 分支去提示"加 --drop-unreferenced"——
		// 用户已经加过了。
		_, _ = fmt.Fprintf(stdout, "无人引用条目\t将删除 %d 个（--dry-run，未实际删除）\n", len(result.Unreferenced))
	case result.UnreferencedDropped > 0:
		_, _ = fmt.Fprintf(stdout, "无人引用条目\t已删除 %d 个（GB/T 42133 6.2.1 c)）\n", result.UnreferencedDropped)
	default:
		_, _ = fmt.Fprintf(stdout, "无人引用条目\t%d 个（未删除，加 --drop-unreferenced 执行）\n", len(result.Unreferenced))
	}
}
