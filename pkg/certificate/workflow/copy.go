package workflow

import (
	"bytes"
	"context"
	"fmt"
	"io"

	protocol "github.com/zc310/ofd/internal/crypt"
	permissionmodel "github.com/zc310/ofd/internal/permission"
	"github.com/zc310/ofd/internal/xmlcodec"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/replace"
	"github.com/zc310/ofd/pkg/permission"
	"github.com/zc310/ofd/pkg/sign"
	"github.com/zc310/ofd/pkg/spec"
)

// CreateCopy 按 GB/T 36905—2018 §7.2 b)、c) 验收源件、移除明确指定的锁定签名、
// 补入加注件信息、调用编辑器、保留业务签名并重新锁定。只在全部验收成功后写出。
// 编辑回调不得修改主入口、业务签名或已准备的加注件信息；原件文件不会被覆盖。
func CreateCopy(ctx context.Context, input []byte, w io.Writer, options CopyOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateVerifyOptions(options.Source); err != nil {
		return err
	}
	if err := validateLockOptions(options.Lock, w); err != nil {
		return err
	}
	if err := options.Metadata.validate(); err != nil {
		return err
	}
	if options.Edit == nil || options.Lock.Sign.ID == options.Source.LockID {
		return fmt.Errorf("%w: 需提供编辑器且新锁定 ID 不能复用旧 ID", ErrOptions)
	}
	input, err := freeze(input)
	if err != nil {
		return err
	}
	s, err := inspect(ctx, input)
	if err != nil {
		return err
	}
	defer s.close()
	if err := verifyLock(ctx, input, s, options.Source); err != nil {
		return err
	}
	// 按源声明而非编辑后声明检查权限，避免回调改写权限后自授权。
	policy := options.Lock.Sign.PermissionPolicy
	for _, operation := range []permission.Action{permission.Copy, permission.Edit, permission.Signature} {
		if err := permissionmodel.CheckPackage(s.pkg, policy, operation, nil); err != nil {
			return err
		}
	}
	var stripped bytes.Buffer
	if err := sign.Remove(ctx, input, &stripped, sign.RemoveOptions{
		IDs: []string{options.Source.LockID}, Deterministic: options.Lock.Sign.Deterministic, PermissionPolicy: policy,
	}); err != nil {
		return err
	}
	prepared, err := prepareCopy(ctx, stripped.Bytes(), options)
	if err != nil {
		return err
	}
	before, err := inspect(ctx, prepared)
	if err != nil {
		// 添加复制元数据已影响业务签名时，在编辑器执行前停止。
		return fmt.Errorf("%w: 加注件元数据会使保留的业务签名失效: %w", protocol.ErrProtectedContent, err)
	}
	defer before.close()
	edited, err := options.Edit(ctx, bytes.Clone(prepared))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	edited, err = freeze(edited)
	if err != nil {
		return err
	}
	after, err := inspect(ctx, edited)
	if err != nil {
		return err
	}
	defer after.close()
	if err := preserveBusiness(before, after); err != nil {
		return err
	}
	output, err := lock(ctx, edited, Copy, options.Lock)
	if err != nil {
		return err
	}
	for _, operation := range []permission.Action{permission.Copy, permission.Edit, permission.Signature} {
		if err := permissionmodel.CheckPackage(s.pkg, policy, operation, nil); err != nil {
			return err
		}
	}
	return publish(ctx, w, output)
}

func prepareCopy(ctx context.Context, input []byte, options CopyOptions) ([]byte, error) {
	s, err := inspect(ctx, input)
	if err != nil {
		return nil, err
	}
	defer s.close()
	s.setCopyMetadata(options.Metadata)
	root, err := xmlcodec.Marshal(s.root)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	//nolint:contextcheck // replace.Files 现有接口无 context 参数，本流程在调用前后检查取消。
	if err := replace.Files(input, []replace.Operation{{Kind: replace.OpSet, Name: spec.RootDocument, Data: root}}, &output, replace.Options{
		PermissionPolicy: options.Lock.Sign.PermissionPolicy, Signatures: creator.SignaturePreserve,
		Deterministic: options.Lock.Sign.Deterministic,
	}); err != nil {
		return nil, err
	}
	return output.Bytes(), ctx.Err()
}

func preserveBusiness(before, after *snapshot) error {
	if len(before.signatures) != len(after.signatures) || before.listName != after.listName || before.docRoot != after.docRoot {
		return fmt.Errorf("%w: 编辑器改变了业务签名列表或文档入口", protocol.ErrProtectedContent)
	}
	paths := []string{spec.RootDocument}
	if before.listName != "" {
		paths = append(paths, before.listName)
	}
	for index, record := range before.signatures {
		if record.ref != after.signatures[index].ref {
			return fmt.Errorf("%w: 编辑器改变了业务签名声明", protocol.ErrProtectedContent)
		}
		paths = append(paths, record.artifacts...)
	}
	for _, name := range paths {
		a, err := readExact(before.pkg, name, 4<<20)
		if err != nil {
			return err
		}
		b, err := readExact(after.pkg, name, 4<<20)
		if err != nil {
			return err
		}
		if !bytes.Equal(a, b) {
			return fmt.Errorf("%w: 编辑器改变了须保持原字节的 %s", protocol.ErrProtectedContent, name)
		}
	}
	changed := false
	for _, entry := range after.pkg.Entries() {
		if entry.IsDir {
			continue
		}
		prior, ok := before.pkg.Lookup(entry.Path)
		if !ok || prior.UncompressedSize != entry.UncompressedSize {
			changed = true
			break
		}
		a, err := readExact(before.pkg, entry.Path, 64<<20)
		if err != nil {
			return err
		}
		b, err := readExact(after.pkg, entry.Path, 64<<20)
		if err != nil {
			return err
		}
		if !bytes.Equal(a, b) {
			changed = true
			break
		}
	}
	if !changed {
		return fmt.Errorf("%w: 编辑器未产生可确认的加注修改", ErrInvalid)
	}
	return nil
}
