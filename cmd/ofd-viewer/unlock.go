package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/zc310/ofd/pkg/crypt"
)

const (
	unlockPasswordMethod = "口令"
	unlockFileMethod     = "口令文件"
	unlockKeyMethod      = "SM2 私钥"
)

// viewerUnlockPrompt 仅在事件线程操作；凭据文件在后台读取后交回该线程。
type viewerUnlockPrompt struct {
	dialog     *dialog.CustomDialog
	method     *widget.Select
	password   *widget.Entry
	user       *widget.Entry
	fileName   *widget.Label
	message    *widget.Label
	confirm    *widget.Button
	fileData   []byte
	fileSeq    uint64
	reading    bool
	filePicker *dialog.FileDialog
}

func (p *viewerUnlockPrompt) clear() {
	p.fileSeq++
	clear(p.fileData)
	p.fileData = nil
	p.password.SetText("")
	p.user.SetText("")
	if p.filePicker != nil {
		picker := p.filePicker
		p.filePicker = nil
		picker.Hide()
	}
}

func (p *viewerUnlockPrompt) credentials() (crypt.DecryptOptions, error) {
	options := crypt.DecryptOptions{UserName: p.user.Text}
	var err error
	switch p.method.Selected {
	case tr(unlockPasswordMethod):
		data := []byte(p.password.Text)
		defer clear(data)
		options.Password, err = viewerPassword(data)
	case tr(unlockFileMethod):
		if p.fileData == nil {
			return options, errors.New(tr("请先选择口令文件或私钥文件"))
		}
		options.Password, err = viewerPassword(p.fileData)
	case tr(unlockKeyMethod):
		if p.fileData == nil {
			return options, errors.New(tr("请先选择口令文件或私钥文件"))
		}
		options.PrivateKey, err = viewerPrivateKey(p.fileData)
	}
	return options, err
}

// cancelDocumentLoad 取消旧加载并清理尚未提交的凭据，不影响当前文档会话。
func (v *viewer) cancelDocumentLoad() {
	if v.loadCancel != nil {
		v.loadCancel()
		v.loadCancel = nil
	}
	if prompt := v.unlock; prompt != nil {
		v.unlock = nil
		prompt.clear()
		prompt.dialog.Hide()
	}
}

func (v *viewer) showUnlock(ctx context.Context, operation uint64, filePath, fileName string, input any, cause error) {
	p := &viewerUnlockPrompt{
		password: widget.NewPasswordEntry(), user: widget.NewEntry(),
		fileName: widget.NewLabel(tr("尚未选择文件")), message: widget.NewLabel(""),
	}
	p.message.Wrapping = fyne.TextWrapWord
	if errors.Is(cause, crypt.ErrDecryptFailed) {
		p.message.SetText(tr("凭据不匹配或密文损坏，请检查口令、私钥或接收者后重试。"))
	}
	p.user.SetPlaceHolder(tr("可选，留空自动匹配"))
	fileButton := widget.NewButton(tr("选择凭据文件"), func() { v.chooseUnlockFile(ctx, p) })
	fileField := container.NewBorder(nil, nil, nil, fileButton, p.fileName)
	p.method = widget.NewSelect([]string{tr(unlockPasswordMethod), tr(unlockFileMethod), tr(unlockKeyMethod)}, func(method string) {
		p.fileSeq++
		clear(p.fileData)
		p.fileData = nil
		p.reading = false
		p.fileName.SetText(tr("尚未选择文件"))
		p.password.SetText("")
		p.confirm.Enable()
		if method == tr(unlockPasswordMethod) {
			p.password.Show()
			fileField.Hide()
		} else {
			p.password.Hide()
			fileField.Show()
		}
	})
	p.confirm = widget.NewButton(tr("解锁并打开"), func() {
		if v.unlock != p || p.reading {
			return
		}
		credentials, err := p.credentials()
		if err != nil {
			p.message.SetText(err.Error())
			return
		}
		v.unlock = nil
		p.clear()
		p.dialog.Hide()
		go v.runLoadContext(ctx, operation, filePath, fileName, input, credentials)
	})
	p.confirm.Importance = widget.HighImportance
	p.password.OnSubmitted = func(string) { p.confirm.OnTapped() }
	p.method.SetSelected(tr(unlockPasswordMethod))
	hint := widget.NewLabel(tr("口令文件保留空格和换行；私钥须为未加密的 SM2 PKCS#8。凭据仅用于本次解锁。"))
	hint.Wrapping = fyne.TextWrapWord
	name := widget.NewLabel(fileName)
	name.Wrapping = fyne.TextWrapWord
	content := container.NewVBox(name,
		widget.NewForm(widget.NewFormItem(tr("解锁方式"), p.method), widget.NewFormItem(tr("接收者"), p.user)),
		p.password, fileField, hint, p.message, p.confirm)
	p.dialog = dialog.NewCustom(tr("解锁加密文档"), tr("取消"), content, v.window)
	p.dialog.SetOnClosed(func() {
		if v.unlock != p {
			return
		}
		v.unlock = nil
		p.clear()
		v.operation.Add(1)
		v.cancelDocumentLoad()
		v.finishLoad()
		v.updatePageWindow(v.operation.Load())
	})
	v.unlock = p
	p.dialog.Show()
}

func (v *viewer) chooseUnlockFile(ctx context.Context, p *viewerUnlockPrompt) {
	if v.unlock != p || p.reading || p.filePicker != nil {
		return
	}
	p.fileSeq++
	sequence := p.fileSeq
	limit := int64(viewerMaxPasswordBytes)
	if p.method.Selected == tr(unlockKeyMethod) {
		limit = viewerMaxPrivateKeyBytes
	}
	var fileDialog *dialog.FileDialog
	fileDialog = dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if p.filePicker == fileDialog {
			p.filePicker = nil
		}
		if reader == nil {
			if err != nil && v.unlock == p && p.fileSeq == sequence {
				p.message.SetText(err.Error())
			}
			return
		}
		if v.unlock != p || p.fileSeq != sequence || err != nil {
			go reader.Close()
			return
		}
		name := reader.URI().Name()
		p.reading = true
		p.confirm.Disable()
		go func() {
			data, err := readViewerCredential(ctx, reader, limit)
			fyne.Do(func() {
				if v.unlock != p || p.fileSeq != sequence || ctx.Err() != nil {
					clear(data)
					return
				}
				p.reading = false
				p.confirm.Enable()
				clear(p.fileData)
				p.fileData = nil
				if err != nil {
					p.fileName.SetText(tr("尚未选择文件"))
					p.message.SetText(err.Error())
					return
				}
				p.fileData = data
				p.fileName.SetText(name)
				p.message.SetText("")
			})
		}()
	}, v.window)
	fileDialog.SetTitleText(tr("选择凭据文件"))
	fileDialog.SetConfirmText(tr("打开"))
	fileDialog.SetDismissText(tr("取消"))
	p.filePicker = fileDialog
	fileDialog.Show()
}

// readViewerCredential 始终关闭输入，失败时清理已经读取的凭据字节。
func readViewerCredential(ctx context.Context, reader io.ReadCloser, limit int64) (data []byte, err error) {
	defer func() {
		if closeErr := reader.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			clear(data)
			data = nil
		}
	}()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	data, err = io.ReadAll(io.LimitReader(reader, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = fmt.Errorf(tr("凭据文件超过大小限制（%d 字节）"), limit)
	}
	if err == nil {
		err = ctx.Err()
	}
	return data, err
}
