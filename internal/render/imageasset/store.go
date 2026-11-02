package imageasset

import (
	"context"
	"errors"
	"image"
	"log/slog"
	"os"
	"sync"

	"github.com/zc310/ofd/internal/cache"
)

const (
	imageCacheCapacity = 128
	imageCacheBytes    = 64 << 20
	svgCacheCapacity   = 64
	svgCacheBytes      = 16 << 20
	// 单条大图单独保留，既不冲掉普通 LRU，也不以编码字节掩盖解码后的内存。
	largeImageBytes = 256 << 20
)

// Store 持有单个文档视图的图片和 SVG 场景缓存，零值可用且支持并发调用。
// 首次使用后不可复制，同一实例始终使用同一份文档资源。
type Store struct {
	mu          sync.Mutex
	keyLocks    map[string]*keyLock
	images      *cache.LRU[string, image.Image]
	svgCanvases *cache.LRU[string, SVGScene]
	failures    *cache.LRU[string, error]
	largeKey    string
	largeImage  image.Image
	closed      bool
}

type keyLock struct {
	mu   sync.Mutex
	refs int
}

// Close 清空图片和场景缓存，并阻止在途加载重新发布结果；可重复调用。
// 正在使用的加载锁在最后一个调用结束后移除。
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.images = nil
	s.svgCanvases = nil
	s.failures = nil
	s.largeKey, s.largeImage = "", nil
}

func (s *Store) lockKey(key string) *keyLock {
	s.mu.Lock()
	if s.keyLocks == nil {
		s.keyLocks = make(map[string]*keyLock)
	}
	lock := s.keyLocks[key]
	if lock == nil {
		lock = &keyLock{}
		s.keyLocks[key] = lock
	}
	lock.refs++
	s.mu.Unlock()
	lock.mu.Lock()
	return lock
}

func (s *Store) unlockKey(key string, lock *keyLock) {
	lock.mu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	lock.refs--
	if lock.refs == 0 && s.keyLocks[key] == lock {
		delete(s.keyLocks, key)
	}
}

// Image 按键合并并发加载，资源图片和已预处理的内存印章共用有界 LRU。
// load 在缓存未命中时执行，执行期间只持该键的锁，不阻塞其他图片。
func (s *Store) Image(key string, load func() (image.Image, error)) (image.Image, error) {
	lock := s.lockKey(key)
	defer s.unlockKey(key, lock)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("渲染文档已关闭")
	}
	if s.largeImage != nil && s.largeKey == key {
		img := s.largeImage
		s.mu.Unlock()
		return img, nil
	}
	if s.failures != nil {
		if err, ok := s.failures.Get(key); ok {
			s.mu.Unlock()
			return nil, err
		}
	}
	if s.images == nil {
		s.images = cache.NewWeightedLRU[string, image.Image](imageCacheCapacity, imageCacheBytes, nil)
	}
	cached, ok := s.images.Get(key)
	s.mu.Unlock()
	if ok {
		return cached, nil
	}
	img, err := load()
	if err != nil {
		s.rememberFailure(key, err)
		return nil, err
	}
	weight := imageCacheWeight(img)
	s.mu.Lock()
	if !s.closed {
		if weight <= imageCacheBytes {
			s.images.AddWeighted(key, img, weight)
		} else if weight <= largeImageBytes {
			s.largeKey, s.largeImage = key, img
		}
	}
	s.mu.Unlock()
	return img, nil
}

// SVG 复用已解析场景，场景解析在缓存锁外执行。
// SVG 场景和同名图片使用不同的加载锁，允许图片回退复用场景而不自锁。
func (s *Store) SVG(key string, load func() ([]byte, error), parse SVGParser) (SVGScene, error) {
	lockKey := "\x00svg-scene:" + key
	lock := s.lockKey(lockKey)
	defer s.unlockKey(lockKey, lock)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("渲染文档已关闭")
	}
	if s.failures != nil {
		if err, ok := s.failures.Get(lockKey); ok {
			s.mu.Unlock()
			return nil, err
		}
	}
	if s.svgCanvases == nil {
		s.svgCanvases = cache.NewWeightedLRU[string, SVGScene](svgCacheCapacity, svgCacheBytes, nil)
	}
	cached, ok := s.svgCanvases.Get(key)
	s.mu.Unlock()
	if ok {
		return cached, nil
	}
	data, err := load()
	if err != nil {
		s.rememberFailure(lockKey, err)
		return nil, err
	}
	scene, err := ParseSVG(data, parse)
	if err != nil {
		if parse != nil {
			err = permanentError{err}
		}
		s.rememberFailure(lockKey, err)
		return nil, err
	}
	s.mu.Lock()
	if !s.closed {
		s.svgCanvases.AddWeighted(key, scene, svgCanvasWeight(data))
	}
	s.mu.Unlock()
	return scene, nil
}

// permanentError 只标记固定资源本身的错误，调用方预算和临时读取失败不能污染共享缓存。
type permanentError struct{ error }

func (e permanentError) Unwrap() error { return e.error }

func (s *Store) rememberFailure(key string, err error) {
	var permanent permanentError
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrPixelBudget) ||
		(!errors.As(err, &permanent) && !errors.Is(err, os.ErrNotExist)) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.failures == nil {
		s.failures = cache.NewLRU[string, error](imageCacheCapacity, nil)
	}
	s.failures.Add(key, err)
	slog.Debug("图像资源加载失败，已缓存失败结果", "resource", key, "error", err)
}
