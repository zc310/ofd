package convertersvc

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// 产物文件名的生成规则。
//
// 命名集中在这里而不是散落在转换流程的各个分支里，是因为同一份规则要同时
// 服务于三种落点：本地目录、内存流、远端目标（FTP/S3/WebDAV/SFTP）。散开写的
// 后果是改了一处、另一处还叫 output.pdf，而调用方拿到的路径对不上。

const (
	// defaultBaseName 是未指定文件名时单文件产物的基名。
	defaultBaseName = "output"
	// defaultPageBaseName 是逐页产物未指定文件名时的基名。
	//
	// 不用 "output"：output-0001.png 读起来像"一个叫 output 的东西的第 1 页"，
	// 而 page-0001.png 至少说清了这是分页的产物。
	defaultPageBaseName = "page"
	// maxBaseNameBytes 是基名的字节上限，给扩展名和文件系统上限留余量。
	// 多数文件系统单个文件名上限是 255 字节。
	maxBaseNameBytes = 200
)

// outputNaming 由调用方给的基名与格式扩展名决定产物文件名。
//
// base 为空表示调用方没指定，各处按需套用默认值。
type outputNaming struct {
	base      string
	extension string
}

// newOutputNaming 校验并归一化基名。
//
// 传入的是基名（不含扩展名），但容忍调用方把扩展名一起写进来：正好等于该
// 格式的主扩展名时剥掉，避免 invoice.pdf 变成 invoice.pdf.pdf。不做更激进的
// 剥离——report.final 是合法的基名，若按"任何后缀都是扩展名"处理会被砍成
// report。
func newOutputNaming(requested, extension string) (outputNaming, error) {
	base := strings.TrimSpace(requested)
	if base == "" {
		return outputNaming{extension: extension}, nil
	}
	if err := validateBaseName(base); err != nil {
		return outputNaming{}, err
	}
	// 用 >= 而不是 >：基名恰好等于扩展名（".pdf"）时剥完就是空的，
	// 而那几乎总是调用方的笔误。产出 .pdf.pdf 只会让人困惑。
	if extension != "" && len(base) >= len(extension) &&
		strings.EqualFold(base[len(base)-len(extension):], extension) {
		stripped := strings.TrimRight(base[:len(base)-len(extension)], ".")
		if stripped == "" {
			return outputNaming{}, fmt.Errorf("%w: 文件名 %q 去掉扩展名后为空",
				ErrBadRequest, requested)
		}
		base = stripped
	}
	return outputNaming{base: base, extension: extension}, nil
}

// validateBaseName 拒绝任何可能变成路径的基名。
//
// 判据是"必须是单个路径组件"：不含分隔符、不含 NUL、不是 . 或 ..。用这套
// 判据而不是黑名单，是因为黑名单总会漏——符号链接、Unicode 分隔符、
// Windows 备用数据流都在名单外。满足这套判据时 filepath.Join(dir, base)
// 落在 dir 之内是结构性成立的，不依赖逐个排除。
func validateBaseName(base string) error {
	if strings.ContainsAny(base, `/\`) {
		return fmt.Errorf("%w: 文件名不能包含路径分隔符: %q", ErrBadRequest, base)
	}
	if strings.ContainsRune(base, 0) {
		return fmt.Errorf("%w: 文件名不能包含空字符", ErrBadRequest)
	}
	if base == "." || base == ".." {
		return fmt.Errorf("%w: 文件名不能是 %q", ErrBadRequest, base)
	}
	// 控制字符在 Windows 上是保留字符，在别的平台会让日志与终端输出错乱。
	for _, r := range base {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: 文件名不能包含控制字符", ErrBadRequest)
		}
	}
	if len(base) > maxBaseNameBytes {
		return fmt.Errorf("%w: 文件名基名 %d 字节，超过 %d 上限",
			ErrBadRequest, len(base), maxBaseNameBytes)
	}
	if !utf8.ValidString(base) {
		return fmt.Errorf("%w: 文件名不是合法 UTF-8", ErrBadRequest)
	}
	return nil
}

// ValidateOutputFileName 供提交路径提前校验，让调用方拿到 400 而不是
// 一个注定失败的任务。
func ValidateOutputFileName(requested string) error {
	if strings.TrimSpace(requested) == "" {
		return nil
	}
	return validateBaseName(strings.TrimSpace(requested))
}

// single 单文件产物的名字。
func (n outputNaming) single() string {
	return n.orDefault(defaultBaseName) + n.extension
}

// page 逐页产物的名字。
//
// index 是从 1 开始的页号，与转换器 Writer 回调给的值一致（pages.go 里是
// len(pages)+1）。这里不再自行加一：调用方传进来是多少，编号就是多少。
func (n outputNaming) page(index int) string {
	return fmt.Sprintf("%s-%04d%s", n.orDefault(defaultPageBaseName), index, n.extension)
}

// orDefault 在调用方没指定基名时套用默认值。
func (n outputNaming) orDefault(fallback string) string {
	if n.base == "" {
		return fallback
	}
	return n.base
}
