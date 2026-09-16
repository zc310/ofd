package render

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/font"
	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/utils"
)

var (
	onceFonts       sync.Once
	fontCacheMu     sync.Mutex
	systemFontCache = make(map[systemFontKey]*systemFontCacheEntry)
	fontRenderLocks = make(map[*canvas.FontFamily]*sync.Mutex)
	// 全局回退字体注册表：同一字体族全局只保存一份解析结果和一份字体数据引用，
	// 首个 render.Document 注册后即锁定；后续文档缺字体时默认全部使用已锁定字体。
	fallbackRegistry = make(map[string]*fallbackRegistration)
)

// fallbackRegistration 保存某个字体族全局唯一定位后的解析家族与来源引用。
type fallbackRegistration struct {
	family   *canvas.FontFamily
	sources  []fallbackFontSource
	ready    bool
	renderMu *sync.Mutex
}

// 空字符串只用于 fallbackRegistry 中的进程级默认字体；公开回退字体族名禁止为空。
const defaultFallbackKey = ""

type systemFontKey struct {
	name  string
	style canvas.FontStyle
}

type systemFontCacheEntry struct {
	family   *canvas.FontFamily
	renderMu *sync.Mutex
}

type Fonts struct {
	*parser.Document
	Fonts          map[models.StRefID]*canvas.FontFamily
	fallbacks      map[string]*canvas.FontFamily
	fallbackFaces  []fallbackFace
	fallbackByFont map[models.StRefID]string
	mu             sync.Mutex
	loadLocksMu    sync.Mutex
	loadLocks      map[models.StRefID]*sync.Mutex
	generation     uint64
	renderLocksMu  sync.Mutex
	renderLocks    map[*canvas.FontFamily]*sync.Mutex
}

type fallbackFace struct {
	family *canvas.FontFamily
	style  canvas.FontStyle
	name   string
}

type fallbackFontSource struct {
	data   []byte
	style  canvas.FontStyle
	digest [sha256.Size]byte
}

func NewFonts(doc *parser.Document) *Fonts {
	onceFonts.Do(func() {
		defaultRegistration := &fallbackRegistration{
			family:   canvas.NewFontFamily("default"),
			renderMu: &sync.Mutex{},
		}
		fallbackRegistry[defaultFallbackKey] = defaultRegistration
		if runtime.GOOS == "android" {
			defaultRegistration.ready = loadAndroidDefaultFont(defaultRegistration.family)
			return
		}
		if runtime.GOOS == "js" {
			// 浏览器没有可供 canvas 查询的本地字体目录。Web 文档应优先
			// 使用 OFD 内嵌字体，避免调用系统字体索引导致 WASM panic。
			return
		}
		var fontPath string
		var err error
		if fontPath, err = utils.FindFirstFileInDirs(font.DefaultFontDirs(), "simhei.ttf", "simfang.ttf", "simsun.ttc", "simkai.ttf"); err == nil {
			slog.Debug("load default font file", "path", fontPath)
			if err = defaultRegistration.family.LoadFontFile(fontPath, canvas.FontRegular); err == nil {
				defaultRegistration.ready = true
				return
			}
		}
		for _, name := range []string{"仿宋", "FangSong", "NSimSum", "楷体", "KaiTi", "黑体", "SimHei", "Noto Sans CJK SC", "WenQuanYi Micro Hei", "Cantarell", "Noto Sans", "Noto Serif", "DejaVu Sans", "DejaVu Serif", "Times"} {
			slog.Debug("load default system font", "family", name, "style", canvas.FontRegular)
			if err := defaultRegistration.family.LoadSystemFont(name, canvas.FontRegular); err == nil {
				defaultRegistration.ready = true
				break
			}
		}
	})
	return &Fonts{
		Document:       doc,
		Fonts:          make(map[models.StRefID]*canvas.FontFamily),
		fallbacks:      make(map[string]*canvas.FontFamily),
		fallbackByFont: make(map[models.StRefID]string),
		loadLocks:      make(map[models.StRefID]*sync.Mutex),
		renderLocks:    make(map[*canvas.FontFamily]*sync.Mutex),
	}
}

func (p *Fonts) loadLock(id models.StRefID) *sync.Mutex {
	p.loadLocksMu.Lock()
	defer p.loadLocksMu.Unlock()
	if p.loadLocks == nil {
		p.loadLocks = make(map[models.StRefID]*sync.Mutex)
	}
	if lock := p.loadLocks[id]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	p.loadLocks[id] = lock
	return lock
}

func (p *Fonts) renderLock(family *canvas.FontFamily) *sync.Mutex {
	fontCacheMu.Lock()
	for _, registration := range fallbackRegistry {
		if registration.family == family && registration.renderMu != nil {
			lock := registration.renderMu
			fontCacheMu.Unlock()
			return lock
		}
	}
	if lock := fontRenderLocks[family]; lock != nil {
		fontCacheMu.Unlock()
		return lock
	}
	fontCacheMu.Unlock()
	p.renderLocksMu.Lock()
	defer p.renderLocksMu.Unlock()
	if p.renderLocks == nil {
		p.renderLocks = make(map[*canvas.FontFamily]*sync.Mutex)
	}
	if lock := p.renderLocks[family]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	p.renderLocks[family] = lock
	return lock
}

func loadCachedSystemFont(name string, style canvas.FontStyle) (*canvas.FontFamily, bool) {
	key := systemFontKey{name: name, style: style}
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if entry := systemFontCache[key]; entry != nil {
		return entry.family, true
	}

	family := canvas.NewFontFamily(name)
	slog.Debug("load system font", "family", name, "style", style)
	if err := family.LoadSystemFont(name, style); err != nil {
		return nil, false
	}
	entry := &systemFontCacheEntry{family: family, renderMu: &sync.Mutex{}}
	systemFontCache[key] = entry
	fontRenderLocks[family] = entry.renderMu
	return family, true
}

func loadAndroidDefaultFont(family *canvas.FontFamily) bool {
	preferredNames := []string{
		"NotoSansCJK-Regular.ttc",
		"NotoSansCJK-Regular.ttf",
		"NotoSansCJK-Regular.otf",
		"NotoSansCJK-VF.ttf",
		"NotoSansSC-Regular.otf",
		"NotoSansSC-Regular.ttf",
		"DroidSansFallback.ttf",
		"NotoSans-Regular.ttf",
		"Roboto-Regular.ttf",
	}
	for _, dir := range font.DefaultFontDirs() {
		for _, name := range preferredNames {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err != nil {
				continue
			}
			if loadFontFileSafely(family, path) {
				return true
			}
		}
	}

	// 不同 Android 版本和厂商使用的字体文件名可能不同。继续尝试其他
	// 常规字体文件，但不使用 canvas 的系统字体索引，因为新版 Android
	// 可能无法提供该索引。
	for _, dir := range font.DefaultFontDirs() {
		found := false
		_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			if !isAndroidFontFile(entry.Name()) {
				return nil
			}
			if loadFontFileSafely(family, path) {
				found = true
				return fs.SkipAll
			}
			return nil
		})
		if found {
			return true
		}
	}
	return false
}

func loadFontFileSafely(family *canvas.FontFamily, path string) (loaded bool) {
	slog.Debug("load android font file", "path", path)
	defer func() {
		if recover() != nil {
			loaded = false
		}
	}()
	return family.LoadFontFile(path, canvas.FontRegular) == nil && fontFamilySupportsCJK(family)
}

func isAndroidFontFile(name string) bool {
	extension := strings.ToLower(filepath.Ext(name))
	return extension == ".ttf" || extension == ".ttc" || extension == ".otf"
}

func fontFamilySupportsCJK(family *canvas.FontFamily) bool {
	if !fontFamilyUsable(family) {
		return false
	}
	face := family.Face(1, canvas.Black)
	return face != nil && face.Font != nil && face.Font.GlyphIndex('中') != 0
}

func (p *Fonts) LoadFont(id models.StRefID) (*canvas.FontFamily, error) {
	if p == nil {
		return nil, fmt.Errorf("字体上下文为空")
	}
	loadLock := p.loadLock(id)
	loadLock.Lock()
	defer loadLock.Unlock()

	for {
		p.mu.Lock()
		if family := p.Fonts[id]; family != nil {
			p.mu.Unlock()
			return family, nil
		}
		var ft *models.Font
		if p.Document != nil {
			ft = p.Document.GetFont(models.StID(id))
		}
		fallbacks := append([]fallbackFace(nil), p.fallbackFaces...)
		generation := p.generation
		p.mu.Unlock()

		family, fallbackName, err := p.loadFontUncached(id, ft, fallbacks)

		p.mu.Lock()
		if generation != p.generation {
			p.mu.Unlock()
			continue
		}
		if err != nil {
			p.mu.Unlock()
			return nil, err
		}
		if current := p.Fonts[id]; current != nil {
			p.mu.Unlock()
			return current, nil
		}
		p.Fonts[id] = family
		if fallbackName != "" {
			p.fallbackByFont[id] = fallbackName
		} else {
			delete(p.fallbackByFont, id)
		}
		p.mu.Unlock()
		return family, nil
	}
}

func (p *Fonts) loadFontUncached(id models.StRefID, ft *models.Font, fallbacks []fallbackFace) (*canvas.FontFamily, string, error) {
	defaultFamily, defaultReady := defaultFallbackFont()
	if ft == nil {
		if fallback, ok := selectFallback(fallbacks, canvas.FontRegular); ok {
			return fallback.family, fallback.family.Name(), nil
		}
		if !defaultReady {
			return nil, "", fmt.Errorf("字体 %d 不存在且没有可用的默认字体", id)
		}
		return defaultFamily, "", nil
	}

	fontName := ft.FontName
	fontStyle := canvas.FontRegular
	if ft.Italic {
		fontStyle |= canvas.FontItalic
	}
	if ft.Bold {
		fontStyle |= canvas.FontBold
	}
	if ft.FontFile != "" {
		family := canvas.NewFontFamily(fontName)
		if data, err := p.FileCache.Read(string(ft.FontFile)); err == nil {
			if err := loadEmbeddedFont(family, data, fontStyle); err == nil {
				return family, "", nil
			}
		}
	}

	var matched *canvas.FontFamily
	p.Document.ForEachFont(func(candidateID models.StID, candidate *models.Font) bool {
		// 没有 FontFile 的资源只是逻辑字体，不能借用同名的 OFD 子集字体。
		if ft.FontFile == "" || candidateID == models.StID(id) || candidate.FontFile == "" || !sameFontName(*ft, *candidate) {
			return true
		}
		data, err := p.FileCache.Read(string(candidate.FontFile))
		if err != nil {
			return true
		}
		if fixed, fixErr := fontfix.Repair(data); fixErr == nil {
			data = fixed
		}
		candidateFamily := canvas.NewFontFamily(fontName)
		slog.Debug("load embedded font candidate", "family", fontName, "style", fontStyle, "bytes", len(data))
		if err := candidateFamily.LoadFont(data, 0, fontStyle); err == nil && fontFamilyUsable(candidateFamily) {
			matched = candidateFamily
			return false
		}
		return true
	})
	if matched != nil {
		return matched, "", nil
	}

	if runtime.GOOS == "js" {
		if fallback, ok := selectFallback(fallbacks, fontStyle, ft.FontName, ft.FamilyName); ok {
			return fallback.family, fallback.family.Name(), nil
		}
		if defaultReady {
			return defaultFamily, "", nil
		}
		return nil, "", fmt.Errorf("浏览器没有可用的字体 %q，请使用内嵌字体", fontName)
	}
	if fallback, ok := selectFallback(fallbacks, fontStyle, ft.FontName, ft.FamilyName); ok {
		return fallback.family, fallback.family.Name(), nil
	}
	if runtime.GOOS == "android" {
		if defaultReady {
			return defaultFamily, "", nil
		}
	} else if family, ok := loadCachedSystemFont(fontName, fontStyle); ok {
		return family, "", nil
	}
	family := canvas.NewFontFamily(fontName)
	if fontName == "宋体" || strings.ToLower(fontName) == "simsun" {
		if fontPath, err := utils.FindFirstFileInDirs(font.DefaultFontDirs(), "simsun.ttc"); err == nil {
			slog.Debug("load fallback system font file", "family", fontName, "path", fontPath, "style", fontStyle)
			if err := family.LoadFontFile(fontPath, fontStyle); err == nil {
				return family, "", nil
			}
		}
	}
	if fontName == "黑体" || strings.ToLower(fontName) == "simhei" {
		if fontPath, err := utils.FindFirstFileInDirs(font.DefaultFontDirs(), "simhei.ttf"); err == nil {
			slog.Debug("load fallback system font file", "family", fontName, "path", fontPath, "style", fontStyle)
			if err := family.LoadFontFile(fontPath, fontStyle); err == nil {
				return family, "", nil
			}
		}
	}
	if defaultFamily != nil {
		if !defaultReady {
			return nil, "", fmt.Errorf("字体 %d 无法加载且没有可用的默认字体", id)
		}
		slog.Warn("字体不可用，使用默认字体", "id", uint64(id), "name", ft.FontName)
		return defaultFamily, "", nil
	}
	return nil, "", fmt.Errorf("字体 %d 无法加载", id)
}

func defaultFallbackFont() (*canvas.FontFamily, bool) {
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	registration := fallbackRegistry[defaultFallbackKey]
	if registration == nil {
		return nil, false
	}
	return registration.family, registration.ready
}

func selectFallback(fallbacks []fallbackFace, style canvas.FontStyle, fontNames ...string) (fallbackFace, bool) {
	bestScore := int(^uint(0) >> 1)
	var best fallbackFace
	found := false
	for _, face := range fallbacks {
		score := absInt(face.style.CSS() - style.CSS())
		if face.style.Italic() != style.Italic() {
			score += 1000
		}
		for _, fontName := range fontNames {
			if sameFallbackName(face.name, fontName) {
				score -= 1000000
				break
			}
		}
		if !found || score < bestScore {
			bestScore = score
			best = face
			found = true
		}
	}
	return best, found
}

func sameFallbackName(left, right string) bool {
	left = normalizeFallbackName(left)
	right = normalizeFallbackName(right)
	if left == "" || right == "" {
		return false
	}
	if left == right {
		return true
	}
	return fallbackNameGroup(left) != "" && fallbackNameGroup(left) == fallbackNameGroup(right)
}

func normalizeFallbackName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.NewReplacer(" ", "", "_", "", "-", "").Replace(name)
	return name
}

func fallbackNameGroup(name string) string {
	switch name {
	case "宋体", "宋体gb2312", "simsun", "nsimsun", "songti", "simsungb2312", "songtigb2312":
		return "simsun"
	case "华文宋体", "stsong":
		return "stsong"
	case "黑体", "黑体gb2312", "simhei", "heiti", "hei", "microsoftheiti", "heitisc":
		return "simhei"
	case "华文黑体", "stheiti":
		return "stheiti"
	case "楷体", "楷体gb2312", "simkai", "kaiti", "kaishu", "kaitigb2312":
		return "simkai"
	case "华文楷体", "stkaiti":
		return "stkaiti"
	case "仿宋", "仿宋gb2312", "simfang", "fangsong", "fangsonggb2312":
		return "simfang"
	case "华文仿宋", "stfangsong":
		return "stfangsong"
	case "微软雅黑", "microsoftyahei", "microsoftyaheiui", "msyh", "yahei":
		return "yahei"
	case "微软正黑", "microsoftjhenghei", "microsoftjhengheiui", "msjh":
		return "jhenghei"
	case "等线", "dengxian":
		return "dengxian"
	case "思源黑体", "sourcehansanssc", "sourcehansanscn", "notosanssc", "notosanscjksc", "ofdnotosanssc":
		return "sanssc"
	case "思源宋体", "sourcehanserifsc", "sourcehanserifcn", "notoserifsc", "notoserifcjksc":
		return "serifsc"
	default:
		return ""
	}
}

// RegisterFallbackFont 把回退字体注册到进程级全局注册表。
// 同一字体族全局只保存一份解析结果和一份字体数据引用，重复注册幂等
// （首个 RenderDocument/Fonts 完成解析后即锁定）。注册后通过
// Fonts.UseFallbackFont 或 Document.UseFallbackFont 应用到具体文档；
// 没有其他回退字体时，缺失字体默认使用该全局字体族。
func RegisterFallbackFont(data []byte, family string, style canvas.FontStyle) error {
	if len(data) == 0 {
		return fmt.Errorf("回退字体数据为空")
	}
	if family == "" {
		return fmt.Errorf("回退字体族名为空")
	}
	_, err := registerFallbackFamily(family, style, data)
	return err
}

// FallbackFontData 返回已全局注册回退字体族的首个来源数据（用于字体签名等
// 只读检查）；未注册时 ok 为 false。
func FallbackFontData(family string) ([]byte, bool) {
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	reg := fallbackRegistry[family]
	if reg == nil || len(reg.sources) == 0 {
		return nil, false
	}
	return reg.sources[0].data, true
}

// UseFallbackFont 使当前字体上下文在缺失字体时使用已全局注册的回退字体族。
// 该字体族必须先通过 RegisterFallbackFont 注册；其全部已注册样式都会生效。
func (p *Fonts) UseFallbackFont(family string) error {
	if p == nil {
		return fmt.Errorf("字体上下文为空")
	}
	if family == "" {
		return fmt.Errorf("回退字体族名为空")
	}

	fontCacheMu.Lock()
	reg := fallbackRegistry[family]
	fontCacheMu.Unlock()
	if reg == nil {
		return fmt.Errorf("回退字体族 %q 未注册", family)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fallbacks[family] == reg.family && p.hasAllFallbackFaces(family, reg.sources) {
		// 该实例已应用全局字体族，无需再次登记。
		return nil
	}
	p.fallbacks[family] = reg.family
	for _, source := range reg.sources {
		p.applyFallbackFace(family, source.style, reg.family)
	}
	// 回退集合变化后需重建按字体 ID 的解析缓存。
	p.Fonts = make(map[models.StRefID]*canvas.FontFamily)
	p.fallbackByFont = make(map[models.StRefID]string)
	p.generation++
	return nil
}

func (p *Fonts) hasFallbackFace(family string, style canvas.FontStyle) bool {
	for _, face := range p.fallbackFaces {
		if face.name == family && face.style == style {
			return true
		}
	}
	return false
}

func (p *Fonts) hasAllFallbackFaces(family string, sources []fallbackFontSource) bool {
	for _, source := range sources {
		if !p.hasFallbackFace(family, source.style) {
			return false
		}
	}
	return true
}

func (p *Fonts) applyFallbackFace(family string, style canvas.FontStyle, globalFamily *canvas.FontFamily) {
	for index, face := range p.fallbackFaces {
		if face.name == family && face.style == style {
			p.fallbackFaces[index] = fallbackFace{family: globalFamily, style: style, name: family}
			return
		}
	}
	p.fallbackFaces = append(p.fallbackFaces, fallbackFace{family: globalFamily, style: style, name: family})
}

// registerFallbackFamily 把 字体族+样式+数据 注册到全局注册表。
// 同一 字体族+样式+数据 只处理一次（锁定）；相同字体族引入不同样式/字体时
// 构建包含全部来源的全局多样式家族，同样只构建一次。
func registerFallbackFamily(family string, style canvas.FontStyle, data []byte) (*canvas.FontFamily, error) {
	digest := sha256.Sum256(data)
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()

	reg := fallbackRegistry[family]
	if reg == nil {
		reg = &fallbackRegistration{renderMu: &sync.Mutex{}}
		fallbackRegistry[family] = reg
	}
	for _, source := range reg.sources {
		if source.style == style && source.digest == digest {
			return reg.family, nil
		}
	}

	reg.renderMu.Lock()
	defer reg.renderMu.Unlock()
	fontFamily := reg.family
	if len(reg.sources) == 0 || !sameFallbackFontData(reg.sources, digest) {
		fontFamily = canvas.NewFontFamily(family)
		for _, source := range reg.sources {
			if err := loadFallbackFace(fontFamily, source.data, source.style); err != nil {
				if len(reg.sources) == 0 {
					delete(fallbackRegistry, family)
				}
				return nil, err
			}
		}
	}
	if err := loadFallbackFace(fontFamily, data, style); err != nil {
		if len(reg.sources) == 0 {
			delete(fallbackRegistry, family)
		}
		return nil, err
	}
	reg.family = fontFamily
	// 字体数据由调用方（webreader.Reader）持有，注册表只保留引用，整个进程
	// 只保存一份字体字节。
	reg.sources = append(reg.sources, fallbackFontSource{data: data, style: style, digest: digest})
	return reg.family, nil
}

func loadFallbackFace(family *canvas.FontFamily, data []byte, style canvas.FontStyle) error {
	slog.Debug("load fallback font", "family", family.Name(), "style", style, "bytes", len(data))
	if err := family.LoadFont(data, 0, style); err != nil {
		return err
	}
	if !fontFamilyUsable(family) {
		return fmt.Errorf("回退字体结构不可用")
	}
	return nil
}

func sameFallbackFontData(sources []fallbackFontSource, digest [sha256.Size]byte) bool {
	for _, source := range sources {
		if source.digest != digest {
			return false
		}
	}
	return true
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// FallbackFontFamily 返回为文档字体选择的外部字体族。
// 必须先调用 LoadFont；TextLayouts 和 Text 会自动完成这一步。
func (p *Fonts) FallbackFontFamily(id models.StRefID) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fallbackByFont[id]
}

// HasLoadedEmbeddedFont 判断文档字体是否解析为可用且已加载的字体族。
// 仅声明 FontFile 并不够，因为文件可能缺失、格式错误或被字体解析器拒绝。
func (p *Fonts) HasLoadedEmbeddedFont(id models.StRefID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Document == nil {
		return false
	}
	ft := p.Document.GetFont(models.StID(id))
	if ft == nil || ft.FontFile == "" {
		return false
	}
	_, ok := p.Fonts[id]
	return ok && p.fallbackByFont[id] == ""
}

// loadEmbeddedFont 优先使用 fontfix 修复后的内嵌字体，修复失败时再尝试
// 原始字体数据。只有两者都无法加载时，调用方才会继续使用外部字体回退。
func loadEmbeddedFont(family *canvas.FontFamily, data []byte, style canvas.FontStyle) error {
	if fixed, err := fontfix.Repair(data); err == nil {
		slog.Debug("load repaired embedded font", "family", family.Name(), "style", style, "bytes", len(fixed))
		if err = family.LoadFont(fixed, 0, style); err == nil && fontFamilyUsable(family) {
			return nil
		}
	}
	slog.Debug("load original embedded font", "family", family.Name(), "style", style, "bytes", len(data))
	if err := family.LoadFont(data, 0, style); err == nil && fontFamilyUsable(family) {
		return nil
	}
	return fmt.Errorf("嵌入字体结构不可用")
}

func sameFontName(left, right models.Font) bool {
	if left.FontName != "" && left.FontName == right.FontName {
		return true
	}
	return left.FamilyName != "" && left.FamilyName == right.FamilyName
}

// fontFamilyUsable 确保字体包含 canvas 绘制文字所需的基本字形表。
func fontFamilyUsable(family *canvas.FontFamily) bool {
	return fontFamilyUsableSafe(family)
}

func fontFamilyUsableSafe(family *canvas.FontFamily) (usable bool) {
	defer func() {
		if recover() != nil {
			usable = false
		}
	}()
	if family == nil {
		return false
	}
	face := family.Face(1, canvas.Black)
	return face != nil && face.Font != nil && face.Font.SFNT != nil &&
		face.Font.SFNT.Head != nil && face.Font.SFNT.Hhea != nil &&
		face.Font.SFNT.OS2 != nil && face.Font.SFNT.Cmap != nil &&
		face.Font.SFNT.Maxp != nil
}
