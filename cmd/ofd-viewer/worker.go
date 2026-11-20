package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/zc310/ofd/pkg/crypt"
)

type request struct {
	filename    string
	input       []byte
	choose      bool
	close       bool
	page        int
	pages       []int // 非 nil 时为批量读取，不改变当前页。
	metadata    bool  // 读取全部页面尺寸元数据，不改变当前页。
	credentials *unlockCredentials
}

type pageResult struct {
	page  int
	scene pageScene
	err   error
}

type result struct {
	scene          pageScene
	pages          []pageResult
	metadata       []continuousPageMetadata
	session        *continuousSession
	filename       string
	page           int
	total          int
	err            error
	lockedFilename string
	transient      bool
	closed         bool
}

const (
	errBatchSize   viewerError = "页面批次超出数量限制"
	errBatchWeight viewerError = "页面批次超出内存预算或无法估算"
	errBatchMixed  viewerError = "页面批次不能与打开文档请求同时使用"
)

// runWorker 串行访问文档；页面引用通过会话租约保护，换文档时异步退役旧会话。
func runWorker(ctx context.Context, requests <-chan request, results chan<- result, wake func(), choose func() (string, error)) {
	runWorkerWithPicker(ctx, requests, results, wake, func() (selectedFile, error) {
		name, err := choose()
		return selectedFile{name: name}, err
	})
}

type selectedFile struct {
	name string
	data []byte
}

func runWorkerWithPicker(ctx context.Context, requests <-chan request, results chan<- result, wake func(), choose func() (selectedFile, error)) {
	runWorkerWithPickerReady(ctx, requests, results, wake, choose, nil)
}

func runWorkerWithPickerReady(ctx context.Context, requests <-chan request, results chan<- result, wake func(), choose func() (selectedFile, error), fontReady <-chan struct{}) {
	var model *documentModel
	var session *continuousSession
	var locked selectedFile
	defer func() {
		select {
		case pending, ok := <-requests:
			if ok {
				pending.credentials.clear()
			}
		default:
		}
		if session != nil {
			session.retire()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case req, ok := <-requests:
			if !ok {
				return
			}
			var response result
			if req.close {
				if session != nil {
					session.retire()
					session = nil
				} else if model != nil {
					model.close()
				}
				model = nil
				locked = selectedFile{}
				response.closed = true
				select {
				case <-ctx.Done():
					return
				case results <- response:
					wake()
				}
				continue
			}
			if req.choose {
				var selected selectedFile
				selected, response.err = choose()
				req.filename, req.input = selected.name, selected.data
				req.choose = false
				if response.err != nil || req.filename == "" {
					if model != nil {
						response.scene, response.filename, response.page, response.total, response.session = model.scene, model.filename, model.current, len(model.pages), session
						response.transient = model.input != nil
					}
					select {
					case <-ctx.Done():
						return
					case results <- response:
						wake()
					}
					continue
				}
				locked = selectedFile{}
			}
			if req.credentials != nil && req.filename == locked.name {
				req.input = locked.data
			}
			if req.filename != "" && fontReady != nil {
				select {
				case <-ctx.Done():
					req.credentials.clear()
					return
				case <-fontReady:
				}
			}
			switch {
			case req.metadata:
				if req.choose || req.filename != "" || req.pages != nil {
					response.err = errBatchMixed
				} else {
					response.metadata, response.err = readContinuousMetadata(ctx, model)
				}
			case req.pages != nil:
				if req.choose || req.filename != "" {
					response.err = errBatchMixed
				} else {
					response.pages, response.err = loadPageBatch(ctx, model, req.pages)
				}
			default:
				if req.credentials == nil {
					response.err = handleRequestWithRetire(&model, req, func() (string, error) { return "", nil }, func(old *documentModel) { //nolint:contextcheck // 同步解析接口不接收 context。
						if session != nil {
							session.retire()
							session = nil
						} else {
							old.close()
						}
					})
				}
				if req.filename != "" && (errors.Is(response.err, crypt.ErrLocked) || req.credentials != nil) {
					var input any = req.filename
					if req.input != nil {
						input = req.input
					}
					next, err := openUnlockedDocumentInput(ctx, req.filename, input, req.credentials)
					response.err = err
					if err == nil {
						if session != nil {
							session.retire()
							session = nil
						} else if model != nil {
							model.close()
						}
						model = next
						locked = selectedFile{}
					} else if needsUnlock(err) || (req.credentials != nil && ctx.Err() == nil) {
						response.lockedFilename = req.filename
						locked = selectedFile{name: req.filename, data: req.input}
					}
				}
			}
			if model != nil && session == nil {
				session = &continuousSession{close: model.close, view: model.view, input: model.input}
			}
			if model != nil {
				response.scene, response.filename = model.scene, model.filename
				response.page, response.total = model.current, len(model.pages)
				response.session = session
				response.transient = model.input != nil
			}
			select {
			case <-ctx.Done():
				return
			case results <- response:
				wake()
			}
		}
	}
}

func handleRequest(model **documentModel, req request, choose func() (string, error)) error {
	return handleRequestWithRetire(model, req, choose, func(old *documentModel) { old.close() })
}

func handleRequestWithRetire(model **documentModel, req request, choose func() (string, error), retire func(*documentModel)) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: %v", errPageOperation, recovered)
		}
	}()
	filename := req.filename
	if req.choose {
		filename, err = choose()
		if err != nil || filename == "" {
			return err
		}
	}
	if filename != "" {
		var input any = filename
		if req.input != nil {
			input = req.input
		}
		next, err := openDocumentInput(filename, input)
		if err != nil {
			return err
		}
		next.input = req.input
		if *model != nil {
			retire(*model)
		}
		*model = next
		return nil
	}
	if *model == nil {
		return errNoDocument
	}
	return (*model).selectPage(req.page)
}
