package parser

import (
	"errors"
	"sync"

	"github.com/zc310/ofd/internal/models"
)

const defaultPageCacheSize = 8

type Page struct {
	models.PageContent
	ID models.StID

	document *Document
	load     func(*Page) error
	mu       sync.Mutex
	loaded   bool
	loadErr  error
}

// EnsureLoaded 在首次访问页面时读取页面内容，并缓存读取结果。
func (p *Page) EnsureLoaded() error {
	if p == nil {
		return errors.New("页面为空")
	}
	if p.document != nil {
		return p.document.ensurePageLoaded(p)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loaded {
		return p.loadErr
	}
	if p.load != nil {
		p.loadErr = p.load(p)
	}
	p.loaded = true
	return p.loadErr
}
