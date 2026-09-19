package creator

type packageState struct {
	entries []zipEntry
}

type zipEntry struct {
	name string
	data []byte
}

type buildState struct {
	document               Document
	pages                  PageProvider
	pageCount              int
	completeTextCodeDeltas bool
	pageSize               PageSize
	pageIDs                []uint64
	templateIDs            []uint64
	templateLayers         [][]builtLayer
	drawParams             []drawParamResource
	drawParamIDs           map[string]uint64
	drawParamIndexes       map[string]int
	fonts                  []fontResource
	fontIDs                map[string]uint64
	images                 []imageResource
	pageImageIDs           map[uint64]bool
	pendingPageImageIDs    map[uint64]bool
	media                  []mediaResource
	mediaIDs               map[uint64]bool
	mediaTypes             map[uint64]string
	composites             []compositeResource
	compositeIDs           map[uint64]int
	annotationPages        []annotationResource
	attachments            []attachmentResource
	attachmentIDs          map[string]bool
	bookmarkNames          map[string]bool
	customTags             []customTagResource
	extensions             []extensionResource
	signatures             []signatureResource
	versions               []versionResource
	coverName              string
	coverData              []byte
	coverSource            DataSource
	colorProfiles          map[uint64]colorProfileResource
	colorSpaces            map[uint64]colorSpaceInfo
	publicResources        []publicResource
	rawReferences          []rawResourceReference
	rawDrawRelations       map[uint64]uint64
	patterns               []*Pattern
	patternSet             map[*Pattern]bool
	maxID                  uint64
	nextID                 uint64
	usedIDs                map[uint64]bool
	reservedIDs            map[uint64]string
	allocationError        bool
}

type builtItem struct {
	id        uint64
	item      Item
	font      uint64
	image     uint64
	drawParam uint64
	composite uint64
	pageBlock []builtItem
}

type builtLayer struct {
	id        uint64
	layerType string
	drawParam uint64
	items     []builtItem
}

type drawParamResource struct {
	id           uint64
	name         string
	relativeName string
	relative     uint64
	lineWidth    float64
	join         string
	cap          string
	dashOffset   float64
	dashPattern  []float64
	miterLimit   float64
	fillColor    *Color
	strokeColor  *Color
}

type fontResource struct {
	id         uint64
	name       string
	familyName string
	charset    string
	italic     bool
	bold       bool
	serif      bool
	fixedWidth bool
	fileName   string
	data       []byte
	source     DataSource
}

type imageResource struct {
	id     uint64
	name   string
	data   []byte
	source DataSource
	format string
}

type pageResource struct {
	name   string
	images []pageImageResource
	data   []byte
	files  []pageResourceFile
}

type pageResourceFile struct {
	path   string
	data   []byte
	source DataSource
}

type pageImageResource struct {
	id     uint64
	name   string
	format string
	data   []byte
	source DataSource
}

type mediaResource struct {
	id     uint64
	name   string
	type_  string
	format string
	data   []byte
	source DataSource
}

type compositeResource struct {
	id           uint64
	width        float64
	height       float64
	thumbnail    uint64
	substitution uint64
	layers       []builtLayer
}

type annotationResource struct {
	pageID uint64
	items  []builtAnnotation
}

type attachmentResource struct {
	value Attachment
	name  string
}

type customTagResource struct {
	value      CustomTag
	schemaName string
	dataName   string
}

type extensionResource struct {
	value    Extension
	dataName string
}

type signatureResource struct {
	value     Signature
	baseName  string
	sealName  string
	valueName string
}

type versionResource struct {
	value    DocumentVersion
	baseName string
	rootName string
}

type colorProfileResource struct {
	name string
	data []byte
}

type colorSpaceInfo struct {
	channels    int
	bits        int
	paletteSize int
}

type publicResource struct {
	name  string
	data  []byte
	files []publicResourceFile
}

type publicResourceFile struct {
	path   string
	data   []byte
	source DataSource
}

type rawResourceReference struct {
	kind   string
	value  uint64
	text   string
	source string
}

type builtAnnotation struct {
	value Annotation
	items []builtItem
}
