package paint

import (
	"image/color"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

type testResources map[models.StID]*models.ColorSpace

func (r testResources) GetColorSpace(id models.StID) *models.ColorSpace { return r[id] }
func (r testResources) ReadColorProfile(string) ([]byte, error)         { return nil, nil }

type colorCache struct {
	Resolver

	resources testResources
}

func colorCacheDocument(t *testing.T) *colorCache {
	t.Helper()
	doc := &colorCache{resources: testResources{
		30: {Type: "RGB", Palette: &models.Palette{CV: []models.StArray{{"255", "0", "0"}}}},
		31: {Type: "RGB", Palette: &models.Palette{CV: []models.StArray{{"0", "255", "0"}}}},
	}}
	t.Cleanup(doc.Close)
	return doc
}

func (p *colorCache) updateCtColor(source *models.CTColor) *Color {
	return p.Resolve(source, p.resources)
}

func TestColorCacheIsBoundedAndReturnsIndependentPaint(t *testing.T) {
	var doc Resolver
	source := &models.CTColor{Value: &models.Color{RGBA: color.RGBA{A: 255}}}
	first := doc.Resolve(source, nil)
	first.Value = color.RGBA{R: 255, A: 255}
	if second := doc.Resolve(source, nil); second.Value.R != 0 {
		t.Fatal("caller modified cached paint")
	}
	for range ctColorCacheCapacity * 2 {
		doc.Resolve(&models.CTColor{Value: &models.Color{RGBA: color.RGBA{A: 255}}}, nil)
	}
	if n := doc.ctColorCache.Len(); n > ctColorCacheCapacity {
		t.Fatalf("cache=%d exceeds capacity", n)
	}
	doc.Close()
	if doc.ctColorCache != nil {
		t.Fatal("Close retained color cache")
	}
}

func awaitColorOperation(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("颜色操作被无关构建阻塞")
	}
}

// 占住真实颜色空间资源的初始化入口，模拟慢 Profile 读取与 ICC 构建。
func pauseColorProfile(t *testing.T, doc *colorCache, id models.StID) func() {
	t.Helper()
	space := doc.resources.GetColorSpace(id)
	if space == nil {
		t.Fatalf("缺少颜色空间%d", id)
	}
	resources := &colorSpaceResources{}
	doc.colorSpacesMu.Lock()
	if doc.colorSpaces == nil {
		doc.colorSpaces = make(map[*models.ColorSpace]*colorSpaceResources)
	}
	doc.colorSpaces[space] = resources
	doc.colorSpacesMu.Unlock()
	entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	go func() {
		defer close(done)
		resources.once.Do(func() { close(entered); <-resume })
	}()
	t.Cleanup(func() { release(); <-done })
	awaitColorOperation(t, entered)
	return release
}

func waitBlockedColors(t *testing.T, count int) {
	t.Helper()
	buffer := make([]byte, 1<<20)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n := runtime.Stack(buffer, true)
		blocked := 0
		for _, stack := range strings.Split(string(buffer[:n]), "\n\n") {
			if strings.Contains(stack, "(*Resolver).Resolve") && strings.Contains(stack, "[sync.Mutex.Lock]") {
				blocked++
			}
		}
		if blocked >= count {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("没有观察到%d个颜色构建进入等待", count)
}

func cacheMeshColor(space models.StRefID) *models.CTColor {
	red := models.CTColor{ColorSpace: space, Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}
	green := models.CTColor{Value: &models.Color{RGBA: color.RGBA{G: 255, A: 255}}}
	blue := models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}
	return &models.CTColor{GouraudShd: &models.CTGouraudShd{Point: []models.GouraudPoint{
		{X: 0, Y: 0, Color: red},
		{X: 10, Y: 0, Color: green},
		{X: 0, Y: 10, Color: blue},
	}}}
}

func TestSlowColorProfilesDoNotBlockOtherColors(t *testing.T) {
	doc := colorCacheDocument(t)
	releaseFirst := pauseColorProfile(t, doc, 30)
	releaseSecond := pauseColorProfile(t, doc, 31)
	var wg sync.WaitGroup
	defer func() { releaseFirst(); releaseSecond(); wg.Wait() }()
	hot := &models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}
	if got := doc.updateCtColor(hot); got == nil || got.Value.R != 255 {
		t.Fatal("普通颜色初始化失败")
	}
	for _, source := range []*models.CTColor{cacheMeshColor(30), cacheMeshColor(31)} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := doc.updateCtColor(source); got == nil || got.Gradient == nil {
				t.Error("网格渐变构建失败")
			}
		}()
	}
	// 两个独立颜色空间都应到达各自的初始化等待，不会被颜色缓存锁串行化。
	waitBlockedColors(t, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if got := doc.updateCtColor(hot); got == nil || got.Value.R != 255 {
			t.Error("慢 Profile 阻塞了缓存命中")
		}
		cold := &models.CTColor{Value: &models.Color{RGBA: color.RGBA{G: 255, A: 255}}}
		if got := doc.updateCtColor(cold); got == nil || got.Value.G != 255 {
			t.Error("慢 Profile 阻塞了其他颜色的首次构建")
		}
		if got := doc.updateCtColor(cacheMeshColor(0)); got == nil || got.Gradient == nil {
			t.Error("慢 Profile 阻塞了不依赖它的网格构建")
		}
	}()
	awaitColorOperation(t, done)
}

func TestConcurrentColorBuildSharesGradientAndReturnsIndependentPaint(t *testing.T) {
	doc := colorCacheDocument(t)
	release := pauseColorProfile(t, doc, 30)
	const count = 8
	results := make([]*Color, count)
	source := cacheMeshColor(30)
	var wg sync.WaitGroup
	defer func() { release(); wg.Wait() }()
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); results[i] = doc.updateCtColor(source) }()
	}
	waitBlockedColors(t, count)
	release()
	wg.Wait()
	if results[0] == nil || results[0].Gradient == nil {
		t.Fatal("网格渐变为空")
	}
	want := results[0].Gradient
	for _, result := range results[1:] {
		if result == nil || result == results[0] || result.Gradient != want {
			t.Fatal("并发构建没有共享同一个渐变并返回独立画笔")
		}
	}
	results[0].Gradient = nil
	if got := doc.updateCtColor(source); got.Gradient != want {
		t.Fatal("修改返回画笔污染了缓存")
	}
	if got := want.At(2, 2); got.R < 150 || got.R > 154 || got.G < 50 || got.G > 52 || got.B < 50 || got.B > 52 {
		t.Fatalf("共享渐变取色不正确：%v", got)
	}
	if len(doc.ctColorBuilds) != 0 || doc.ctColorCache.Len() != 1 {
		t.Fatal("构建完成后仍保留在途条目或重复结果")
	}
}

func TestSolidColorDefersMeshConstructionUntilGradientPlacement(t *testing.T) {
	doc := colorCacheDocument(t)
	release := pauseColorProfile(t, doc, 30)
	defer release()
	source := cacheMeshColor(30)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if got := doc.ResolveSolid(source, doc.resources); got != nil {
			t.Error("只含着色渐变的颜色不应提前构建画笔")
		}
	}()
	// 样式准备不应提前构建一份没有 Boundary 变换的网格并等待 ICC。
	awaitColorOperation(t, done)
	release()
	gradient := doc.Gradient(source, identityGradientTransform, &geom.Rect{X1: 10, Y1: 10}, doc.resources)
	if gradient == nil {
		t.Fatal("实际放置路径时未构建渐变")
	}
}

func TestCloseDuringColorBuildDoesNotRefillCaches(t *testing.T) {
	doc := colorCacheDocument(t)
	release := pauseColorProfile(t, doc, 30)
	source := cacheMeshColor(30)
	// 恢复后还会解析另一个颜色空间，覆盖关闭后首次访问颜色空间缓存的情况。
	source.GouraudShd.Point[1].Color.ColorSpace = 31
	var wg sync.WaitGroup
	defer func() { release(); wg.Wait() }()
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := doc.updateCtColor(source); got == nil || got.Gradient == nil {
				t.Error("关闭前开始的颜色构建未完成")
			}
		}()
	}
	waitBlockedColors(t, 4)
	closed := make(chan struct{})
	go func() { doc.Close(); close(closed) }()
	awaitColorOperation(t, closed)
	release()
	wg.Wait()
	if doc.updateCtColor(source) != nil {
		t.Fatal("关闭后仍开始新的颜色缓存访问")
	}
	doc.colorSpaceResources(doc.resources.GetColorSpace(31), doc.resources)
	if doc.ctColorCache != nil || len(doc.ctColorBuilds) != 0 || len(doc.colorSpaces) != 0 {
		t.Fatal("关闭后颜色构建重新填充了颜色或 Profile 资源缓存")
	}
}
