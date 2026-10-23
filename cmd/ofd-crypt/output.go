package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func validatePaths(o *options, protected []string) error {
	input, err := os.Stat(o.input)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return usage("输入文件不存在")
		}
		return err
	}
	if !input.Mode().IsRegular() {
		return usage("输入 OFD 必须为普通文件")
	}
	if o.output == "" || o.output == "-" {
		return nil
	}
	out, err := os.Lstat(o.output)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if out != nil {
		if !out.Mode().IsRegular() {
			return usage("输出必须为普通文件，不能是目录或符号链接")
		}
		if !o.force {
			return usage("输出已存在；如需替换请指定 --force")
		}
	}
	outputAbs, err := filepath.Abs(o.output)
	if err != nil {
		return err
	}
	for _, name := range append([]string{o.input}, protected...) {
		if name == "" || name == "-" {
			continue
		}
		absolute, err := filepath.Abs(name)
		if err != nil {
			return err
		}
		if absolute == outputAbs {
			return usage("输出不能覆盖输入或凭据文件")
		}
		info, err := os.Stat(name)
		if err == nil && out != nil && os.SameFile(info, out) {
			return usage("输出与输入或凭据指向同一文件")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func atomicOutput(ctx context.Context, o *options, write func(io.Writer) error) error {
	if err := validatePaths(o, credentialPaths(o)); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(o.output), ".ofd-crypt-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	defer file.Close()
	if err := write(file); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validatePaths(o, credentialPaths(o)); err != nil {
		return err
	}
	if o.force {
		return os.Rename(name, o.output)
	}
	// A link commits the complete file without a check-then-rename overwrite
	// race. An unsupported filesystem returns an error, never a non-atomic copy.
	if err := os.Link(name, o.output); err != nil {
		return fmt.Errorf("提交输出失败: %w", err)
	}
	return nil
}
