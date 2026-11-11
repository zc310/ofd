package session

import (
	"context"
	"io"

	"github.com/zc310/ofd/pkg/reader"
)

type documentMetadata interface {
	Permissions() ([]reader.DocumentPermissions, error)
	Outline() (reader.OutlineTree, error)
	Preferences() (reader.ViewPreferences, error)
	Info() (reader.DocumentInfo, error)
	FontList() ([]reader.FontInfo, error)
	Stats() (reader.DocumentStats, error)
	Attachments() ([]reader.AttachmentInfo, error)
	Media() ([]reader.MediaInfo, error)
	FontUsage(ctx context.Context, ref reader.FontRef, options reader.FontUsageOptions) (reader.FontUsage, error)
	FontUsageAll(ctx context.Context, options reader.FontUsageOptions) (reader.FontUsageReport, error)
}

type layerDocument interface {
	Layers(index int) ([]reader.LayerInfo, error)
	SetHiddenLayers(index int, ids []string) error
}

type pageMetadataDocument interface {
	signatureDocument
	AnnotationsForPage(index int) ([]reader.AnnotationInfo, error)
	PageLinksForPage(index int) ([]reader.PageLink, error)
	Signatures() ([]reader.SignatureInfo, error)
	MediaData(scope int, mediaID uint64, maxBytes int64) ([]byte, error)
	AttachmentData(scope int, id string, maxBytes int64) ([]byte, error)
	CheckDocumentPermission(action reader.PermissionAction, scope int) error
}

type renderingDocument interface {
	RenderPage(ctx context.Context, output io.Writer, index int, options reader.RenderOptions) error
	RenderPDF(ctx context.Context, output io.Writer, indices []int, options reader.RenderOptions) error
}

type signatureDocument interface {
	SignatureValue(scope int, id string) ([]byte, error)
	SignatureCertificate(scope int, id, slot string) ([]byte, error)
	SignatureSeal(scope int, id string, stampIndex int) ([]byte, string, error)
}

type document interface {
	documentMetadata
	layerDocument
	pageMetadataDocument
	renderingDocument

	Pages() ([]reader.PageInfo, error)
	Page(index int) (reader.PageInfo, error)
	Text(index int) ([]reader.TextRun, error)
	CheckPermission(action reader.PermissionAction, indices []int) error
	Search(ctx context.Context, query string, options reader.SearchOptions) (reader.SearchReport, error)
	Close() error
}
