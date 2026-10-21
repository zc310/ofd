//go:build pdfium

package backends_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/structs"
	"github.com/klippa-app/go-pdfium/webassembly"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/testutil"
)

// pointsToPDF 是毫米到点的换算系数。
const pointsToPDF = 72.0 / 25.4

var _ render.DrawContext = (*pdfiumPageContext)(nil)

// TestPDFiumDirectPDFPrototype 把 OFD 第 5 页画成 PDFium 页面对象并保存 PDF，
// 文字走字形轮廓：PDFium 能栅格化这份 PDF，因此可以直接与 canvas 渲染做
// 像素相似度核对。
//
// 已知限制都源于 PDFium 本身而非核心逻辑：go-pdfium 的编辑接口没有 SMask
// 支持，图片按不透明位图写入，半透明图会与 canvas 参考存在差异。
//
// 150 DPI 实测：PDFium 栅格与 canvas 参考逐像素相似度约 98.5%，对齐 1 像素
// 行数取整差后平均通道误差约 1.6；poppler 栅格同一份 PDF 约 99.2%。
//
// 产物：page-5-pdfium-direct.pdf、page-5-pdfium-direct-pdfium-raster.png
// （PDFium 栅格）、page-5-pdfium-direct-poppler.png（poppler 栅格）、
// page-5-canvas.png（canvas 参考）。
func TestPDFiumDirectPDFPrototype(t *testing.T) {
	runDirectPDFPrototype(t)
}

// runDirectPDFPrototype 生成 PDFium 版页面并与 canvas 渲染比较。
func runDirectPDFPrototype(t *testing.T) {
	const (
		dpi        = 150
		pageNumber = 5
	)
	input := filepath.Join("..", "..", "..", "testdata", "ofdrw", "intro.ofd")
	ofd, err := parser.NewOFD(input)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := render.NewDocumentWithDPI(color.White, ofd.Documents[0], dpi)
	defer doc.Close()
	if len(doc.Pages) < pageNumber {
		t.Fatalf("document has %d pages; cannot compare page %d", len(doc.Pages), pageNumber)
	}
	box, err := doc.Pages[pageNumber-1].PhysicalBox()
	if err != nil {
		t.Fatal(err)
	}

	pool, err := webassembly.Init(webassembly.Config{MinIdle: 1, MaxIdle: 1, MaxTotal: 1})
	if err != nil {
		t.Fatalf("initialize PDFium WebAssembly runtime: %v", err)
	}
	defer func() {
		if err := pool.Close(); err != nil {
			t.Errorf("close PDFium pool: %v", err)
		}
	}()
	instance, err := pool.GetInstance(30 * time.Second)
	if err != nil {
		t.Fatalf("get PDFium instance: %v", err)
	}
	defer func() {
		if err := instance.Close(); err != nil {
			t.Errorf("close PDFium instance: %v", err)
		}
	}()

	pdfDoc, err := instance.FPDF_CreateNewDocument(&requests.FPDF_CreateNewDocument{})
	if err != nil {
		t.Fatalf("create PDFium document: %v", err)
	}
	defer func() {
		if _, err := instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: pdfDoc.Document}); err != nil {
			t.Errorf("close PDFium document: %v", err)
		}
	}()
	pageResp, err := instance.FPDFPage_New(&requests.FPDFPage_New{
		Document:  pdfDoc.Document,
		PageIndex: 0,
		Width:     box.Width * 72 / 25.4,
		Height:    box.Height * 72 / 25.4,
	})
	if err != nil {
		t.Fatalf("create PDFium page: %v", err)
	}
	pageRef := pageResp.Page
	page := requests.Page{ByReference: &pageRef}
	defer func() {
		if _, err := instance.FPDF_ClosePage(&requests.FPDF_ClosePage{Page: pageRef}); err != nil {
			t.Errorf("close PDFium page: %v", err)
		}
	}()

	ctx := &pdfiumPageContext{
		instance: instance,
		document: pdfDoc.Document,
		page:     page,
		pageW:    box.Width,
		pageH:    box.Height,
		state: pdfiumDrawState{
			matrix: geom.Identity,
			fill:   color.White,
		},
	}
	if err := doc.Draw(ctx, doc.Pages[pageNumber-1]); err != nil {
		t.Fatalf("draw OFD page into PDFium objects: %v", err)
	}
	defer func() {
		for _, bitmap := range ctx.bitmaps {
			if _, err := instance.FPDFBitmap_Destroy(&requests.FPDFBitmap_Destroy{Bitmap: bitmap}); err != nil {
				t.Errorf("destroy PDFium source bitmap: %v", err)
			}
		}
	}()
	if ctx.err != nil {
		t.Fatalf("write PDFium page object: %v", ctx.err)
	}
	if _, err := instance.FPDFPage_GenerateContent(&requests.FPDFPage_GenerateContent{Page: page}); err != nil {
		t.Fatalf("generate PDF page content: %v", err)
	}
	outputDir := filepath.Join(os.TempDir(), "ofd-pdfium-prototype")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("create output directory: %v", err)
	}
	name := "page-5-pdfium-direct"
	pdfPath := filepath.Join(outputDir, name+".pdf")
	if _, err := instance.FPDF_SaveAsCopy(&requests.FPDF_SaveAsCopy{
		Flags: requests.SaveFlagNoIncremental, Document: pdfDoc.Document, FilePath: &pdfPath,
	}); err != nil {
		t.Fatalf("save PDFium-authored PDF: %v", err)
	}

	result, err := instance.RenderPageInDPI(&requests.RenderPageInDPI{
		DPI:  dpi,
		Page: page,
	})
	if err != nil {
		t.Fatalf("render PDFium-authored page: %v", err)
	}
	defer result.Cleanup()
	img, ok := result.Result.RenderedImage.(*image.RGBA)
	if !ok || img.Bounds().Empty() {
		t.Fatalf("PDFium returned invalid raster: %T", result.Result.RenderedImage)
	}
	nonBlack := 0
	for i := 0; i+3 < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 || img.Pix[i+1] != 0 || img.Pix[i+2] != 0 {
			nonBlack++
		}
	}
	if nonBlack == 0 {
		t.Fatal("PDFium-authored page contains no visible content")
	}
	writePrototypePNG(t, filepath.Join(outputDir, name+"-pdfium-raster.png"), img)
	canvasImage, err := doc.RasterizePage(doc.Pages[pageNumber-1], render.BackendCanvas, geom.DPI(dpi))
	if err != nil {
		t.Fatalf("render canvas comparison page: %v", err)
	}
	writePrototypePNG(t, filepath.Join(outputDir, "page-5-canvas.png"), canvasImage)
	changed, meanDelta := comparePrototypeImages(img, canvasImage)
	t.Logf("%s: size=%v; canvas=%v; changed_pixels=%.2f%%; mean_channel_delta=%.2f; paths=%d text_paths=%d filled=%d stroked=%d outside_paths=%d gradient_paths=%d alpha_images=%d",
		name, img.Bounds().Size(), canvasImage.Bounds().Size(), changed, meanDelta,
		ctx.pathObjects, ctx.textPaths, ctx.filledPaths, ctx.strokedPaths, ctx.outsidePaths, ctx.gradientPaths, len(ctx.bitmaps))
	writePopplerRaster(t, outputDir, name+"-poppler.png", pdfPath, dpi)
	t.Logf("direct PDFium outputs: %s", outputDir)
}

// writePopplerRaster 用 pdftoppm 栅格化 PDF 并保存，用于交叉核对 PDF 的观感。
func writePopplerRaster(t *testing.T, outputDir, name, pdfPath string, dpi int) {
	t.Helper()
	tool, err := exec.LookPath("pdftoppm")
	if err != nil {
		t.Logf("缺少 pdftoppm，跳过 %s", name)
		return
	}
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatalf("read PDF for rasterization: %v", err)
	}
	pages, err := testutil.RasterizePDFPages(t.Context(), tool, data, dpi)
	if err != nil {
		t.Fatalf("rasterize PDF with pdftoppm: %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("pdftoppm did not produce any page image")
	}
	writePrototypePNG(t, filepath.Join(outputDir, name), pages[0])
}

type pdfiumDrawState struct {
	matrix       geom.Matrix
	fill         color.Color
	fillGradient geom.Gradient
	stroke       color.Color
	strokeGrad   geom.Gradient
	fillRule     geom.FillRule
	strokeWidth  float64
	dashes       []float64
	dashOffset   float64
	cap          geom.Capper
	join         geom.Joiner
}

type pdfiumPageContext struct {
	instance      pdfium.Pdfium
	document      references.FPDF_DOCUMENT
	page          requests.Page
	pageW         float64
	pageH         float64
	state         pdfiumDrawState
	stack         []pdfiumDrawState
	err           error
	gradientPaths int
	bitmaps       []references.FPDF_BITMAP
	pathObjects   int
	filledPaths   int
	strokedPaths  int
	textPaths     int
	outsidePaths  int
}

func (c *pdfiumPageContext) Push() { c.stack = append(c.stack, c.state) }
func (c *pdfiumPageContext) Pop() {
	if n := len(c.stack); n > 0 {
		c.state = c.stack[n-1]
		c.stack = c.stack[:n-1]
	}
}
func (c *pdfiumPageContext) Translate(x, y float64)     { c.Transform(geom.Identity.Translate(x, y)) }
func (c *pdfiumPageContext) Scale(x, y float64)         { c.Transform(geom.Identity.Scale(x, y)) }
func (c *pdfiumPageContext) Rotate(deg float64)         { c.Transform(geom.Identity.Rotate(deg)) }
func (c *pdfiumPageContext) Transform(m geom.Matrix)    { c.state.matrix = c.state.matrix.Mul(m) }
func (c *pdfiumPageContext) CurrentMatrix() geom.Matrix { return c.state.matrix }
func (c *pdfiumPageContext) SetFillColor(value color.Color) {
	c.state.fill, c.state.fillGradient = value, nil
}
func (c *pdfiumPageContext) SetFillGradient(value geom.Gradient) {
	c.state.fill, c.state.fillGradient = nil, value
}
func (c *pdfiumPageContext) SetFillPaint(value geom.Paint) {
	c.state.fill, c.state.fillGradient = value.Color, value.Gradient
}
func (c *pdfiumPageContext) ClearFill() { c.state.fill, c.state.fillGradient = nil, nil }
func (c *pdfiumPageContext) SetStrokeColor(value color.Color) {
	c.state.stroke, c.state.strokeGrad = value, nil
}
func (c *pdfiumPageContext) SetStrokeGradient(value geom.Gradient) {
	c.state.stroke, c.state.strokeGrad = nil, value
}
func (c *pdfiumPageContext) SetStrokePaint(value geom.Paint) {
	c.state.stroke, c.state.strokeGrad = value.Color, value.Gradient
}
func (c *pdfiumPageContext) ClearStroke() { c.state.stroke, c.state.strokeGrad = nil, nil }
func (c *pdfiumPageContext) CopyStrokeToFill() {
	c.state.fill, c.state.fillGradient = c.state.stroke, c.state.strokeGrad
}
func (c *pdfiumPageContext) SetStrokeWidth(value float64) { c.state.strokeWidth = value }
func (c *pdfiumPageContext) SetDashes(offset float64, values ...float64) {
	c.state.dashOffset, c.state.dashes = offset, append([]float64(nil), values...)
}
func (c *pdfiumPageContext) SetStrokeCapper(value geom.Capper) { c.state.cap = value }
func (c *pdfiumPageContext) SetStrokeJoiner(value geom.Joiner) { c.state.join = value }
func (c *pdfiumPageContext) StrokeWidth() float64              { return c.state.strokeWidth }
func (c *pdfiumPageContext) StrokeCapper() geom.Capper         { return c.state.cap }
func (c *pdfiumPageContext) StrokeJoiner() geom.Joiner         { return c.state.join }
func (c *pdfiumPageContext) SetFillRule(value geom.FillRule)   { c.state.fillRule = value }

func (c *pdfiumPageContext) TextPath(path *geom.Path, x, y float64) {
	c.textPaths++
	c.DrawPath(x, y, path)
}
func (c *pdfiumPageContext) DrawImage(img image.Image, x, y, dpmm float64) {
	if dpmm <= 0 {
		c.err = fmt.Errorf("invalid image resolution %g dpmm", dpmm)
		return
	}
	c.drawImage(img, c.state.matrix.Translate(x, y), 1/dpmm, 1/dpmm)
}
func (c *pdfiumPageContext) RenderImage(img image.Image, matrix geom.Matrix) {
	c.drawImage(img, matrix, 1, 1)
}

func (c *pdfiumPageContext) drawImage(img image.Image, matrix geom.Matrix, sx, sy float64) {
	if c.err != nil || img == nil {
		return
	}
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	if width == 0 || height == 0 {
		return
	}
	flat := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(flat, flat.Bounds(), img, img.Bounds().Min, draw.Src)
	bitmap, err := c.instance.FPDFBitmap_Create(&requests.FPDFBitmap_Create{Width: width, Height: height, Alpha: 1})
	if err != nil {
		c.err = fmt.Errorf("create alpha bitmap: %w", err)
		return
	}
	stride, err := c.instance.FPDFBitmap_GetStride(&requests.FPDFBitmap_GetStride{Bitmap: bitmap.Bitmap})
	if err != nil {
		c.err = fmt.Errorf("get alpha bitmap stride: %w", err)
		return
	}
	buffer, err := c.instance.FPDFBitmap_GetBuffer(&requests.FPDFBitmap_GetBuffer{Bitmap: bitmap.Bitmap})
	if err != nil {
		c.err = fmt.Errorf("get alpha bitmap buffer: %w", err)
		return
	}
	for y := 0; y < height; y++ {
		dst := buffer.Buffer[y*stride.Stride : y*stride.Stride+width*4]
		src := flat.Pix[y*flat.Stride : y*flat.Stride+width*4]
		for x := 0; x < width; x++ {
			dst[x*4+0] = src[x*4+2]
			dst[x*4+1] = src[x*4+1]
			dst[x*4+2] = src[x*4+0]
			dst[x*4+3] = src[x*4+3]
		}
	}
	imageObject, err := c.instance.FPDFPageObj_NewImageObj(&requests.FPDFPageObj_NewImageObj{Document: c.document})
	if err != nil {
		c.err = err
		return
	}
	if _, err := c.instance.FPDFImageObj_SetBitmap(&requests.FPDFImageObj_SetBitmap{
		ImageObject: imageObject.PageObject,
		Bitmap:      bitmap.Bitmap,
	}); err != nil {
		c.err = fmt.Errorf("attach alpha bitmap: %w", err)
		return
	}
	const pointsToPDF = 72.0 / 25.4
	m := matrix
	transform := structs.FPDF_FS_MATRIX{
		A: float32(pointsToPDF * m[0][0] * sx * float64(width)),
		B: float32(pointsToPDF * m[1][0] * sx * float64(width)),
		C: float32(pointsToPDF * m[0][1] * sy * float64(height)),
		D: float32(pointsToPDF * m[1][1] * sy * float64(height)),
		E: float32(pointsToPDF * m[0][2]),
		F: float32(pointsToPDF * m[1][2]),
	}
	if _, err := c.instance.FPDFImageObj_SetMatrix(&requests.FPDFImageObj_SetMatrix{ImageObject: imageObject.PageObject, Transform: transform}); err != nil {
		c.err = fmt.Errorf("set PDFium image matrix: %w", err)
		return
	}
	if _, err := c.instance.FPDFPage_InsertObject(&requests.FPDFPage_InsertObject{Page: c.page, PageObject: imageObject.PageObject}); err != nil {
		c.err = fmt.Errorf("insert PDFium image object: %w", err)
		return
	}
	c.bitmaps = append(c.bitmaps, bitmap.Bitmap)
}

func (c *pdfiumPageContext) DrawPath(x, y float64, path *geom.Path) {
	if c.err != nil || path == nil || path.Empty() {
		return
	}
	transformed := path.Flatten(0.01).Transform(c.state.matrix.Translate(x, y))
	bounds := transformed.Bounds()
	if bounds.X0 < -0.01 || bounds.Y0 < -0.01 || bounds.X1 > c.pageW+0.01 || bounds.Y1 > c.pageH+0.01 {
		c.outsidePaths++
	}
	data := transformed.Data()
	const pointsToPDF = 72.0 / 25.4
	var object references.FPDF_PAGEOBJECT
	initialized := false
	for i := 0; i < len(data); {
		command := data[i]
		switch command {
		case geom.MoveToCmd:
			p := c.toPDFPoint(data[i+1], data[i+2], pointsToPDF)
			if !initialized {
				created, err := c.instance.FPDFPageObj_CreateNewPath(&requests.FPDFPageObj_CreateNewPath{X: float32(p.X), Y: float32(p.Y)})
				if err != nil {
					c.err = err
					return
				}
				object, initialized = created.PageObject, true
			} else if _, err := c.instance.FPDFPath_MoveTo(&requests.FPDFPath_MoveTo{PageObject: object, X: float32(p.X), Y: float32(p.Y)}); err != nil {
				c.err = err
				return
			}
			i += 4
		case geom.LineToCmd:
			p := c.toPDFPoint(data[i+1], data[i+2], pointsToPDF)
			if _, err := c.instance.FPDFPath_LineTo(&requests.FPDFPath_LineTo{PageObject: object, X: float32(p.X), Y: float32(p.Y)}); err != nil {
				c.err = err
				return
			}
			i += 4
		case geom.CloseCmd:
			if _, err := c.instance.FPDFPath_Close(&requests.FPDFPath_Close{PageObject: object}); err != nil {
				c.err = err
				return
			}
			i += 4
		default:
			c.err = fmt.Errorf("unsupported flattened path command %v", command)
			return
		}
	}
	if !initialized {
		return
	}

	fillColor := c.state.fill
	if c.state.fillGradient != nil {
		fillColor = c.state.fillGradient.At(0, 0)
		c.gradientPaths++
	}
	strokeColor := c.state.stroke
	if c.state.strokeGrad != nil {
		strokeColor = c.state.strokeGrad.At(0, 0)
		c.gradientPaths++
	}
	fill, stroke := fillColor != nil, strokeColor != nil
	c.pathObjects++
	if fill {
		c.filledPaths++
	}
	if stroke {
		c.strokedPaths++
	}
	mode := enums.FPDF_FILLMODE_NONE
	if fill {
		mode = enums.FPDF_FILLMODE_WINDING
		if c.state.fillRule == geom.EvenOdd {
			mode = enums.FPDF_FILLMODE_ALTERNATE
		}
		if _, err := c.instance.FPDFPageObj_SetFillColor(&requests.FPDFPageObj_SetFillColor{PageObject: object, FillColor: pdfiumColor(fillColor)}); err != nil {
			c.err = err
			return
		}
	}
	if stroke {
		if _, err := c.instance.FPDFPageObj_SetStrokeColor(&requests.FPDFPageObj_SetStrokeColor{PageObject: object, StrokeColor: pdfiumColor(strokeColor)}); err != nil {
			c.err = err
			return
		}
		scale := math.Sqrt(math.Abs(c.state.matrix[0][0]*c.state.matrix[1][1] - c.state.matrix[0][1]*c.state.matrix[1][0]))
		if _, err := c.instance.FPDFPageObj_SetStrokeWidth(&requests.FPDFPageObj_SetStrokeWidth{PageObject: object, StrokeWidth: float32(c.state.strokeWidth * scale * pointsToPDF)}); err != nil {
			c.err = err
			return
		}
		cap := enums.FPDF_LINECAP_BUTT
		switch c.state.cap.(type) {
		case geom.RoundCapper:
			cap = enums.FPDF_LINECAP_ROUND
		case geom.SquareCapper:
			cap = enums.FPDF_LINECAP_PROJECTING_SQUAR
		}
		if _, err := c.instance.FPDFPageObj_SetLineCap(&requests.FPDFPageObj_SetLineCap{PageObject: object, LineCap: cap}); err != nil {
			c.err = err
			return
		}
		join := enums.FPDF_LINEJOIN_MITER
		switch c.state.join.(type) {
		case geom.RoundJoiner:
			join = enums.FPDF_LINEJOIN_ROUND
		case geom.BevelJoiner:
			join = enums.FPDF_LINEJOIN_BEVEL
		}
		if _, err := c.instance.FPDFPageObj_SetLineJoin(&requests.FPDFPageObj_SetLineJoin{PageObject: object, LineJoin: join}); err != nil {
			c.err = err
			return
		}
		if len(c.state.dashes) > 0 {
			dashes := make([]float32, len(c.state.dashes))
			for i, dash := range c.state.dashes {
				dashes[i] = float32(dash * scale * pointsToPDF)
			}
			if _, err := c.instance.FPDFPageObj_SetDashArray(&requests.FPDFPageObj_SetDashArray{PageObject: object, DashArray: dashes, DashPhase: float32(c.state.dashOffset * scale * pointsToPDF)}); err != nil {
				c.err = err
				return
			}
		}
	}
	if _, err := c.instance.FPDFPath_SetDrawMode(&requests.FPDFPath_SetDrawMode{PageObject: object, FillMode: mode, Stroke: stroke}); err != nil {
		c.err = err
		return
	}
	if _, err := c.instance.FPDFPage_InsertObject(&requests.FPDFPage_InsertObject{Page: c.page, PageObject: object}); err != nil {
		c.err = err
	}
}

// toPDFPoint 把已变换到设备坐标（毫米、y 向上）的点换算成 PDF 点。
// 路径已经在 DrawPath 里乘过当前矩阵，这里只做单位换算。
func (c *pdfiumPageContext) toPDFPoint(x, y, factor float64) geom.Point {
	return geom.Point{X: x * factor, Y: y * factor}
}

func pdfiumColor(value color.Color) structs.FPDF_COLOR {
	nrgba := color.NRGBAModel.Convert(value).(color.NRGBA)
	return structs.FPDF_COLOR{R: uint(nrgba.R), G: uint(nrgba.G), B: uint(nrgba.B), A: uint(nrgba.A)}
}

func comparePrototypeImages(a, b *image.RGBA) (changedPercent, meanDelta float64) {
	width, height := min(a.Bounds().Dx(), b.Bounds().Dx()), min(a.Bounds().Dy(), b.Bounds().Dy())
	changed := 0
	var delta uint64
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			pa, pb := a.RGBAAt(x, y), b.RGBAAt(x, y)
			if pa != pb {
				changed++
			}
			for _, pair := range [][2]uint8{{pa.R, pb.R}, {pa.G, pb.G}, {pa.B, pb.B}, {pa.A, pb.A}} {
				d := int(pair[0]) - int(pair[1])
				if d < 0 {
					d = -d
				}
				delta += uint64(d)
			}
		}
	}
	pixels := width * height
	return 100 * float64(changed) / float64(pixels), float64(delta) / float64(pixels*4)
}

func writePrototypePNG(t testing.TB, path string, img image.Image) {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	if err := os.WriteFile(path, data.Bytes(), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
