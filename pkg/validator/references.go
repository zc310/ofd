package validator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"sync"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/crypt"
	"github.com/zc310/ofd/internal/schema"
	"github.com/zc310/ofd/pkg/spec"
)

var errReferenceContext = errors.New("引用分析上下文为空")

// MissingReference 描述一处指向包内不存在文件的引用。
type MissingReference struct {
	// From 是引用所在条目。
	From string
	// Value 是引用处的原始文本或属性值。
	Value string
	// Path 是解析后的目标路径。
	Path string
}

// String 按单行输出，便于报告直接引用。
func (m MissingReference) String() string {
	return fmt.Sprintf("%s 引用 %s（解析为 %s）", m.From, m.Value, m.Path)
}

// ReferenceIndex 是包内文件引用关系的索引。
//
// 它由与文件引用校验同一套逻辑得出：collectReferences 负责识别引用，
// resolvePackagePath 负责解析路径。两者都不在此处另写一份，否则「校验通过」
// 与「闭包完整」两个结论会各自漂移，删除无人引用的条目就可能误删在用文件。
type ReferenceIndex struct {
	// Reachable 是从 OFD.xml 出发、经嵌套引用可达的条目集合，含 OFD.xml 本身。
	// 目录条目不在其中。
	Reachable map[string]bool
	// Missing 是引用了但包内不存在的条目。它们不改变可达集合，但意味着
	// 输入本身有悬空引用，判断「某条目是否无人引用」时要留意这一前提。
	Missing []MissingReference
	// Escaped 是越过包根目录的非法路径引用。
	Escaped []string
	// Unparsed 是无法完整确认引用结构的 XML，包含解析、命名空间、根元素或模式失败。
	// 已识别的引用仍可列入 Reachable，但不能据此删除其他条目。
	//
	// 这一项直接决定能否删除条目：testdata/ofdrw/intro.ofd 的命名空间缺
	// "/2016" 后缀，解析失败后它引用的 74 个字体会全部落进「无人引用」，
	// 照单删除就是在真实文件上毁数据。
	Unparsed []string
}

// Complete 报告引用闭包是否可据以删除条目。
//
// 三类情况都会让闭包不可信：引用缺失、路径越界、被引用文件无法解析。只有
// 三者皆空时，「某条目不在可达集合中」才等价于「确实无人引用它」。
func (r *ReferenceIndex) Complete() bool {
	return len(r.Missing) == 0 && len(r.Escaped) == 0 && len(r.Unparsed) == 0
}

// IncompleteReason 返回闭包不可信的首要原因，供报告直接展示；闭包完整时返回空串。
func (r *ReferenceIndex) IncompleteReason() string {
	switch {
	case len(r.Unparsed) > 0:
		return fmt.Sprintf("有 %d 个被引用的 XML 无法解析，其引用未能收集，其中 %s",
			len(r.Unparsed), r.Unparsed[0])
	case len(r.Missing) > 0:
		return fmt.Sprintf("有 %d 处引用指向不存在的文件，其中 %s",
			len(r.Missing), r.Missing[0])
	case len(r.Escaped) > 0:
		return fmt.Sprintf("有 %d 处引用路径非法，其中 %s", len(r.Escaped), r.Escaped[0])
	}
	return ""
}

// Unreachable 列出包内不在可达集合中的条目，按字典序返回。
// 目录条目被排除，删除它们没有意义。
func (r *ReferenceIndex) Unreachable(archive *container.Package) []string {
	var out []string
	for _, entry := range archive.Entries() {
		if entry.IsDir || r.Reachable[entry.Path] {
			continue
		}
		out = append(out, entry.Path)
	}
	return out
}

// PackageReferences 解析 input 的引用闭包。
//
// 遍历检查已解释 XML 的模式和引用，不执行内容语义与摘要校验。
// 未能通过模式确认的 XML 记入 Unparsed，不能据此得出完整闭包。
func PackageReferences(ctx context.Context, input any, options ...Option) (*ReferenceIndex, error) {
	return PackageReferencesWithOverrides(ctx, input, nil, options...)
}

// PackageReferencesWithOverrides 在引用分析中用 overrides 替换既有条目的内容，
// 不修改输入包。键必须是包内规范路径且已存在；替换内容同样受文件和总大小限制。
// 用于转换后、实际写回前计算引用闭包，沿用校验器的引用识别和完整性检查。
func PackageReferencesWithOverrides(ctx context.Context, input any, overrides map[string][]byte, options ...Option) (*ReferenceIndex, error) {
	v, err := New(options...)
	if err != nil {
		return nil, err
	}
	return v.packageReferences(ctx, input, overrides)
}

// packageReferences 执行引用闭包遍历。
func (v *Validator) packageReferences(ctx context.Context, input any, overrides map[string][]byte) (*ReferenceIndex, error) {
	resolver, err := v.newReferenceResolver(ctx, input)
	if err != nil {
		return nil, err
	}
	return resolver.Resolve(ctx, overrides)
}

// ReferenceResolver 持有受规模限制的只读包索引，供批量变换重复分析文件引用。
// 原始条目只解压一次，未修改的 XML 解析结果按文件及预期根类型缓存。
// Resolve 可并发调用，内部串行处理；不持有输入文件句柄。
type ReferenceResolver struct {
	mu        sync.Mutex
	validator *Validator
	archive   *packageIndex
	cache     map[[2]string]referenceParse
}

type referenceParse struct {
	refs      []fileReference
	valid     bool
	validated bool
}

// NewReferenceResolver 建立批量引用分析器，输入类型与 PackageReferences 相同。
// 只索引文件；每次 Resolve 仍须检查 Complete，不能根据不完整闭包删除条目。
func NewReferenceResolver(ctx context.Context, input any, options ...Option) (*ReferenceResolver, error) {
	v, err := New(options...)
	if err != nil {
		return nil, err
	}
	return v.newReferenceResolver(ctx, input)
}

func (v *Validator) newReferenceResolver(ctx context.Context, input any) (*ReferenceResolver, error) {
	if ctx == nil {
		return nil, errReferenceContext
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	v.opts.CheckXSD = true
	if v.opts.Schemas == nil {
		var err error
		v.opts.Schemas, err = schema.Default()
		if err != nil {
			return nil, err
		}
	}
	pkg, owned, err := v.openInput(input)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = pkg.Close() }()
	}
	if crypt.Present(pkg) {
		info, err := crypt.InspectPackage(ctx, pkg, crypt.InspectOptions{Mode: crypt.ModeCompat})
		return nil, &crypt.LockedError{Info: info, Cause: err}
	}

	// 引用解析不产生报告，丢弃即可；此处只用它来满足 indexArchive 的签名。
	discard := &Report{}
	archive, err := v.indexArchive(ctx, pkg, discard)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &ReferenceResolver{validator: v, archive: archive, cache: make(map[[2]string]referenceParse)}, nil
}

// Resolve 以 overrides 中的内容替换既有文件后计算引用闭包，不改变原始索引。
// 文件路径、大小及未知 XML 的处理与 PackageReferencesWithOverrides 一致。
func (r *ReferenceResolver) Resolve(ctx context.Context, overrides map[string][]byte) (*ReferenceIndex, error) {
	if ctx == nil {
		return nil, errReferenceContext
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.validator
	archive := &packageIndex{files: maps.Clone(r.archive.files)}
	discard := &Report{}
	for name, data := range overrides {
		if err := container.ValidateEntryName(name); err != nil {
			return nil, err
		}
		file, exists := archive.files[name]
		if !exists || file.isDir {
			return nil, fmt.Errorf("引用分析替换的条目不存在或为目录: %s", name)
		}
		if v.opts.MaxFileSize > 0 && int64(len(data)) > v.opts.MaxFileSize {
			return nil, fmt.Errorf("引用分析替换的条目 %s 超过单条大小上限", name)
		}
		file.data = data
		archive.files[name] = file
	}
	if v.opts.MaxTotalSize > 0 {
		remaining := uint64(v.opts.MaxTotalSize)
		for _, file := range archive.files {
			if uint64(len(file.data)) > remaining {
				return nil, fmt.Errorf("引用分析替换后的解压总大小超过上限")
			}
			remaining -= uint64(len(file.data))
		}
	}
	if _, ok := archive.get(spec.RootDocument); !ok {
		return nil, fmt.Errorf("缺少必需的 %s 文件", spec.RootDocument)
	}

	// Reachable 先记入入口，但 visited 必须留空：预置 visited 会让入口在出队
	// 时被当成已处理而跳过，导致整个遍历一次文件都不解析。
	index := &ReferenceIndex{Reachable: map[string]bool{spec.RootDocument: true}}
	visited := map[string]bool{}

	type item struct {
		path     string
		expected string
	}
	queue := []item{{path: spec.RootDocument, expected: "OFD"}}

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		current := queue[0]
		queue = queue[1:]
		if visited[current.path] {
			continue
		}
		visited[current.path] = true

		file, ok := archive.get(current.path)
		if !ok {
			continue
		}
		key := [2]string{current.path, current.expected}
		parsed, cached := r.cache[key]
		_, changed := overrides[current.path]
		if !cached || changed {
			doc, refs, valid := v.parseXML(file, current.expected, "", "", discard)
			parsed = referenceParse{refs: refs, valid: doc != nil && valid, validated: doc != nil && doc.validated}
			if !changed {
				r.cache[key] = parsed
			}
		}
		if !parsed.valid {
			// 根元素或命名空间不符时该文件的引用收集不到。记下来而不是当作
			// 「无引用」——否则它引用的资源会被误判为无人引用。
			index.Unparsed = append(index.Unparsed, current.path)
			continue
		}
		if !parsed.validated {
			// 仍保留已识别引用以减少误导性候选，但不能给出完整闭包。
			index.Unparsed = append(index.Unparsed, current.path)
		}
		for _, ref := range parsed.refs {
			resolved, resolveErr := resolvePackagePath(ref.base, ref.value)
			if resolveErr != nil {
				// 越界或非法路径：尝试产物怪癖的回退基准，仍失败则记为越界。
				if ref.fallbackBase == "" {
					index.Escaped = append(index.Escaped,
						fmt.Sprintf("%s: %v", ref.from, resolveErr))
					continue
				}
				fallback, fallbackErr := resolvePackagePath(ref.fallbackBase, ref.value)
				if fallbackErr != nil || !archive.has(fallback) {
					index.Escaped = append(index.Escaped,
						fmt.Sprintf("%s: %v", ref.from, resolveErr))
					continue
				}
				resolved, resolveErr = fallback, nil
			}
			if !archive.has(resolved) {
				index.Missing = append(index.Missing, MissingReference{
					From: ref.from, Value: ref.value, Path: resolved,
				})
				continue
			}
			index.Reachable[resolved] = true
			// 只有 XML 才继续下探；字体、图像等是叶子。
			if ref.checkXML && ref.expected != "" && !visited[resolved] {
				scope := ref.scope
				if scope == "" && ref.expected == "Document" {
					scope = documentScope(resolved)
				}
				queue = append(queue, item{path: resolved, expected: ref.expected})
			}
		}
	}
	return index, nil
}

// openInput 把各种输入形态统一成包，第二个返回值表示包是否由本函数创建。
// 遍历需要随机访问条目，故不接受 io.Reader。传入已打开的包时所有权仍归调用方，
// 本函数不会关闭它。
func (v *Validator) openInput(input any) (*container.Package, bool, error) {
	switch value := input.(type) {
	case container.PackageSource:
		pkg, err := value.OFDPackage()
		return pkg, true, err
	case string:
		pkg, err := container.OpenFile(value)
		return pkg, true, err
	case []byte:
		pkg, err := container.OpenBytes(value)
		return pkg, true, err
	case *container.Package:
		return value, false, nil
	case io.ReaderAt:
		return nil, false, fmt.Errorf("引用闭包需要可随机访问的输入，请传入路径或字节")
	default:
		return nil, false, fmt.Errorf("不支持的输入类型 %T", input)
	}
}
