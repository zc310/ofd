package export

import (
	"bytes"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
)

func TestAttachmentActionManifestRoundTripPreservesNewWindowOmissionAndFalse(t *testing.T) {
	for _, tc := range []struct {
		name      string
		newWindow *bool
	}{
		{name: "省略"}, {name: "显式false", newWindow: new(false)}, {name: "显式true", newWindow: new(true)},
	} {
		data, err := creator.Marshal(creator.Document{
			ID: "attachment-action", Pages: []creator.Page{{}},
			Attachments: []creator.Attachment{{ID: "note", Name: "note.txt", Data: []byte("说明")}},
			Actions:     []creator.Action{{GotoA: &creator.GotoAAction{AttachID: "note", NewWindow: tc.newWindow}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, format := range []string{"json", "yaml"} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				root := t.TempDir()
				var output bytes.Buffer
				if err := WriteManifest(data, &output, Options{AssetRoot: filepath.Join(root, "assets"), AssetPrefix: "assets", Format: format}); err != nil {
					t.Fatal(err)
				}
				value, err := manifest.Parse(output.Bytes(), format)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(value.Document.Actions[0].GotoA.NewWindow, tc.newWindow) {
					t.Fatalf("清单改变了 NewWindow 的省略或显式值: %s", output.String())
				}
				document, err := value.Build(root, "")
				if err != nil {
					t.Fatal(err)
				}
				roundtrip, err := creator.Marshal(document)
				if err != nil {
					t.Fatal(err)
				}
				ofd, err := parser.NewOFD(roundtrip)
				if err != nil {
					t.Fatal(err)
				}
				defer ofd.Close()
				got := ofd.Documents[0].Actions.Actions[0].GotoA.NewWindow
				if !reflect.DeepEqual(got, tc.newWindow) {
					t.Fatalf("重建 OFD 改变了 NewWindow: %v，期望 %v", got, tc.newWindow)
				}
			})
		}
	}
}
