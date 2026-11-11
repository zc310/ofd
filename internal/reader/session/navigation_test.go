package session

import (
	"context"
	"errors"
	"testing"

	"github.com/zc310/ofd/pkg/reader"
)

type navigationSnapshotDocument struct {
	blockingDocument

	afterRead func()
}

func (d *navigationSnapshotDocument) CustomTags() ([]reader.CustomTagTree, error) {
	if d.afterRead != nil {
		d.afterRead()
	}
	return []reader.CustomTagTree{{Namespace: "urn:test", Root: &reader.CustomTagNode{Name: "根节点"}}}, nil
}

func (d *navigationSnapshotDocument) Versions() ([]reader.VersionInfo, error) {
	if d.afterRead != nil {
		d.afterRead()
	}
	return []reader.VersionInfo{{ID: "v1", Pages: []int{1, 2}}}, nil
}

func (*navigationSnapshotDocument) SelectVersion(int, string) error { return nil }

func TestNavigationReturnsReaderSnapshotsAndRejectsLateResults(t *testing.T) {
	doc := &navigationSnapshotDocument{}
	s := newSession(doc, 1)
	handler, err := NewPageHandler(s)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{session: s, pages: handler}
	t.Cleanup(func() { _ = service.Close() })
	prefix := handler.Prefix()
	tags, err := service.CustomTags(t.Context(), prefix)
	if err != nil || len(tags) != 1 || tags[0].Namespace != "urn:test" || tags[0].Root == nil || tags[0].Root.Name != "根节点" {
		t.Fatalf("标签快照 = %+v, %v", tags, err)
	}
	versions, err := service.Versions(t.Context(), prefix)
	if err != nil || len(versions) != 1 || versions[0].ID != "v1" || len(versions[0].Pages) != 2 {
		t.Fatalf("版本快照 = %+v, %v", versions, err)
	}
	for _, name := range []string{"tags", "versions"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			doc.afterRead = cancel
			if name == "tags" {
				if result, err := service.CustomTags(ctx, prefix); !errors.Is(err, context.Canceled) || result != nil {
					t.Fatalf("取消后的标签快照 = %+v, %v", result, err)
				}
			} else if result, err := service.Versions(ctx, prefix); !errors.Is(err, context.Canceled) || result != nil {
				t.Fatalf("取消后的版本快照 = %+v, %v", result, err)
			}
		})
	}
	if err := service.CloseCurrent(prefix); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CustomTags(t.Context(), prefix); !errors.Is(err, ErrStaleDocument) {
		t.Fatalf("旧会话标签 = %v", err)
	}
	if _, err := service.Versions(t.Context(), prefix); !errors.Is(err, ErrStaleDocument) {
		t.Fatalf("旧会话版本 = %v", err)
	}
}
