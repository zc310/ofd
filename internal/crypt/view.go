package crypt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/pkg/spec"
)

type viewNode struct {
	meta      container.Entry
	checked   bool
	operation string
	read      func(context.Context) ([]byte, error)
}
type viewKey struct {
	fek, iv   []byte
	recipient Recipient
	scheme    string
}

// View 是不可变的逻辑条目视图，仅保留密文数据源和恢复的密钥，
// 不保存明文 ZIP 或解密后的资源文件缓存。
// 打开时建立并校验索引，后续每次读取都重新解密。
type View struct {
	mu                             sync.RWMutex
	ownerClosed, released          bool
	refs                           int
	physical                       *container.Package
	inputSize                      int64
	closeInput                     func() error
	opt                            OpenOptions
	info                           *Info
	nodes                          map[string]*viewNode
	keys                           []*viewKey
	results                        []OperationResult
	removed                        map[string]bool
	restored                       map[string]string
	workMu                         sync.Mutex
	processed                      int64
	attempts, recipients, mappings int
	metadataRemaining              int64
}

// OpenView 借用 pkg，直至最后一个租约关闭。若提供 closeInput，
// 仅在成功时接管输入资源；失败时仍由调用方关闭 pkg。
func OpenView(ctx context.Context, pkg *container.Package, opt OpenOptions, closeInput func() error) (view *View, err error) {
	opt.DecryptOptions, err = NormalizeDecryptOptions(opt.DecryptOptions)
	if err != nil {
		return nil, err
	}
	s := &inspector{descriptorOnly: true}
	info, err := inspectWith(ctx, pkg, InspectOptions{Mode: opt.Mode, Limits: opt.Limits}, s)
	if err != nil {
		return nil, err
	}
	if !info.Encrypted {
		if err := s.indexEntries(pkg.Entries()); err != nil {
			return nil, err
		}
	}
	if err := s.checkInputBudget(); err != nil {
		return nil, err
	}
	v := &View{refs: 1, physical: pkg, inputSize: pkg.InputSize(), opt: opt, info: info, nodes: map[string]*viewNode{}, removed: map[string]bool{}, restored: map[string]string{}, metadataRemaining: s.remaining, processed: opt.Limits.MaxTotalMetadataBytes - s.remaining}
	defer func() {
		if err != nil {
			v.erase()
		}
	}()
	maxEntryBytes := opt.Limits.MaxEntryBytes
	for name, entry := range s.files {
		if entry.IsDir {
			continue
		}
		v.nodes[name] = &viewNode{meta: entry, read: func(ctx context.Context) ([]byte, error) {
			r, err := pkg.OpenEntry(entry)
			if err != nil {
				return nil, err
			}
			probe := maxEntryBytes
			if probe < 1<<63-1 {
				probe++
			}
			data, readErr := io.ReadAll(io.LimitReader(contextReader{ctx, r}, probe))
			closeErr := r.Close()
			if readErr == nil {
				readErr = closeErr
			}
			if ctx.Err() != nil {
				readErr = ctx.Err()
			}
			if readErr == nil && int64(len(data)) > maxEntryBytes {
				readErr = limitError(entry.Path, "单条读取超过上限")
			}
			if readErr != nil {
				clear(data)
				return nil, readErr
			}
			return data, nil
		}}
	}
	order, err := layerOrder(info.Operations, opt.Limits.MaxLayers)
	if err != nil {
		return nil, err
	}
	complete := map[string]bool{}
	for _, index := range order {
		op := &info.Operations[index]
		blocked := false
		for _, child := range info.Operations {
			if child.Relative == op.ID && !complete[child.ID] {
				blocked = true
			}
		}
		if blocked {
			op.State = "locked"
			op.Deferred = true
			info.Opaque = true
			v.results = append(v.results, OperationResult{ID: op.ID, State: "locked"})
			continue
		}
		result, err := v.unlockOperation(ctx, op)
		if err != nil {
			return nil, err
		}
		op.State = result.State
		complete[op.ID] = result.State == "unlocked"
		v.results = append(v.results, result)
	}
	if info.Encrypted {
		info.State = "unlocked"
		unlocked := false
		locked := false
		for _, r := range v.results {
			if r.DecryptedEntries > 0 {
				unlocked = true
			}
			if r.State != "unlocked" {
				locked = true
			}
		}
		if locked {
			info.State = "locked"
			if unlocked {
				info.State = "partially_unlocked"
			}
		}
	}
	delete(v.nodes, info.EntryPoint)
	if info.Encrypted {
		v.removed[info.EntryPoint] = true
	}
	if !info.Opaque {
		if err := v.checkLogicalSize(); err != nil {
			return nil, err
		}
		if info.State == "unlocked" || info.State == "none" {
			root, ok := v.nodes[spec.RootDocument]
			if !ok {
				return nil, invalid(spec.RootDocument, "恢复后仍缺少主入口")
			}
			data, err := v.load(ctx, root)
			if err != nil {
				return nil, err
			}
			guard := newProtection(&inspector{ctx: ctx, opt: InspectOptions{Mode: opt.Mode, Limits: opt.Limits}}, nil, nil)
			err = guard.observe(spec.RootDocument, data)
			clear(data)
			if err != nil {
				return nil, err
			}
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	v.closeInput = closeInput
	// 恢复的密钥已足够用于后续读取，无需保留调用方的口令、私钥句柄，
	// 或凭据解析器闭包捕获的对象。
	v.opt.Password = nil
	v.opt.PrivateKey = nil
	v.opt.Resolve = nil
	return v, nil
}

func layerOrder(operations []Operation, maximum int) ([]int, error) {
	byID := map[string]int{}
	for i, op := range operations {
		byID[op.ID] = i
	}
	for _, op := range operations {
		depth := 0
		for id := op.ID; id != ""; {
			depth++
			if depth > maximum {
				return nil, limitError(EntryPoint, "加密依赖深度超过上限")
			}
			i, ok := byID[id]
			if !ok {
				return nil, invalid(EntryPoint, "悬空 Relative")
			}
			id = operations[i].Relative
		}
	}
	var order []int
	done := map[string]bool{}
	for len(order) < len(operations) {
		progress := false
		for i, op := range operations {
			if done[op.ID] {
				continue
			}
			ready := true
			for _, child := range operations {
				if child.Relative == op.ID && !done[child.ID] {
					ready = false
					break
				}
			}
			if ready {
				done[op.ID] = true
				order = append(order, i)
				progress = true
			}
		}
		if !progress {
			return nil, invalid(EntryPoint, "Relative 循环")
		}
	}
	return order, nil
}

func (v *View) related(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	parent := map[string]string{}
	for _, op := range v.info.Operations {
		parent[op.ID] = op.Relative
	}
	for id := a; id != ""; id = parent[id] {
		if id == b {
			return true
		}
	}
	for id := b; id != ""; id = parent[id] {
		if id == a {
			return true
		}
	}
	return false
}

func (v *View) unlockOperation(ctx context.Context, op *Operation) (result OperationResult, err error) {
	result = OperationResult{ID: op.ID, State: "locked"}
	pkg, err := v.virtual(ctx, v.nodes, nil)
	if err != nil {
		return result, err
	}
	defer pkg.Close()
	s := &inspector{ctx: ctx, pkg: pkg, opt: InspectOptions{Mode: v.opt.Mode, Limits: v.opt.Limits}, info: v.info, files: map[string]container.Entry{}, remaining: v.metadataRemaining, seeds: map[string]SeedInfo{}, rawSeeds: map[string]seedXML{}, mapData: map[string][]byte{}, inputSeen: map[string]bool{}, inputRemaining: v.opt.Limits.MaxTotalBytes, decrypting: true, allowLayers: true, recipients: v.recipients, mappings: v.mappings, attempts: &v.attempts}
	defer func() { v.metadataRemaining = s.remaining; v.recipients = s.recipients; v.mappings = s.mappings }()
	if err := s.indexEntries(pkg.Entries()); err != nil {
		return result, err
	}
	op.Deferred = false
	op.Seed, err = s.seed(op.DecryptSeedLoc, op.ID)
	if err != nil {
		return result, err
	}
	if err := s.entries(op); err != nil {
		return result, err
	}
	result.Scheme = op.Seed.Scheme
	cache := map[string]*viewKey{}
	keyFor := func(target *Operation) (*viewKey, error) {
		if key, ok := cache[target.DecryptSeedLoc]; ok {
			if key == nil {
				return nil, ErrCredentialsRequired
			}
			return key, nil
		}
		options := v.opt.DecryptOptions
		if options.Resolve != nil {
			credentials, err := options.Resolve(ctx, CredentialRequest{OperationID: op.ID, DecryptSeedLoc: target.DecryptSeedLoc, Scheme: target.Seed.Scheme, Recipients: append([]Recipient(nil), target.Seed.Recipients...)})
			if err != nil {
				if missingCredentials(err) {
					cache[target.DecryptSeedLoc] = nil
				}
				return nil, err
			}
			options.Password = credentials.Password
			options.PrivateKey = credentials.PrivateKey
			options.UserName = credentials.UserName
			options.MissingScheme = credentials.MissingScheme
		}
		options, err = NormalizeDecryptOptions(options)
		if err != nil {
			return nil, err
		}
		fek, iv, recipient, scheme, err := s.unlock(target, options)
		if err != nil {
			if missingCredentials(err) {
				cache[target.DecryptSeedLoc] = nil
			}
			return nil, err
		}
		key := &viewKey{fek, iv, recipient, scheme}
		v.keys = append(v.keys, key)
		cache[target.DecryptSeedLoc] = key
		result.Keys = append(result.Keys, SeedResult{DecryptSeedLoc: target.DecryptSeedLoc, Scheme: scheme, Recipient: recipient})
		if target.DecryptSeedLoc == op.DecryptSeedLoc {
			result.Scheme = scheme
			result.Recipient = recipient
		}
		return key, nil
	}
	var common *viewKey
	if op.MapState == "encrypted" {
		common, err = keyFor(op)
		if err != nil {
			if v.opt.AllowPartial && missingCredentials(err) {
				v.info.Opaque = true
				return result, nil
			}
			return result, err
		}
		result.Scheme = common.scheme
		result.Recipient = common.recipient
	}
	entryKeys := map[string]*viewKey{}
	for i := range op.Entries {
		entry := &op.Entries[i]
		seedLoc := op.DecryptSeedLoc
		if entry.DecryptSeedLoc != "" {
			seedLoc = entry.DecryptSeedLoc
		}
		target := op
		if seedLoc != op.DecryptSeedLoc {
			seed, err := s.seed(seedLoc, op.ID)
			if err != nil {
				return result, err
			}
			entry.Seed = &seed
			target = &Operation{ID: op.ID, DecryptSeedLoc: seedLoc, Seed: seed, MapState: "plaintext"}
		}
		key := common
		err = nil
		if seedLoc != op.DecryptSeedLoc || key == nil {
			key, err = keyFor(target)
		}
		if err != nil {
			if !v.opt.AllowPartial || !missingCredentials(err) {
				return result, err
			}
			key = nil
		}
		entryKeys[entry.Path] = key
	}
	outputs, removed, restored, err := s.plan(op)
	if err != nil {
		return result, err
	}
	next := map[string]*viewNode{}
	for _, name := range outputs {
		if _, ok := restored[name]; !ok {
			next[name] = v.nodes[name]
		}
	}
	for _, entry := range op.Entries {
		source, ok := v.nodes[entry.EPath]
		if !ok {
			return result, invalid(entry.EPath, "映射引用的密文缺失")
		}
		if !v.related(source.operation, op.ID) {
			return result, invalid(entry.EPath, "密文依赖未通过 Relative 声明")
		}
		if existing := v.nodes[entry.Path]; existing != nil && !v.related(existing.operation, op.ID) {
			return result, invalid(entry.Path, "独立操作的恢复范围冲突")
		}
		for _, prior := range v.info.Operations {
			if prior.ID == op.ID {
				continue
			}
			for _, m := range prior.Entries {
				if m.Path == entry.Path && !v.related(prior.ID, op.ID) {
					return result, invalid(entry.Path, "独立操作的映射范围冲突")
				}
			}
		}
		key := entryKeys[entry.Path]
		meta := container.Entry{Name: entry.Path, Path: entry.Path, Virtual: true}
		node := &viewNode{meta: meta, operation: op.ID}
		if key == nil {
			node.meta.Locked = true
			node.meta.SizeUnknown = true
			node.read = func(context.Context) ([]byte, error) { return nil, ErrLocked }
			result.LockedEntries++
		} else {
			node.read = func(ctx context.Context) ([]byte, error) {
				data, err := v.load(ctx, source)
				if err != nil {
					return nil, err
				}
				plain, err := decryptCBC(ctx, data, key.fek, key.iv)
				if err != nil {
					clear(data)
					return nil, err
				}
				return plain, nil
			}
			data, err := v.load(ctx, node)
			if err != nil {
				return result, fmt.Errorf("%w: %s", err, entry.Path)
			}
			node.meta.UncompressedSize = uint64(len(data))
			node.meta.CRC32 = crc32.ChecksumIEEE(data)
			node.checked = true
			clear(data)
			result.DecryptedEntries++
		}
		next[entry.Path] = node
		v.restored[entry.Path] = entry.EPath
	}
	for name := range removed {
		v.removed[name] = true
	}
	v.nodes = next
	result.State = "unlocked"
	if result.LockedEntries > 0 {
		result.State = "partially_unlocked"
		if result.DecryptedEntries == 0 {
			result.State = "locked"
		}
	}
	return result, v.checkLogicalSize()
}

func (v *View) checkLogicalSize() error {
	var total uint64
	if len(v.nodes) > v.opt.Limits.MaxEntries {
		return limitError("", "逻辑条目数超过上限")
	}
	for name, n := range v.nodes {
		if n.meta.UncompressedSize > uint64(v.opt.Limits.MaxEntryBytes) || n.meta.UncompressedSize > uint64(v.opt.Limits.MaxTotalBytes)-total {
			return limitError(name, "逻辑条目规模超过上限")
		}
		total += n.meta.UncompressedSize
	}
	return nil
}

func (v *View) load(ctx context.Context, n *viewNode) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	v.workMu.Lock()
	exhausted := v.processed >= v.opt.Limits.MaxProcessedBytes
	v.workMu.Unlock()
	if exhausted {
		return nil, limitError(n.meta.Path, "累计处理字节数超过上限")
	}
	data, err := n.read(ctx)
	if err != nil {
		return nil, err
	}
	if n.checked && (uint64(len(data)) != n.meta.UncompressedSize || crc32.ChecksumIEEE(data) != n.meta.CRC32) {
		clear(data)
		return nil, fmt.Errorf("%w: %s 的内容已偏离打开时的索引", ErrDecryptFailed, n.meta.Path)
	}
	v.workMu.Lock()
	remaining := v.opt.Limits.MaxProcessedBytes - v.processed
	if int64(len(data)) > remaining {
		v.processed = v.opt.Limits.MaxProcessedBytes
		v.workMu.Unlock()
		clear(data)
		return nil, limitError(n.meta.Path, "累计处理字节数超过上限")
	}
	v.processed += int64(len(data))
	v.workMu.Unlock()
	return data, nil
}

func (v *View) virtual(ctx context.Context, nodes map[string]*viewNode, closer io.Closer) (*container.Package, error) {
	names := make([]string, 0, len(nodes))
	for name := range nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]container.Entry, 0, len(names))
	for _, name := range names {
		entry := nodes[name].meta
		entry.Virtual = true
		entry.CompressedSize = 0
		entry.Method = 0
		entries = append(entries, entry)
	}
	return container.NewVirtual(entries, func(name string) (io.ReadCloser, error) {
		v.mu.RLock()
		defer v.mu.RUnlock()
		if v.released {
			return nil, container.ErrPackageClosed
		}
		n := nodes[name]
		if n == nil {
			return nil, os.ErrNotExist
		}
		data, err := v.load(ctx, n)
		if err != nil {
			return nil, err
		}
		return &clearingReader{Reader: bytes.NewReader(data), data: data}, nil
	}, closer)
}

type clearingReader struct {
	*bytes.Reader

	data   []byte
	closed bool
}

func (r *clearingReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, container.ErrPackageClosed
	}
	return r.Reader.Read(p)
}
func (r *clearingReader) Close() error {
	if !r.closed {
		r.closed = true
		clear(r.data)
		r.data = nil
	}
	return nil
}

type closeFunc func() error

func (f closeFunc) Close() error { return f() }

// OFDPackage 返回供使用方持有的独立租约。部分解锁的视图不能传给文档处理器，
// 可通过 Read/Entries 检查其中可用的条目。
func (v *View) OFDPackage() (*container.Package, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.ownerClosed || v.released {
		return nil, container.ErrPackageClosed
	}
	if v.info.State != "unlocked" && v.info.State != "none" {
		return nil, &LockedError{Info: cloneInfo(v.info)}
	}
	v.refs++
	pkg, err := v.virtual(context.Background(), v.nodes, closeFunc(v.release))
	if err != nil {
		v.refs--
	}
	return pkg, err
}

// OpenPhysicalPackage 提供原始 ZIP 的租约，供审计使用。
// 返回物理条目的元数据，不以解密后的虚拟条目替代。
func (v *View) OpenPhysicalPackage() (*container.Package, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.ownerClosed || v.released {
		return nil, container.ErrPackageClosed
	}
	v.refs++
	entries := v.physical.Entries()
	pkg, err := container.NewVirtual(entries, func(name string) (io.ReadCloser, error) {
		v.mu.RLock()
		defer v.mu.RUnlock()
		if v.released {
			return nil, container.ErrPackageClosed
		}
		return v.physical.Open(name)
	}, closeFunc(v.release))
	if err != nil {
		v.refs--
	}
	return pkg, err
}

func (v *View) EncryptionInfo() *Info { v.mu.RLock(); defer v.mu.RUnlock(); return cloneInfo(v.info) }
func (v *View) InputSize() int64      { v.mu.RLock(); defer v.mu.RUnlock(); return v.inputSize }
func (v *View) Entries() []container.Entry {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.ownerClosed || v.info.Opaque {
		return nil
	}
	names := make([]string, 0, len(v.nodes))
	for n := range v.nodes {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]container.Entry, 0, len(names))
	for _, n := range names {
		e := v.nodes[n].meta
		e.Virtual = true
		e.CompressedSize = 0
		e.Method = 0
		out = append(out, e)
	}
	return out
}
func (v *View) Read(name string) ([]byte, error) { return v.ReadContext(context.Background(), name) }
func (v *View) ReadContext(ctx context.Context, name string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.ownerClosed || v.released {
		return nil, container.ErrPackageClosed
	}
	if v.info.Opaque {
		return nil, ErrLocked
	}
	name = strings.TrimPrefix(name, "/")
	n := v.nodes[name]
	if n == nil {
		return nil, os.ErrNotExist
	}
	return v.load(ctx, n)
}

func missingCredentials(err error) bool {
	return errors.Is(err, ErrCredentialsRequired) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, ErrDecryptFailed) && !errors.Is(err, ErrLimitExceeded) && !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrInvalidMetadata) && !errors.Is(err, ErrProtectedContent) && !errors.Is(err, container.ErrPackageClosed)
}

func (v *View) Export(ctx context.Context, emit func(string, []byte) error) (*DecryptReport, error) {
	v.mu.Lock()
	if v.ownerClosed || v.released {
		v.mu.Unlock()
		return nil, container.ErrPackageClosed
	}
	if v.info.State != "unlocked" && v.info.State != "none" {
		info := cloneInfo(v.info)
		v.mu.Unlock()
		return nil, &LockedError{Info: info}
	}
	v.refs++
	v.mu.Unlock()
	defer v.release()
	report := &DecryptReport{State: v.info.State, Operations: cloneResults(v.results), Diagnostics: append([]Diagnostic(nil), v.info.Diagnostics...)}
	if len(v.results) > 0 {
		report.OperationID = v.results[0].ID
		report.Scheme = v.results[0].Scheme
		report.Recipient = v.results[0].Recipient
	}
	for _, r := range v.results {
		report.DecryptedEntries += r.DecryptedEntries
	}
	s := &inspector{ctx: ctx, opt: InspectOptions{Mode: v.opt.Mode, Limits: v.opt.Limits}}
	guard := newProtection(s, v.removed, v.restored)
	names := make([]string, 0, len(v.nodes))
	for name := range v.nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.EqualFold(name, spec.IntegrityEntryPoint) {
			return nil, fmt.Errorf("%w: OFDEntries.xml 需要重建", ErrProtectedContent)
		}
		data, err := v.load(ctx, v.nodes[name])
		if err != nil {
			return nil, err
		}
		err = func() error {
			defer clear(data)
			if err := guard.observe(name, data); err != nil {
				return err
			}
			if err := emit(name, data); err != nil {
				return err
			}
			report.OutputEntries++
			report.OutputBytes += int64(len(data))
			return nil
		}()
		if err != nil {
			return nil, err
		}
	}
	if err := guard.check(); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return report, nil
}

func (v *View) erase() {
	for _, k := range v.keys {
		clear(k.fek)
		clear(k.iv)
	}
	v.keys = nil
}
func (v *View) release() error {
	v.mu.Lock()
	v.refs--
	if v.refs != 0 {
		v.mu.Unlock()
		return nil
	}
	v.released = true
	v.erase()
	closeInput := v.closeInput
	v.closeInput = nil
	v.nodes = nil
	v.physical = nil
	v.removed = nil
	v.restored = nil
	v.mu.Unlock()
	if closeInput != nil {
		return closeInput()
	}
	return nil
}
func (v *View) Close() error {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	if v.ownerClosed {
		v.mu.Unlock()
		return nil
	}
	v.ownerClosed = true
	v.mu.Unlock()
	return v.release()
}

func cloneInfo(info *Info) *Info {
	if info == nil {
		return nil
	}
	out := *info
	out.Diagnostics = append([]Diagnostic(nil), info.Diagnostics...)
	out.Operations = append([]Operation(nil), info.Operations...)
	for i := range out.Operations {
		op := &out.Operations[i]
		op.Seed.Recipients = append([]Recipient(nil), op.Seed.Recipients...)
		op.Entries = append([]Mapping(nil), op.Entries...)
		for j := range op.Entries {
			if op.Entries[j].Seed != nil {
				seed := *op.Entries[j].Seed
				seed.Recipients = append([]Recipient(nil), seed.Recipients...)
				op.Entries[j].Seed = &seed
			}
		}
	}
	return &out
}

func cloneResults(results []OperationResult) []OperationResult {
	out := append([]OperationResult(nil), results...)
	for i := range out {
		out[i].Keys = append([]SeedResult(nil), out[i].Keys...)
	}
	return out
}
