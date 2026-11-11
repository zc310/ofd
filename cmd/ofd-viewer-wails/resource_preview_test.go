package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/gif"
	"strconv"
	"testing"

	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/reader"
)

func TestNativeBodyAttachmentLinksAndResourcePreviewsRespectDifferentPermissions(t *testing.T) {
	var picture bytes.Buffer
	if err := gif.Encode(&picture, image.NewGray(image.Rect(0, 0, 4, 3)), nil); err != nil {
		t.Fatal(err)
	}
	wav := make([]byte, 124)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1)
	binary.LittleEndian.PutUint16(wav[22:24], 1)
	binary.LittleEndian.PutUint32(wav[24:28], 8000)
	binary.LittleEndian.PutUint32(wav[28:32], 16000)
	binary.LittleEndian.PutUint16(wav[32:34], 2)
	binary.LittleEndian.PutUint16(wav[34:36], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:44], 80)
	for _, allowed := range []bool{true, false} {
		t.Run(strconv.FormatBool(allowed), func(t *testing.T) {
			data, err := creator.Marshal(creator.Document{ID: "resource-preview", Permissions: &creator.Permissions{Export: &allowed},
				Media: []creator.Media{{ID: 200, Type: "Audio", Format: "wav", Name: "sound.wav", Data: wav},
					{ID: 201, Type: "Image", Format: "gif", Name: "image.gif", Data: picture.Bytes()}},
				Attachments: []creator.Attachment{{ID: "pic", Name: "附件.gif", FileName: "image.gif", Data: picture.Bytes()},
					{ID: "sound", Name: "附件.wav", FileName: "sound.wav", Data: wav}},
				Pages: []creator.Page{{Items: []creator.Item{creator.Path{X: 1, Y: 1, Width: 10, Height: 10, Data: "M 0 0 L 10 10", Stroke: true,
					Actions: []creator.Action{
						{Event: creator.ActionEventClick, GotoA: &creator.GotoAAction{AttachID: "pic"}},
						{Event: creator.ActionEventClick, Sound: &creator.SoundAction{ResourceID: 200}},
						{Event: creator.ActionEventClick, URI: &creator.URIAction{URI: "https://example.test"}},
					}}}}}})
			if err != nil {
				t.Fatal(err)
			}
			d, opened := capabilityDesktop(t, data)
			links, err := d.PageLinksForPage(t.Context(), opened.PagePrefix, 0)
			if err != nil || len(links) != 1 || len(links[0].Actions) != 3 || links[0].Actions[0].AttachmentID != "pic" {
				t.Fatalf("正文动作未保留: %+v, %v", links, err)
			}
			if preview, err := d.MediaPlayback(t.Context(), opened.PagePrefix, 0, 200, 0); err != nil || !bytes.Equal(preview.Data, wav) {
				t.Fatalf("资源播放不能要求导出权限: %v", err)
			}
			if preview, err := d.MediaImagePreview(t.Context(), opened.PagePrefix, 0, 201); err != nil || preview.MIME != "image/png" {
				t.Fatalf("GIF 转 PNG 预览 = %s, %v", preview.MIME, err)
			}
			if thumbnail, err := d.MediaThumbnail(t.Context(), opened.PagePrefix, 0, 201); err != nil || thumbnail.Width != 4 || thumbnail.Height != 3 {
				t.Fatalf("GIF 缩略图 = %+v, %v", thumbnail, err)
			}
			_, imageErr := d.AttachmentImagePreview(t.Context(), opened.PagePrefix, 0, "pic")
			_, soundErr := d.AttachmentPlayback(t.Context(), opened.PagePrefix, 0, "sound")
			if allowed {
				if imageErr != nil || soundErr != nil {
					t.Fatalf("允许导出时附件预览失败: %v, %v", imageErr, soundErr)
				}
			} else if !errors.Is(imageErr, reader.ErrPermissionDenied) || !errors.Is(soundErr, reader.ErrPermissionDenied) {
				t.Fatalf("附件预览绕过导出: %v, %v", imageErr, soundErr)
			}
			if reply, err := d.CheckDocumentPermission(opened.PagePrefix, 0, "export"); err != nil || (reply.Denied == nil) != allowed {
				t.Fatalf("文档体权限 = %+v, %v", reply, err)
			}
			if err := d.Close(opened.PagePrefix); err != nil {
				t.Fatal(err)
			}
			if _, err := d.MediaPlayback(t.Context(), opened.PagePrefix, 0, 200, 0); !errors.Is(err, readersession.ErrStaleDocument) {
				t.Fatal(err)
			}
			if _, err := d.AttachmentImagePreview(t.Context(), opened.PagePrefix, 0, "pic"); !errors.Is(err, readersession.ErrStaleDocument) {
				t.Fatal(err)
			}
		})
	}
}
