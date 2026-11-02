package pdf

import (
	"fmt"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

// RenderCanvas embeds an immutable canvas as a Form XObject, reused by identity
// within this PDF. Other PDFs get independent objects and resource references.
func (r *PDF) RenderCanvas(scene *canvas.Canvas, m canvas.Matrix) {
	r.RenderCanvasWith(scene, m, func() { scene.RenderTo(r) })
}

// RenderCanvasWith is RenderCanvas with an optional renderer adapter. draw is
// called synchronously, only when the Form is first built, with r temporarily
// targeting the Form. The callback must render the scene in local coordinates
// and must not add pages, links, anchors or outlines. The same scene identity
// must always use equivalent rendering settings.
func (r *PDF) RenderCanvasWith(scene *canvas.Canvas, m canvas.Matrix, draw func()) {
	if scene == nil || scene.Empty() || scene.W <= 0 || scene.H <= 0 {
		return
	}
	writer := r.w.pdf
	ref, ok := writer.forms[scene]
	if !ok {
		form := writer.newContentWriter(scene.W, scene.H)
		// Forms inherit the caller's graphics state. Establish the defaults that
		// the independent content writer assumes, including text spacing and alpha.
		form.WriteString(" 0 g 0 G 1 w 0 J 0 j 10 M [] 0 d BT 0 Tc 0 Tr ET")
		form.alpha = -1
		form.SetAlpha(1)
		func() {
			page, width, height := r.w, r.width, r.height
			r.w, r.width, r.height = form, scene.W, scene.H
			defer func() { r.w, r.width, r.height = page, width, height }()
			if draw != nil {
				draw()
			} else {
				scene.RenderTo(r)
			}
		}()
		// Replaying a Canvas does not clip to its declared size. Include content
		// outside that rectangle and leave padding for stroke/antialias coverage;
		// clipping again at the exact page edge changes its last pixel column.
		bounds := scene.Bounds().Add(canvas.Rect{X1: scene.W, Y1: scene.H}).Expand(1)
		dict := pdfDict{
			"Type": pdfName("XObject"), "Subtype": pdfName("Form"), "FormType": 1,
			"BBox": pdfArray{bounds.X0, bounds.Y0, bounds.X1, bounds.Y1}, "Resources": form.resources,
		}
		if writer.compress {
			dict["Filter"] = pdfFilterFlate
		}
		ref = writer.writeObject(pdfStream{dict: dict, stream: form.Bytes()})
		// Bound both retained scene pointers and drawing memory. Evicted Forms
		// remain valid PDF objects; large scenes are written without retention.
		weight := scene.MemorySize()
		if weight <= 32<<20 {
			if writer.forms == nil || len(writer.forms) >= 64 || writer.formBytes > (32<<20)-weight {
				writer.forms = make(map[*canvas.Canvas]pdfRef)
				writer.formBytes = 0
			}
			writer.forms[scene] = ref
			writer.formBytes += weight
		}
	}
	if r.w.resources["XObject"] == nil {
		r.w.resources["XObject"] = pdfDict{}
	}
	name := pdfName(fmt.Sprintf("Fm%d", ref))
	r.w.resources["XObject"].(pdfDict)[name] = ref
	// q/Q isolates the Form from both preceding and subsequent page content.
	state := r.w.pushState()
	defer r.w.popState(state)
	_, _ = fmt.Fprintf(r.w, " %v %v %v %v %v %v cm /%s Do", dec(m[0][0]), dec(m[1][0]), dec(m[0][1]), dec(m[1][1]), dec(m[0][2]), dec(m[1][2]), name)
}
