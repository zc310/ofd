package main

import (
	"context"
	"fmt"
)

// loadPageBatch 仅在 worker 独占文档时调用。结果须在会话租约期间重放；
// 租约只保护关闭时序，并不允许与 worker 的文档访问并发重放。
// 响应中页面总估算重量与数量均有上限，不包括当前页、缓存及共享资源。
func loadPageBatch(ctx context.Context, model *documentModel, pages []int) ([]pageResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if model == nil {
		return nil, errNoDocument
	}
	if len(pages) > pageSceneCacheEntries {
		return nil, errBatchSize
	}
	results := make([]pageResult, 0, len(pages))
	var total int64
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		scene, err := safeSceneAt(model, page)
		if err == nil {
			weighted, ok := scene.(interface{ Weight() int64 })
			if !ok {
				err = errBatchWeight
			} else if weight := weighted.Weight(); weight <= 0 || weight > pageSceneCacheBytes-total {
				err = errBatchWeight
			} else {
				total += weight
			}
		}
		if err != nil {
			scene = nil
		}
		results = append(results, pageResult{page: page, scene: scene, err: err})
	}
	return results, nil
}

func safeSceneAt(model *documentModel, page int) (scene pageScene, err error) { //nolint:nonamedreturns,ireturn // 将单页异常隔离，沿用页面记录接口。
	defer func() {
		if recovered := recover(); recovered != nil {
			scene, err = nil, fmt.Errorf("%w: %v", errPageOperation, recovered)
		}
	}()
	return model.sceneAt(page)
}
