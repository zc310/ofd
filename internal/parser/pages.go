package parser

import (
	"errors"
	"sync"

	"github.com/zc310/ofd/internal/models"
)

type Page struct {
	models.PageContent
	ID models.StID

	load     func(*Page) error
	loadOnce sync.Once
	loadErr  error
}

// EnsureLoaded 在首次访问页面时读取页面内容，并缓存读取结果。
func (p *Page) EnsureLoaded() error {
	if p == nil {
		return errors.New("页面为空")
	}
	p.loadOnce.Do(func() {
		if p.load != nil {
			p.loadErr = p.load(p)
		}
	})
	return p.loadErr
}
