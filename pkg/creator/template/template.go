// Package template 根据导出的 JSON/YAML 创作清单与 JSON 数据生成固定版式 OFD 文档。
package template

import (
	"io"

	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/pkg/creator"
)

// Template 是可复用的创作清单模板；Render 不修改模板，允许并发调用。
type Template struct {
	manifest manifest.Manifest
	baseDir  string
}

// Options 控制模板资源加载与生成规模。
type Options struct {
	// AssetRoot 覆盖资源根目录；相对路径以模板目录为基准。
	AssetRoot string
	// MaxPages 限制展开后的页数；0 使用 10000。
	MaxPages int
	// LoadAsset 可从内存加载相对路径资源；为空时读取资源根目录下的文件。
	LoadAsset func(name string) ([]byte, error)
	// StreamAssets 为普通静态资源使用惰性数据来源。
	StreamAssets bool
}

// Load 根据文件扩展名读取 JSON/YAML 模板，并记录资源的基准目录。
func Load(path string) (*Template, error) {
	m, baseDir, err := manifest.Load(path, "auto")
	if err != nil {
		return nil, err
	}
	return &Template{manifest: m, baseDir: baseDir}, nil
}

// Parse 从内存解析模板；format 为 json 或 yaml，baseDir 是文件资源的基准目录。
// 所有资源内联或通过 Options.LoadAsset 提供时，baseDir 可为空。
func Parse(data []byte, format, baseDir string) (*Template, error) {
	m, err := manifest.Parse(data, format)
	if err != nil {
		return nil, err
	}
	return &Template{manifest: m, baseDir: baseDir}, nil
}

// Render 绑定单个 JSON 对象，返回可由 creator.Create/Marshal 写出的文档模型。
// 数字保持原有十进制表示；字段、区域、缺字或资源错误均在返回前定位报告。
func (t *Template) Render(data io.Reader, options Options) (creator.Document, error) {
	return t.manifest.RenderTemplate(data, t.baseDir, options.AssetRoot, manifest.TemplateOptions{
		BuildOptions: manifest.BuildOptions{LoadAsset: options.LoadAsset, StreamAssets: options.StreamAssets},
		MaxPages:     options.MaxPages,
	})
}
