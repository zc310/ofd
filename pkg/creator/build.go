package creator

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/beevik/etree"
)

func build(document Document) (*packageState, error) {
	state, err := prepare(document)
	if err != nil {
		return nil, err
	}
	defer state.clearPatternCaches()

	root, err := rootXML(state)
	if err != nil {
		return nil, err
	}
	documentXMLData, err := documentXML(state)
	if err != nil {
		return nil, err
	}
	entries := []zipEntry{
		{name: "OFD.xml", data: root},
		{name: docDir + "/Document.xml", data: documentXMLData},
	}
	if len(state.coverData) > 0 {
		entries = append(entries, zipEntry{name: docDir + "/Cover/" + state.coverName, data: state.coverData})
	}
	for _, resource := range state.publicResources {
		entries = append(entries, zipEntry{name: docDir + "/" + resource.name, data: resource.data})
		base := path.Dir(resource.name)
		for _, file := range resource.files {
			entries = append(entries, zipEntry{name: path.Join(docDir, base, file.path), data: file.data})
		}
	}
	if len(state.attachments) > 0 {
		attachmentsData, attachmentsErr := attachmentsXML(state)
		if attachmentsErr != nil {
			return nil, attachmentsErr
		}
		entries = append(entries, zipEntry{name: docDir + "/Attachments/Attachments.xml", data: attachmentsData})
		for _, attachment := range state.attachments {
			entries = append(entries, zipEntry{name: attachmentPath(attachment.name), data: attachment.value.Data})
		}
	}
	if len(state.customTags) > 0 {
		customTagsData, customTagsErr := customTagsXML(state)
		if customTagsErr != nil {
			return nil, customTagsErr
		}
		entries = append(entries, zipEntry{name: docDir + "/CustomTags/CustomTags.xml", data: customTagsData})
		for _, tag := range state.customTags {
			entries = append(entries, zipEntry{name: customTagDataPath(tag.dataName), data: tag.value.Data})
			if len(tag.value.Schema) > 0 {
				entries = append(entries, zipEntry{name: customTagSchemaPath(tag.schemaName), data: tag.value.Schema})
			}
		}
	}
	if len(state.extensions) > 0 {
		extensionsData, extensionsErr := extensionsXML(state)
		if extensionsErr != nil {
			return nil, extensionsErr
		}
		entries = append(entries, zipEntry{name: docDir + "/Extensions/Extensions.xml", data: extensionsData})
		for _, extension := range state.extensions {
			if len(extension.value.DataFile) > 0 {
				entries = append(entries, zipEntry{name: extensionDataPath(extension.dataName), data: extension.value.DataFile})
			}
		}
	}
	if len(state.versions) > 0 {
		for _, version := range state.versions {
			versionData, versionErr := versionXML(version)
			if versionErr != nil {
				return nil, versionErr
			}
			entries = append(entries, zipEntry{name: versionPath(version.baseName), data: versionData})
			if len(version.value.DocRoot) > 0 {
				entries = append(entries, zipEntry{name: versionRootPath(version.rootName), data: version.value.DocRoot})
			}
		}
	}
	for pageIndex := range state.document.Pages {
		pageData, pageErr := pageXML(state, pageIndex)
		if pageErr != nil {
			return nil, pageErr
		}
		entries = append(entries, zipEntry{
			name: pagePath(pageIndex),
			data: pageData,
		})
		for resourceIndex, resource := range state.pageResources[pageIndex] {
			resourceData, resourceErr := pageResourceXML(resource)
			if resourceErr != nil {
				return nil, resourceErr
			}
			entries = append(entries, zipEntry{name: pageResourcePath(pageIndex, resourceIndex), data: resourceData})
			for _, file := range resource.files {
				entries = append(entries, zipEntry{name: path.Join(docDir, "Pages", fmt.Sprintf("Page_%d", pageIndex), file.path), data: file.data})
			}
			for _, image := range resource.images {
				entries = append(entries, zipEntry{name: pageResourceImagePath(pageIndex, image.id, image.name), data: image.data})
			}
		}
	}
	for templateIndex := range state.document.Templates {
		templateData, templateErr := templateXML(state, templateIndex)
		if templateErr != nil {
			return nil, templateErr
		}
		entries = append(entries, zipEntry{name: templatePath(templateIndex), data: templateData})
	}
	if len(state.annotationPages) > 0 {
		annotationsData, annotationsErr := annotationsXML(state)
		if annotationsErr != nil {
			return nil, annotationsErr
		}
		entries = append(entries, zipEntry{name: docDir + "/Annotations.xml", data: annotationsData})
		for _, annotationPage := range state.annotationPages {
			pageData, pageErr := pageAnnotationsXML(state, annotationPage)
			if pageErr != nil {
				return nil, pageErr
			}
			entries = append(entries, zipEntry{name: annotationPath(annotationPage.pageID), data: pageData})
		}
	}
	if len(state.drawParams) > 0 || len(state.fonts) > 0 || len(state.images) > 0 || len(state.media) > 0 || len(state.composites) > 0 || len(state.document.ColorSpaces) > 0 {
		resourceData, resourceErr := resourceXML(state)
		if resourceErr != nil {
			return nil, resourceErr
		}
		entries = append(entries, zipEntry{
			name: docDir + "/DocumentRes.xml",
			data: resourceData,
		})
		for _, image := range state.images {
			entries = append(entries, zipEntry{
				name: resDir + "/Images/" + image.name,
				data: image.data,
			})
		}
		for _, media := range state.media {
			entries = append(entries, zipEntry{
				name: resDir + "/Media/" + media.name,
				data: media.data,
			})
		}
		profileIDs := make([]uint64, 0, len(state.colorProfiles))
		for id := range state.colorProfiles {
			profileIDs = append(profileIDs, id)
		}
		sort.Slice(profileIDs, func(i, j int) bool { return profileIDs[i] < profileIDs[j] })
		profileNames := make(map[string]bool, len(profileIDs))
		for _, id := range profileIDs {
			profile := state.colorProfiles[id]
			if profileNames[profile.name] {
				continue
			}
			profileNames[profile.name] = true
			entries = append(entries, zipEntry{name: resDir + "/" + profile.name, data: profile.data})
		}
		for _, font := range state.fonts {
			if len(font.data) == 0 {
				continue
			}
			entries = append(entries, zipEntry{
				name: resDir + "/Fonts/" + font.fileName,
				data: font.data,
			})
		}
	}
	if len(state.signatures) > 0 {
		if err := populateSignatureDigests(state, entries); err != nil {
			return nil, err
		}
		signaturesData, signaturesErr := signaturesXML(state)
		if signaturesErr != nil {
			return nil, signaturesErr
		}
		entries = append(entries, zipEntry{name: docDir + "/Signatures.xml", data: signaturesData})
		for _, signature := range state.signatures {
			signatureData, signatureErr := signatureXML(signature, state.pageIDs)
			if signatureErr != nil {
				return nil, signatureErr
			}
			entries = append(entries, zipEntry{name: signaturePath(signature.baseName), data: signatureData})
			if len(signature.value.SealFile) > 0 {
				entries = append(entries, zipEntry{name: signatureDataPath(signature.sealName), data: signature.value.SealFile})
			}
			if len(signature.value.SignedValue) > 0 {
				entries = append(entries, zipEntry{name: signatureDataPath(signature.valueName), data: signature.value.SignedValue})
			}
		}
	}
	seenEntryNames := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.name == "" {
			return nil, errors.New("OFD 包条目路径不能为空")
		}
		if strings.HasPrefix(entry.name, "/") || strings.ContainsAny(entry.name, "\\\x00") {
			return nil, fmt.Errorf("OFD 包条目路径无效: %s", entry.name)
		}
		cleanName := path.Clean(entry.name)
		if cleanName != entry.name || cleanName == "." || strings.HasPrefix(cleanName, "../") || cleanName == ".." {
			return nil, fmt.Errorf("OFD 包条目路径无效: %s", entry.name)
		}
		if seenEntryNames[entry.name] {
			return nil, fmt.Errorf("OFD 包条目路径重复: %s", entry.name)
		}
		seenEntryNames[entry.name] = true
	}
	for _, signature := range state.signatures {
		for index, reference := range signature.value.References {
			target := path.Clean(path.Join(docDir+"/Signatures", reference.FileRef))
			if !seenEntryNames[target] {
				return nil, fmt.Errorf("签名 %q 的引用 %d 目标文件不存在: %s", signature.value.ID, index+1, reference.FileRef)
			}
		}
	}
	for _, version := range state.versions {
		for index, file := range version.value.Files {
			target := path.Clean(path.Join(docDir, file.Path))
			if !seenEntryNames[target] {
				return nil, fmt.Errorf("文档版本 %q 的文件 %d 目标不存在: %s", version.value.ID, index+1, file.Path)
			}
		}
	}
	return &packageState{entries: entries}, nil
}

func populateSignatureDigests(state *buildState, entries []zipEntry) error {
	entryData := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		entryData[entry.name] = entry.data
	}
	for signatureIndex := range state.signatures {
		signature := &state.signatures[signatureIndex]
		for referenceIndex := range signature.value.References {
			reference := &signature.value.References[referenceIndex]
			target := path.Clean(path.Join(docDir+"/Signatures", reference.FileRef))
			data, ok := entryData[target]
			if !ok {
				return fmt.Errorf("签名 %q 的引用 %d 无法自动计算摘要，目标文件不存在或属于签名文件: %s", signature.value.ID, referenceIndex+1, reference.FileRef)
			}
			digest, err := signatureDigest(signature.value.CheckMethod, data)
			if err != nil {
				return fmt.Errorf("签名 %q 的引用 %d 摘要计算失败: %w", signature.value.ID, referenceIndex+1, err)
			}
			if len(reference.CheckValue) > 0 {
				if !bytes.Equal(reference.CheckValue, digest) {
					return fmt.Errorf("签名 %q 的引用 %d CheckValue 与目标文件摘要不匹配: %s", signature.value.ID, referenceIndex+1, reference.FileRef)
				}
				continue
			}
			reference.CheckValue = digest
		}
	}
	return nil
}

func attachmentsXML(state *buildState) ([]byte, error) {
	doc := newXMLDocument()
	root := doc.CreateElement("Attachments")
	root.CreateAttr("xmlns", ofNamespace)
	for _, attachment := range state.attachments {
		element := root.CreateElement("Attachment")
		element.CreateAttr("ID", attachment.value.ID)
		element.CreateAttr("Name", attachment.value.Name)
		if attachment.value.Format != "" {
			element.CreateAttr("Format", attachment.value.Format)
		}
		if !attachment.value.CreationDate.IsZero() {
			element.CreateAttr("CreationDate", attachment.value.CreationDate.Format(time.RFC3339))
		}
		if !attachment.value.ModDate.IsZero() {
			element.CreateAttr("ModDate", attachment.value.ModDate.Format(time.RFC3339))
		}
		element.CreateAttr("Size", number(float64(len(attachment.value.Data))))
		if attachment.value.Visible != nil {
			element.CreateAttr("Visible", strconv.FormatBool(*attachment.value.Visible))
		}
		if attachment.value.Usage != "" {
			element.CreateAttr("Usage", attachment.value.Usage)
		}
		element.CreateElement("FileLoc").SetText("Files/" + attachment.name)
	}
	return documentBytes(doc)
}

func customTagsXML(state *buildState) ([]byte, error) {
	doc := newXMLDocument()
	root := doc.CreateElement("CustomTags")
	root.CreateAttr("xmlns", ofNamespace)
	for _, tag := range state.customTags {
		element := root.CreateElement("CustomTag")
		element.CreateAttr("NameSpace", tag.value.NameSpace)
		if tag.schemaName != "" {
			element.CreateElement("SchemaLoc").SetText("Schemas/" + tag.schemaName)
		}
		element.CreateElement("FileLoc").SetText("Data/" + tag.dataName)
	}
	return documentBytes(doc)
}

func extensionsXML(state *buildState) ([]byte, error) {
	doc := newXMLDocument()
	root := doc.CreateElement("Extensions")
	root.CreateAttr("xmlns", ofNamespace)
	for _, extension := range state.extensions {
		element := root.CreateElement("Extension")
		element.CreateAttr("AppName", extension.value.AppName)
		element.CreateAttr("RefId", strconv.FormatUint(extension.value.RefID, 10))
		if extension.value.Company != "" {
			element.CreateAttr("Company", extension.value.Company)
		}
		if extension.value.AppVersion != "" {
			element.CreateAttr("AppVersion", extension.value.AppVersion)
		}
		if !extension.value.Date.IsZero() {
			element.CreateAttr("Date", extension.value.Date.Format(time.RFC3339))
		}
		for _, property := range extension.value.Properties {
			item := element.CreateElement("Property")
			item.CreateAttr("Name", property.Name)
			if property.Type != "" {
				item.CreateAttr("Type", property.Type)
			}
			item.SetText(property.Value)
		}
		if extension.value.Data != "" {
			element.CreateElement("Data").SetText(extension.value.Data)
		} else if len(extension.value.DataXML) > 0 {
			dataElement := element.CreateElement("Data")
			if err := appendRawXML(dataElement, extension.value.DataXML); err != nil {
				return nil, fmt.Errorf("扩展 %q 的 DataXML 无效: %w", extension.value.AppName, err)
			}
		}
		if extension.dataName != "" {
			element.CreateElement("ExtendData").SetText("Data/" + extension.dataName)
		}
	}
	return documentBytes(doc)
}

func signaturesXML(state *buildState) ([]byte, error) {
	doc := newXMLDocument()
	root := doc.CreateElement("Signatures")
	root.CreateAttr("xmlns", ofNamespace)
	if len(state.signatures) > 0 {
		maxSignID := "MaxSignId-" + strconv.Itoa(len(state.signatures))
		used := make(map[string]bool, len(state.signatures))
		for _, signature := range state.signatures {
			used[signature.value.ID] = true
		}
		for used[maxSignID] {
			maxSignID += "-1"
		}
		root.CreateElement("MaxSignId").SetText(maxSignID)
	}
	for _, signature := range state.signatures {
		element := root.CreateElement("Signature")
		element.CreateAttr("ID", signature.value.ID)
		signatureType := signature.value.Type
		if signatureType == "" {
			signatureType = "Seal"
		}
		element.CreateAttr("Type", signatureType)
		element.CreateAttr("BaseLoc", "Signatures/"+signature.baseName)
	}
	return documentBytes(doc)
}

func signatureXML(resource signatureResource, pageIDs []uint64) ([]byte, error) {
	doc := newXMLDocument()
	root := doc.CreateElement("Signature")
	root.CreateAttr("xmlns", ofNamespace)
	info := root.CreateElement("SignedInfo")
	provider := info.CreateElement("Provider")
	provider.CreateAttr("ProviderName", resource.value.ProviderName)
	if resource.value.ProviderVersion != "" {
		provider.CreateAttr("Version", resource.value.ProviderVersion)
	}
	if resource.value.Company != "" {
		provider.CreateAttr("Company", resource.value.Company)
	}
	if resource.value.Method != "" {
		info.CreateElement("SignatureMethod").SetText(resource.value.Method)
	}
	if !resource.value.Date.IsZero() {
		info.CreateElement("SignatureDateTime").SetText(resource.value.Date.Format(time.RFC3339))
	}
	references := info.CreateElement("References")
	method := resource.value.CheckMethod
	if method == "" {
		method = "MD5"
	}
	references.CreateAttr("CheckMethod", method)
	for _, reference := range resource.value.References {
		element := references.CreateElement("Reference")
		element.CreateAttr("FileRef", reference.FileRef)
		element.CreateElement("CheckValue").SetText(base64.StdEncoding.EncodeToString(reference.CheckValue))
	}
	for _, stamp := range resource.value.StampAnnots {
		element := info.CreateElement("StampAnnot")
		element.CreateAttr("ID", stamp.ID)
		element.CreateAttr("PageRef", strconv.FormatUint(pageIDs[stamp.Page], 10))
		element.CreateAttr("Boundary", boxString(stamp.Boundary.X, stamp.Boundary.Y, stamp.Boundary.Width, stamp.Boundary.Height))
		if stamp.Clip != nil {
			element.CreateAttr("Clip", boxString(stamp.Clip.X, stamp.Clip.Y, stamp.Clip.Width, stamp.Clip.Height))
		}
	}
	if len(resource.value.SealFile) > 0 {
		seal := info.CreateElement("Seal")
		seal.CreateElement("BaseLoc").SetText("Data/" + resource.sealName)
	}
	signedValueName := resource.valueName
	if len(resource.value.SignedValue) > 0 {
		signedValueName = "Data/" + signedValueName
	} else if len(resource.value.SealFile) > 0 {
		signedValueName = "Data/" + resource.sealName
	}
	root.CreateElement("SignedValue").SetText(signedValueName)
	return documentBytes(doc)
}

func versionsXML(state *buildState) ([]byte, error) {
	doc := newXMLDocument()
	root := doc.CreateElement("Versions")
	root.CreateAttr("xmlns", ofNamespace)
	for _, version := range state.versions {
		element := root.CreateElement("Version")
		element.CreateAttr("ID", version.value.ID)
		element.CreateAttr("Index", strconv.Itoa(version.value.Index))
		if version.value.Current {
			element.CreateAttr("Current", "true")
		}
		element.CreateAttr("BaseLoc", "Versions/"+version.baseName)
	}
	return documentBytes(doc)
}

func versionXML(resource versionResource) ([]byte, error) {
	doc := newXMLDocument()
	root := doc.CreateElement("DocVersion")
	root.CreateAttr("xmlns", ofNamespace)
	root.CreateAttr("ID", resource.value.ID)
	if resource.value.Version != "" {
		root.CreateAttr("Version", resource.value.Version)
	}
	if resource.value.Name != "" {
		root.CreateAttr("Name", resource.value.Name)
	}
	if !resource.value.CreationDate.IsZero() {
		root.CreateAttr("CreationDate", resource.value.CreationDate.Format("2006-01-02"))
	}
	files := root.CreateElement("FileList")
	for _, file := range resource.value.Files {
		element := files.CreateElement("File")
		element.CreateAttr("ID", file.ID)
		element.SetText(file.Path)
	}
	rootPath := resource.rootName
	if rootPath == "" {
		rootPath = "../Document.xml"
	} else {
		rootPath = "Files/" + rootPath
	}
	root.CreateElement("DocRoot").SetText(rootPath)
	return documentBytes(doc)
}

func prepare(document Document) (*buildState, error) {
	if err := subsetEmbeddedFonts(&document); err != nil {
		return nil, err
	}
	if strings.TrimSpace(document.ID) == "" {
		return nil, errors.New("文档 ID 不能为空")
	}
	if err := validateXMLDate(document.CreationDate, "文档 CreationDate"); err != nil {
		return nil, err
	}
	if err := validateXMLDate(document.ModDate, "文档 ModDate"); err != nil {
		return nil, err
	}
	if len(document.Pages) == 0 {
		return nil, errors.New("文档至少需要一个页面")
	}
	pageSize := document.PageSize
	if pageSize.Width == 0 && pageSize.Height == 0 {
		pageSize = A4
	}
	if !validSize(pageSize.Width, pageSize.Height) {
		return nil, fmt.Errorf("页面尺寸无效: %.6g x %.6g", pageSize.Width, pageSize.Height)
	}
	if document.Area != nil {
		if err := validatePageArea(document.Area); err != nil {
			return nil, fmt.Errorf("文档页面区域无效: %w", err)
		}
	}
	if document.DefaultCS > maxOFDID {
		return nil, fmt.Errorf("默认颜色空间 ID 超出 OFD 范围: %d", document.DefaultCS)
	}
	if len(document.ColorSpaces) > 0 {
		document.ColorSpaces = append([]ColorSpace(nil), document.ColorSpaces...)
	}

	state := &buildState{
		document:            document,
		pageSize:            pageSize,
		drawParamIDs:        make(map[string]uint64),
		drawParamIndexes:    make(map[string]int),
		fontIDs:             make(map[string]uint64),
		compositeIDs:        make(map[uint64]int),
		nextID:              1,
		mediaIDs:            make(map[uint64]bool),
		mediaTypes:          make(map[uint64]string),
		attachmentIDs:       make(map[string]bool),
		bookmarkNames:       make(map[string]bool),
		colorProfiles:       make(map[uint64]colorProfileResource),
		colorSpaces:         make(map[uint64]colorSpaceInfo),
		pageImageIDs:        make(map[uint64]bool),
		pendingPageImageIDs: make(map[uint64]bool),
		patternSet:          make(map[*Pattern]bool),
		usedIDs:             make(map[uint64]bool),
		reservedIDs:         make(map[uint64]string),
		rawDrawRelations:    make(map[uint64]uint64),
	}
	if err := state.reserveExplicitIDs(document); err != nil {
		return nil, err
	}
	prepareSucceeded := false
	defer func() {
		if !prepareSucceeded {
			state.clearPatternCaches()
		}
	}()
	if err := state.collectPageImageIDs(document.Pages); err != nil {
		return nil, err
	}
	profileNames := make(map[string][]byte)
	if err := state.prepareAttachments(document.Attachments); err != nil {
		return nil, err
	}
	if err := state.prepareMetadata(); err != nil {
		return nil, err
	}
	if err := state.preparePublicResources(document.PublicRes); err != nil {
		return nil, err
	}
	if err := state.prepareCustomTags(document.CustomTags); err != nil {
		return nil, err
	}
	if err := state.prepareExtensions(document.Extensions); err != nil {
		return nil, err
	}
	if err := state.prepareSignatures(document.Signatures, len(document.Pages)); err != nil {
		return nil, err
	}
	if err := state.prepareVersions(document.Versions); err != nil {
		return nil, err
	}
	for index, bookmark := range document.Bookmarks {
		name := strings.TrimSpace(bookmark.Name)
		if name == "" {
			return nil, fmt.Errorf("文档书签 %d 名称不能为空", index+1)
		}
		if state.bookmarkNames[name] {
			return nil, fmt.Errorf("文档书签名称重复: %s", name)
		}
		state.bookmarkNames[name] = true
		if strings.TrimSpace(bookmark.Goto.Bookmark) != "" {
			return nil, fmt.Errorf("文档书签 %q 的目标必须是页面 Dest，不能使用 Bookmark", name)
		}
		if err := validateGoto(&bookmark.Goto, len(document.Pages), fmt.Sprintf("文档书签 %d", index+1)); err != nil {
			return nil, err
		}
	}
	seenMediaNames := make(map[string]bool, len(document.Media))
	for index, media := range document.Media {
		if err := validateMedia(media); err != nil {
			return nil, fmt.Errorf("多媒体资源 %d 无效: %w", index+1, err)
		}
		if state.mediaIDs[media.ID] {
			return nil, fmt.Errorf("多媒体资源 ID 重复: %d", media.ID)
		}
		if media.ID == ^uint64(0) {
			return nil, fmt.Errorf("多媒体资源 ID 超出可分配范围: %d", media.ID)
		}
		if media.ID > maxOFDID {
			return nil, fmt.Errorf("多媒体资源 ID 超出 OFD 范围: %d", media.ID)
		}
		if state.reservedIDs[media.ID] != "media" || state.usedIDs[media.ID] {
			return nil, fmt.Errorf("多媒体资源 ID %d 与已有资源 ID 冲突", media.ID)
		}
		delete(state.reservedIDs, media.ID)
		state.mediaIDs[media.ID] = true
		state.usedIDs[media.ID] = true
		state.mediaTypes[media.ID] = media.Type
		if media.ID >= state.nextID {
			state.nextID = media.ID + 1
		}
		format := mediaFormat(media)
		digest := sha256.Sum256(media.Data)
		name := strings.TrimSpace(media.Name)
		if name == "" {
			name = hex.EncodeToString(digest[:]) + "." + format
		}
		if seenMediaNames[name] {
			return nil, fmt.Errorf("多媒体文件名重复: %s", name)
		}
		seenMediaNames[name] = true
		state.media = append(state.media, mediaResource{id: media.ID, name: name, type_: media.Type, format: format, data: append([]byte(nil), media.Data...)})
	}
	if err := validateActions(document.Actions, len(document.Pages), state.mediaIDs, state.mediaTypes, state.attachmentIDs, state.bookmarkNames); err != nil {
		return nil, fmt.Errorf("文档动作无效: %w", err)
	}
	if err := validateOutlines(document.Outlines, len(document.Pages), state.mediaIDs, state.mediaTypes, state.attachmentIDs, state.bookmarkNames); err != nil {
		return nil, fmt.Errorf("文档大纲无效: %w", err)
	}
	if err := validatePermissions(document.Permissions); err != nil {
		return nil, fmt.Errorf("文档权限无效: %w", err)
	}
	if err := validatePreferences(document.Preferences); err != nil {
		return nil, fmt.Errorf("文档视图首选项无效: %w", err)
	}
	state.pageIDs = make([]uint64, len(document.Pages))
	state.layers = make([][]builtLayer, len(document.Pages))
	state.pageResources = make([][]pageResource, len(document.Pages))
	state.templateIDs = make([]uint64, len(document.Templates))
	state.templateLayers = make([][]builtLayer, len(document.Templates))
	for paramIndex, param := range document.DrawParams {
		if err := validateDrawParam(param); err != nil {
			return nil, fmt.Errorf("绘制参数 %d 无效: %w", paramIndex+1, err)
		}
		name := strings.TrimSpace(param.Name)
		if _, exists := state.drawParamIDs[name]; exists {
			return nil, fmt.Errorf("绘制参数 %q 重复", name)
		}
		state.addDrawParam(param)
	}
	if err := state.resolveDrawParamRelations(); err != nil {
		return nil, err
	}
	for fontIndex, font := range document.Fonts {
		if err := validateFont(font); err != nil {
			return nil, fmt.Errorf("字体资源 %d 无效: %w", fontIndex+1, err)
		}
		name := strings.TrimSpace(font.Name)
		if _, exists := state.fontIDs[name]; exists {
			return nil, fmt.Errorf("字体资源 %q 重复", name)
		}
		state.addFont(font)
	}
	for index, space := range document.ColorSpaces {
		if err := validateColorSpace(space); err != nil {
			return nil, fmt.Errorf("颜色空间资源 %d 无效: %w", index+1, err)
		}
		if index > 0 {
			for previous := 0; previous < index; previous++ {
				if document.ColorSpaces[previous].ID == space.ID {
					return nil, fmt.Errorf("颜色空间资源 ID 重复: %d", space.ID)
				}
			}
		}
		if space.ID > maxOFDID {
			return nil, fmt.Errorf("颜色空间 ID 超出 OFD 范围: %d", space.ID)
		}
		if state.reservedIDs[space.ID] != "color-space" || state.usedIDs[space.ID] {
			return nil, fmt.Errorf("颜色空间资源 ID %d 与已有资源 ID 冲突", space.ID)
		}
		delete(state.reservedIDs, space.ID)
		state.colorSpaces[space.ID] = colorSpaceInfo{
			channels:    colorSpaceChannels(space.Type),
			bits:        space.BitsPerComponent,
			paletteSize: len(space.Palette),
		}
		state.usedIDs[space.ID] = true
		if len(space.ProfileData) > 0 {
			profileName := strings.TrimSpace(space.Profile)
			if profileName == "" {
				digest := sha256.Sum256(space.ProfileData)
				profileName = "Profiles/" + hex.EncodeToString(digest[:]) + ".icc"
			}
			if strings.ContainsAny(profileName, "\\") || strings.HasPrefix(profileName, "/") || strings.Contains(profileName, "../") {
				return nil, fmt.Errorf("颜色空间 Profile 路径无效: %q", profileName)
			}
			if previous, exists := profileNames[profileName]; exists && !bytes.Equal(previous, space.ProfileData) {
				return nil, fmt.Errorf("颜色空间 Profile 文件名对应的数据不一致: %s", profileName)
			}
			if _, exists := profileNames[profileName]; !exists {
				profileNames[profileName] = append([]byte(nil), space.ProfileData...)
			}
			state.colorProfiles[space.ID] = colorProfileResource{name: profileName, data: append([]byte(nil), space.ProfileData...)}
			s := space
			s.Profile = profileName
			state.document.ColorSpaces[index] = s
		}
		if space.ID >= state.nextID {
			state.nextID = space.ID + 1
		}
	}
	for pageIndex, page := range document.Pages {
		for resourceIndex, resource := range page.Resources {
			if len(resource.Data) == 0 {
				continue
			}
			if err := registerPageResourceResources(state, resource.Data, pageIndex, resourceIndex); err != nil {
				return nil, err
			}
		}
	}
	if document.DefaultCS != 0 {
		found := false
		for _, space := range document.ColorSpaces {
			if space.ID == document.DefaultCS {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("默认颜色空间引用了不存在的颜色空间 ID %d", document.DefaultCS)
		}
	}
	if err := state.prepareComposites(); err != nil {
		return nil, err
	}
	if err := state.validateRawResourceReferences(); err != nil {
		return nil, err
	}
	if err := validateDocumentColors(document, state); err != nil {
		return nil, err
	}
	usedTemplateIDs := make(map[uint64]bool)
	for templateIndex, template := range document.Templates {
		if err := validateTemplate(template); err != nil {
			return nil, fmt.Errorf("模板页 %d 无效: %w", templateIndex+1, err)
		}
		if template.ID == 0 {
			return nil, fmt.Errorf("模板页 %d ID 不能为空", templateIndex+1)
		}
		if template.ID == ^uint64(0) {
			return nil, fmt.Errorf("模板页 ID 超出可分配范围: %d", template.ID)
		}
		if template.ID > maxOFDID {
			return nil, fmt.Errorf("模板页 ID 超出 OFD 范围: %d", template.ID)
		}
		if usedTemplateIDs[template.ID] {
			return nil, fmt.Errorf("模板页 ID 重复: %d", template.ID)
		}
		if state.reservedIDs[template.ID] != "template" || state.usedIDs[template.ID] {
			return nil, fmt.Errorf("模板页 ID %d 与已有资源 ID 冲突", template.ID)
		}
		delete(state.reservedIDs, template.ID)
		usedTemplateIDs[template.ID] = true
		state.templateIDs[templateIndex] = template.ID
		state.usedIDs[template.ID] = true
		if template.ID >= state.nextID {
			state.nextID = template.ID + 1
		}
		usedTemplateIDs[state.templateIDs[templateIndex]] = true
		if err := state.prepareLayers(template.Items, template.Layers, &state.templateLayers[templateIndex], len(document.Pages), fmt.Sprintf("模板页 %d", templateIndex+1)); err != nil {
			return nil, err
		}
	}
	if err := validateTemplateReferences(document.Pages, document.Templates, state.templateIDs); err != nil {
		return nil, err
	}
	for pageIndex, page := range document.Pages {
		if err := validatePage(page); err != nil {
			return nil, fmt.Errorf("页面 %d 无效: %w", pageIndex+1, err)
		}
		state.pageIDs[pageIndex] = state.allocate()
		if err := state.preparePageResources(pageIndex, page.Resources); err != nil {
			return nil, err
		}
		if err := validateActions(page.Actions, len(document.Pages), state.mediaIDs, state.mediaTypes, state.attachmentIDs, state.bookmarkNames); err != nil {
			return nil, fmt.Errorf("页面 %d 的动作无效: %w", pageIndex+1, err)
		}
		pageLayers := page.Layers
		if len(pageLayers) == 0 {
			pageLayers = []Layer{{Type: page.LayerType, Items: page.Items}}
		}
		if err := state.prepareLayers(nil, pageLayers, &state.layers[pageIndex], len(document.Pages), fmt.Sprintf("页面 %d", pageIndex+1)); err != nil {
			return nil, err
		}
	}
	seenAnnotationPages := make(map[int]bool, len(document.Annotations))
	for annotationIndex, annotationPage := range document.Annotations {
		if err := validateAnnotationPage(annotationPage, len(document.Pages)); err != nil {
			return nil, fmt.Errorf("注解页面 %d 无效: %w", annotationIndex+1, err)
		}
		if seenAnnotationPages[annotationPage.Page] {
			return nil, fmt.Errorf("注解页面重复: %d", annotationPage.Page)
		}
		seenAnnotationPages[annotationPage.Page] = true
		resource := annotationResource{pageID: state.pageIDs[annotationPage.Page]}
		for itemIndex, annotation := range annotationPage.Items {
			if annotation.LastModDate.IsZero() {
				annotation.LastModDate = defaultAnnotationDate(document)
			}
			var layers []builtLayer
			if err := state.prepareLayers(annotation.Items, nil, &layers, len(document.Pages), fmt.Sprintf("注解页面 %d 的注解 %d", annotationIndex+1, itemIndex+1)); err != nil {
				return nil, err
			}
			var items []builtItem
			for _, layer := range layers {
				items = append(items, layer.items...)
			}
			resource.items = append(resource.items, builtAnnotation{value: annotation, items: items})
		}
		state.annotationPages = append(state.annotationPages, resource)
	}
	for _, pattern := range state.patterns {
		if pattern.Thumbnail != 0 && !state.pageImageIDs[pattern.Thumbnail] && !state.pendingPageImageIDs[pattern.Thumbnail] && !state.documentImageID(pattern.Thumbnail) && state.mediaTypes[pattern.Thumbnail] != "Image" {
			return nil, fmt.Errorf("图案缩略图引用了不存在的图片资源 ID %d", pattern.Thumbnail)
		}
	}
	if len(state.pendingPageImageIDs) > 0 {
		return nil, errors.New("页面图片资源未完成准备")
	}
	for index, extension := range state.extensions {
		if !state.usedIDs[extension.value.RefID] {
			return nil, fmt.Errorf("扩展 %d RefID 引用了不存在的对象 ID %d", index+1, extension.value.RefID)
		}
	}
	if state.allocationError || state.nextID == 0 || state.nextID > maxOFDID+1 {
		return nil, errors.New("资源 ID 分配溢出")
	}
	state.maxID = state.nextID - 1
	prepareSucceeded = true
	return state, nil
}

func (s *buildState) clearPatternCaches() {
	for _, pattern := range s.patterns {
		pattern.builtState = nil
		pattern.builtItems = nil
		pattern.prepared = false
		pattern.building = false
	}
	s.patterns = nil
	s.patternSet = nil
}

func (s *buildState) allocate() uint64 {
	for s.nextID > 0 && s.nextID <= maxOFDID && (s.usedIDs[s.nextID] || s.reservedIDs[s.nextID] != "") {
		s.nextID++
	}
	if s.nextID == 0 || s.nextID > maxOFDID {
		s.allocationError = true
		return 0
	}
	id := s.nextID
	s.usedIDs[id] = true
	s.nextID++
	return id
}

func (s *buildState) reserveExplicitIDs(document Document) error {
	reserve := func(id uint64, kind string) error {
		if id == 0 || id > maxOFDID || id == ^uint64(0) {
			return nil
		}
		if previous, exists := s.reservedIDs[id]; exists {
			return fmt.Errorf("显式资源 ID %d 同时用于 %s 和 %s", id, previous, kind)
		}
		s.reservedIDs[id] = kind
		return nil
	}
	for _, media := range document.Media {
		if err := reserve(media.ID, "media"); err != nil {
			return err
		}
	}
	for _, space := range document.ColorSpaces {
		if err := reserve(space.ID, "color-space"); err != nil {
			return err
		}
	}
	for _, composite := range document.Composites {
		if err := reserve(composite.ID, "composite"); err != nil {
			return err
		}
	}
	for _, template := range document.Templates {
		if err := reserve(template.ID, "template"); err != nil {
			return err
		}
	}
	for _, page := range document.Pages {
		for _, resource := range page.Resources {
			for _, image := range resource.Images {
				if err := reserve(image.ID, "page-image"); err != nil {
					return err
				}
			}
			if err := reserveRawResourceIDs(reserve, resource.Data); err != nil {
				return err
			}
		}
	}
	for _, resource := range document.PublicRes {
		if err := reserveRawResourceIDs(reserve, resource.Data); err != nil {
			return err
		}
	}
	return nil
}

func reserveRawResourceIDs(reserve func(uint64, string) error, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil || doc.Root() == nil {
		return nil
	}
	for _, pattern := range []string{
		"ColorSpaces/ColorSpace",
		"DrawParams/DrawParam",
		"Fonts/Font",
		"MultiMedias/MultiMedia",
		"CompositeGraphicUnits/CompositeGraphicUnit",
	} {
		for _, element := range doc.Root().FindElements(pattern) {
			id, err := strconv.ParseUint(strings.TrimSpace(element.SelectAttrValue("ID", "")), 10, 64)
			if err != nil || id == 0 || id > maxOFDID {
				continue
			}
			if err := reserve(id, "raw-resource"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *buildState) prepareAttachments(values []Attachment) error {
	seen := make(map[string]bool, len(values))
	seenNames := make(map[string]bool, len(values))
	for index, value := range values {
		if err := validateAttachment(value); err != nil {
			return fmt.Errorf("附件 %d 无效: %w", index+1, err)
		}
		if seen[value.ID] {
			return fmt.Errorf("附件 ID 重复: %s", value.ID)
		}
		seen[value.ID] = true
		s.attachmentIDs[value.ID] = true
		name := strings.TrimSpace(value.FileName)
		if name == "" {
			digest := sha256.Sum256(value.Data)
			name = hex.EncodeToString(digest[:])
			if value.Format != "" {
				name += "." + attachmentFormat(value.Format)
			}
		}
		if seenNames[name] {
			return fmt.Errorf("附件文件名重复: %s", name)
		}
		seenNames[name] = true
		value.Format = attachmentFormat(value.Format)
		s.attachments = append(s.attachments, attachmentResource{value: value, name: name})
	}
	return nil
}

func (s *buildState) prepareMetadata() error {
	value := s.document
	for index, keyword := range value.Keywords {
		if strings.TrimSpace(keyword) == "" {
			return fmt.Errorf("关键词 %d 不能为空", index+1)
		}
	}
	seenCustomData := make(map[string]bool, len(value.CustomDatas))
	for index, customData := range value.CustomDatas {
		if strings.TrimSpace(customData.Name) == "" {
			return fmt.Errorf("自定义元数据 %d 名称不能为空", index+1)
		}
		if seenCustomData[customData.Name] {
			return fmt.Errorf("自定义元数据名称重复: %s", customData.Name)
		}
		seenCustomData[customData.Name] = true
	}
	if strings.TrimSpace(value.Cover) == "" && len(value.CoverData) == 0 {
		return nil
	}
	if len(value.CoverData) == 0 {
		return errors.New("Cover 必须同时提供 CoverData")
	}
	name := strings.TrimSpace(value.CoverName)
	if name == "" {
		digest := sha256.Sum256(value.CoverData)
		format := imageFormat(Image{Data: value.CoverData})
		if format == "" {
			format = "bin"
		}
		name = hex.EncodeToString(digest[:]) + "." + strings.ToLower(format)
	}
	if err := validateLeafFileName(name); err != nil {
		return fmt.Errorf("Cover 文件名无效: %w", err)
	}
	s.coverName = name
	s.coverData = append([]byte(nil), value.CoverData...)
	s.document.Cover = docDir + "/Cover/" + name
	return nil
}

func (s *buildState) preparePublicResources(values []PublicResource) error {
	seen := make(map[string]bool, len(values))
	for index, value := range values {
		name := filepath.ToSlash(strings.TrimSpace(value.Name))
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
			return fmt.Errorf("公共资源 %d 文件名无效", index+1)
		}
		clean := path.Clean(name)
		if clean != name || clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
			return fmt.Errorf("公共资源 %d 路径无效: %q", index+1, value.Name)
		}
		if seen[name] {
			return fmt.Errorf("公共资源文件名重复: %s", name)
		}
		if len(value.Data) == 0 {
			return fmt.Errorf("公共资源 %d 数据不能为空", index+1)
		}
		if err := validateOFDSchema(value.Data, "Res"); err != nil {
			return fmt.Errorf("公共资源 %s 不符合 Res.xsd: %w", name, err)
		}
		if err := registerPageResourceResources(s, value.Data, -1, index); err != nil {
			return fmt.Errorf("公共资源 %s 资源索引无效: %w", name, err)
		}
		files, err := preparePublicResourceFiles(value.Files, name)
		if err != nil {
			return err
		}
		if err := validateResExternalFiles(value.Data, name, publicResourceFilePaths(files)); err != nil {
			return fmt.Errorf("公共资源 %s 的外部文件引用无效: %w", name, err)
		}
		doc := etree.NewDocument()
		if err := doc.ReadFromBytes(value.Data); err != nil {
			return fmt.Errorf("公共资源 %s XML 无效: %w", name, err)
		}
		if doc.Root() == nil || doc.Root().Tag != "Res" || doc.Root().NamespaceURI() != ofNamespace {
			return fmt.Errorf("公共资源 %s 根元素必须是 OFD 命名空间中的 Res", name)
		}
		seen[name] = true
		s.publicResources = append(s.publicResources, publicResource{name: name, data: append([]byte(nil), value.Data...), files: files})
	}
	return nil
}

func preparePublicResourceFiles(values []PublicResourceFile, resourceName string) ([]publicResourceFile, error) {
	seen := make(map[string]bool, len(values))
	files := make([]publicResourceFile, 0, len(values))
	for index, value := range values {
		name, err := prepareResourceFilePath(value.Path, resourceName)
		if err != nil {
			return nil, fmt.Errorf("公共资源 %s 的文件 %d 路径无效", resourceName, index+1)
		}
		if len(value.Data) == 0 {
			return nil, fmt.Errorf("公共资源 %s 的文件 %d 数据不能为空", resourceName, index+1)
		}
		if seen[name] {
			return nil, fmt.Errorf("公共资源 %s 的文件路径重复: %s", resourceName, name)
		}
		seen[name] = true
		files = append(files, publicResourceFile{path: name, data: append([]byte(nil), value.Data...)})
	}
	return files, nil
}

func publicResourceFilePaths(values []publicResourceFile) []string {
	paths := make([]string, len(values))
	for index, value := range values {
		paths[index] = value.path
	}
	return paths
}

func pageResourceFilePaths(values []pageResourceFile) []string {
	paths := make([]string, len(values))
	for index, value := range values {
		paths[index] = value.path
	}
	return paths
}

func prepareResourceFilePath(value, resourceName string) (string, error) {
	name := filepath.ToSlash(strings.TrimSpace(value))
	if name == "" || name != value || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\\x00") {
		return "", errors.New("路径必须是规范化的包内相对路径")
	}
	clean := path.Clean(name)
	if clean != name || clean == "." || clean == ".." {
		return "", errors.New("路径必须是规范化的包内相对路径")
	}
	resolved := path.Clean(path.Join(path.Dir(resourceName), clean))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return "", errors.New("路径不能超出文档目录")
	}
	return clean, nil
}

func (s *buildState) prepareCustomTags(values []CustomTag) error {
	seen := make(map[string]bool, len(values))
	seenDataNames := make(map[string]bool, len(values))
	seenSchemaNames := make(map[string]bool, len(values))
	for index, value := range values {
		if err := validateCustomTag(value); err != nil {
			return fmt.Errorf("自定义标签 %d 无效: %w", index+1, err)
		}
		if seen[value.NameSpace] {
			return fmt.Errorf("自定义标签命名空间重复: %s", value.NameSpace)
		}
		seen[value.NameSpace] = true
		digest := sha256.Sum256(value.Data)
		dataName := strings.TrimSpace(value.DataName)
		if dataName == "" {
			dataName = hex.EncodeToString(digest[:]) + ".xml"
		}
		schemaName := strings.TrimSpace(value.SchemaName)
		if len(value.Schema) > 0 && schemaName == "" {
			schemaDigest := sha256.Sum256(value.Schema)
			schemaName = hex.EncodeToString(schemaDigest[:]) + ".xsd"
		}
		if seenDataNames[dataName] {
			return fmt.Errorf("自定义标签数据文件名重复: %s", dataName)
		}
		seenDataNames[dataName] = true
		if schemaName != "" {
			if seenSchemaNames[schemaName] {
				return fmt.Errorf("自定义标签 Schema 文件名重复: %s", schemaName)
			}
			seenSchemaNames[schemaName] = true
		}
		s.customTags = append(s.customTags, customTagResource{value: value, schemaName: schemaName, dataName: dataName})
	}
	return nil
}

func (s *buildState) prepareExtensions(values []Extension) error {
	seenDataNames := make(map[string]bool, len(values))
	for index, value := range values {
		if err := validateExtension(value); err != nil {
			return fmt.Errorf("扩展 %d 无效: %w", index+1, err)
		}
		if value.RefID > maxOFDID {
			return fmt.Errorf("扩展 %d RefID 超出 OFD 范围: %d", index+1, value.RefID)
		}
		name := strings.TrimSpace(value.DataName)
		if len(value.DataFile) > 0 && name == "" {
			digest := sha256.Sum256(value.DataFile)
			name = hex.EncodeToString(digest[:]) + ".bin"
		}
		if name != "" {
			if seenDataNames[name] {
				return fmt.Errorf("扩展数据文件名重复: %s", name)
			}
			seenDataNames[name] = true
		}
		s.extensions = append(s.extensions, extensionResource{value: value, dataName: name})
	}
	return nil
}

func (s *buildState) prepareSignatures(values []Signature, pageCount int) error {
	seen := make(map[string]bool, len(values))
	seenDataNames := make(map[string]bool, len(values))
	for index, value := range values {
		if value.Type == "" {
			value.Type = "Seal"
		}
		if value.CheckMethod == "" {
			value.CheckMethod = "MD5"
		}
		value.CheckMethod = strings.ToUpper(strings.TrimSpace(value.CheckMethod))
		if err := validateSignature(value, pageCount); err != nil {
			return fmt.Errorf("签名 %d 无效: %w", index+1, err)
		}
		if seen[value.ID] {
			return fmt.Errorf("签名 ID 重复: %s", value.ID)
		}
		seen[value.ID] = true
		baseName := "Signature_" + value.ID + ".xml"
		valueName := strings.TrimSpace(value.SignedValueName)
		if valueName == "" {
			valueName = value.ID + ".bin"
		}
		sealName := strings.TrimSpace(value.SealName)
		if len(value.SealFile) > 0 && sealName == "" {
			sealName = value.ID + ".seal"
		}
		dataName := ""
		if len(value.SignedValue) > 0 {
			dataName = valueName
		} else if len(value.SealFile) > 0 {
			dataName = sealName
		}
		if dataName != "" {
			if seenDataNames[dataName] {
				return fmt.Errorf("签名数据文件名重复: %s", dataName)
			}
			seenDataNames[dataName] = true
		}
		if len(value.References) > 0 {
			value.References = append([]SignatureReference(nil), value.References...)
		}
		s.signatures = append(s.signatures, signatureResource{value: value, baseName: baseName, sealName: sealName, valueName: valueName})
	}
	return nil
}

func (s *buildState) prepareVersions(values []DocumentVersion) error {
	seen := make(map[string]bool, len(values))
	seenIndexes := make(map[int]bool, len(values))
	seenRootNames := make(map[string]bool, len(values))
	currentCount := 0
	for index, value := range values {
		if err := validateDocumentVersion(value); err != nil {
			return fmt.Errorf("文档版本 %d 无效: %w", index+1, err)
		}
		if seen[value.ID] {
			return fmt.Errorf("文档版本 ID 重复: %s", value.ID)
		}
		if seenIndexes[value.Index] {
			return fmt.Errorf("文档版本 Index 重复: %d", value.Index)
		}
		seen[value.ID] = true
		seenIndexes[value.Index] = true
		if value.Current {
			currentCount++
		}
		baseName := "DocVersion_" + value.ID + ".xml"
		rootName := strings.TrimSpace(value.DocRootName)
		if len(value.DocRoot) > 0 && rootName == "" {
			rootName = value.ID + "-Document.xml"
		}
		if rootName != "" {
			if seenRootNames[rootName] {
				return fmt.Errorf("文档版本根文件名重复: %s", rootName)
			}
			seenRootNames[rootName] = true
		}
		s.versions = append(s.versions, versionResource{value: value, baseName: baseName, rootName: rootName})
	}
	if currentCount > 1 {
		return errors.New("文档版本最多只能有一个 Current 版本")
	}
	return nil
}

func (s *buildState) preparePageResources(pageIndex int, resources []PageResource) error {
	for resourceIndex, resource := range resources {
		if len(resource.Data) > 0 && len(resource.Images) > 0 {
			return fmt.Errorf("页面 %d 资源文件 %d 不能同时设置 Data 和 Images", pageIndex+1, resourceIndex+1)
		}
		if len(resource.Data) > 0 {
			if err := validateOFDSchema(resource.Data, "Res"); err != nil {
				return fmt.Errorf("页面 %d 资源文件 %d 不符合 Res.xsd: %w", pageIndex+1, resourceIndex+1, err)
			}
			doc := etree.NewDocument()
			if err := doc.ReadFromBytes(resource.Data); err != nil {
				return fmt.Errorf("页面 %d 资源文件 %d XML 无效: %w", pageIndex+1, resourceIndex+1, err)
			}
			if doc.Root() == nil || doc.Root().Tag != "Res" || doc.Root().NamespaceURI() != ofNamespace {
				return fmt.Errorf("页面 %d 资源文件 %d 根元素必须是 OFD 命名空间中的 Res", pageIndex+1, resourceIndex+1)
			}
			files, err := preparePageResourceFiles(resource.Files, pageIndex, resourceIndex)
			if err != nil {
				return err
			}
			resourceName := fmt.Sprintf("Pages/Page_%d/PageRes_%d_%d.xml", pageIndex, pageIndex, resourceIndex)
			if err := validateResExternalFiles(resource.Data, resourceName, pageResourceFilePaths(files)); err != nil {
				return fmt.Errorf("页面 %d 资源文件 %d 的外部文件引用无效: %w", pageIndex+1, resourceIndex+1, err)
			}
			s.pageResources[pageIndex] = append(s.pageResources[pageIndex], pageResource{
				name:  fmt.Sprintf("PageRes_%d_%d.xml", pageIndex, resourceIndex),
				data:  append([]byte(nil), resource.Data...),
				files: files,
			})
			continue
		}
		if len(resource.Images) == 0 {
			return fmt.Errorf("页面 %d 资源文件 %d 不能为空", pageIndex+1, resourceIndex+1)
		}
		built := pageResource{name: fmt.Sprintf("PageRes_%d_%d.xml", pageIndex, resourceIndex)}
		seen := make(map[uint64]bool)
		seenNames := make(map[string]bool, len(resource.Images))
		for imageIndex, image := range resource.Images {
			if err := validatePageImage(image); err != nil {
				return fmt.Errorf("页面 %d 资源文件 %d 图片 %d 无效: %w", pageIndex+1, resourceIndex+1, imageIndex+1, err)
			}
			if seen[image.ID] || s.pageImageIDs[image.ID] {
				return fmt.Errorf("页面资源图片 ID 重复: %d", image.ID)
			}
			if s.pendingPageImageIDs[image.ID] {
				delete(s.pendingPageImageIDs, image.ID)
			}
			if image.ID == ^uint64(0) {
				return fmt.Errorf("页面资源图片 ID 超出可分配范围: %d", image.ID)
			}
			if image.ID > maxOFDID {
				return fmt.Errorf("页面资源图片 ID 超出 OFD 范围: %d", image.ID)
			}
			if s.reservedIDs[image.ID] != "page-image" || s.usedIDs[image.ID] {
				return fmt.Errorf("页面资源图片 ID %d 与已有资源 ID 冲突", image.ID)
			}
			delete(s.reservedIDs, image.ID)
			seen[image.ID] = true
			s.pageImageIDs[image.ID] = true
			s.usedIDs[image.ID] = true
			if image.ID >= s.nextID {
				s.nextID = image.ID + 1
			}
			format := pageImageFormat(image)
			digest := sha256.Sum256(image.Data)
			name := strings.TrimSpace(image.Name)
			if name == "" {
				name = hex.EncodeToString(digest[:]) + "." + format
			}
			if seenNames[name] {
				return fmt.Errorf("页面 %d 资源文件 %d 图片文件名重复: %s", pageIndex+1, resourceIndex+1, name)
			}
			seenNames[name] = true
			built.images = append(built.images, pageImageResource{id: image.ID, name: name, format: format, data: append([]byte(nil), image.Data...)})
		}
		s.pageResources[pageIndex] = append(s.pageResources[pageIndex], built)
	}
	return nil
}

func (s *buildState) collectPageImageIDs(pages []Page) error {
	for pageIndex, page := range pages {
		for resourceIndex, resource := range page.Resources {
			for imageIndex, image := range resource.Images {
				if image.ID == 0 || image.ID > maxOFDID || image.ID == ^uint64(0) {
					return fmt.Errorf("页面 %d 资源文件 %d 图片 %d ID 无效: %d", pageIndex+1, resourceIndex+1, imageIndex+1, image.ID)
				}
				if s.pendingPageImageIDs[image.ID] || s.pageImageIDs[image.ID] || pageResourceIDUsed(s, image.ID) {
					return fmt.Errorf("页面资源图片 ID 重复: %d", image.ID)
				}
				if s.reservedIDs[image.ID] != "page-image" {
					return fmt.Errorf("页面资源图片 ID 重复: %d", image.ID)
				}
				s.pendingPageImageIDs[image.ID] = true
			}
		}
	}
	return nil
}

func registerPageResourceResources(s *buildState, data []byte, pageIndex, resourceIndex int) error {
	if err := validateOFDSchema(data, "Res"); err != nil {
		return fmt.Errorf("页面 %d 资源文件 %d 不符合 Res.xsd: %w", pageIndex+1, resourceIndex+1, err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		return fmt.Errorf("页面 %d 资源文件 %d XML 无效: %w", pageIndex+1, resourceIndex+1, err)
	}
	root := doc.Root()
	seenIDs := make(map[uint64]bool)
	register := func(element *etree.Element, kind string, callback func(uint64, *etree.Element) error) error {
		id, err := strconv.ParseUint(strings.TrimSpace(element.SelectAttrValue("ID", "")), 10, 64)
		if err != nil || id == 0 || id > maxOFDID {
			return fmt.Errorf("页面 %d 资源文件 %d 的 %s ID 无效", pageIndex+1, resourceIndex+1, kind)
		}
		if seenIDs[id] || pageResourceIDUsed(s, id) {
			return fmt.Errorf("页面资源 %s ID %d 与已有资源 ID 冲突", kind, id)
		}
		if owner := s.reservedIDs[id]; owner != "" && owner != "raw-resource" {
			return fmt.Errorf("页面资源 %s ID %d 与已有资源 ID 冲突", kind, id)
		}
		seenIDs[id] = true
		delete(s.reservedIDs, id)
		s.usedIDs[id] = true
		if err := callback(id, element); err != nil {
			return err
		}
		s.collectRawResourceReferences(element, kind, pageIndex, resourceIndex)
		return nil
	}
	for _, element := range root.FindElements("ColorSpaces/ColorSpace") {
		if err := register(element, "ColorSpace", func(id uint64, element *etree.Element) error {
			if _, exists := s.colorSpaces[id]; exists {
				return fmt.Errorf("页面资源 ColorSpace ID 重复: %d", id)
			}
			typeName := element.SelectAttrValue("Type", "")
			if typeName != "GRAY" && typeName != "RGB" && typeName != "CMYK" {
				return fmt.Errorf("页面资源 ColorSpace 类型无效: %q", typeName)
			}
			bits := 8
			if raw := element.SelectAttrValue("BitsPerComponent", ""); raw != "" {
				parsed, parseErr := strconv.Atoi(raw)
				if parseErr != nil {
					return fmt.Errorf("页面资源 ColorSpace BitsPerComponent 无效: %q", raw)
				}
				if parsed < 0 || parsed > 32 {
					return fmt.Errorf("页面资源 ColorSpace BitsPerComponent 无效: %q", raw)
				}
				bits = parsed
			}
			paletteSize := len(element.FindElements("Palette/CV"))
			s.colorSpaces[id] = colorSpaceInfo{channels: colorSpaceChannels(typeName), bits: bits, paletteSize: paletteSize}
			return nil
		}); err != nil {
			return err
		}
	}
	for _, element := range root.FindElements("DrawParams/DrawParam") {
		if err := register(element, "DrawParam", func(id uint64, element *etree.Element) error {
			name := strings.TrimSpace(element.SelectAttrValue("Name", ""))
			if name == "" {
				name = fmt.Sprintf("#%d", id)
			}
			if _, exists := s.drawParamIDs[name]; exists {
				return fmt.Errorf("页面资源 DrawParam 名称重复: %s", name)
			}
			s.drawParamIDs[name] = id
			s.drawParamIDs[strconv.FormatUint(id, 10)] = id
			s.drawParamIDs["#"+strconv.FormatUint(id, 10)] = id
			return nil
		}); err != nil {
			return err
		}
	}
	for _, element := range root.FindElements("Fonts/Font") {
		if err := register(element, "Font", func(id uint64, element *etree.Element) error {
			name := strings.TrimSpace(element.SelectAttrValue("FontName", ""))
			if name == "" {
				return errors.New("页面资源 FontName 不能为空")
			}
			if _, exists := s.fontIDs[name]; exists {
				return fmt.Errorf("页面资源字体名称重复: %s", name)
			}
			s.fontIDs[name] = id
			return nil
		}); err != nil {
			return err
		}
	}
	for _, element := range root.FindElements("MultiMedias/MultiMedia") {
		if err := register(element, "MultiMedia", func(id uint64, element *etree.Element) error {
			if s.mediaIDs[id] || s.pageImageIDs[id] {
				return fmt.Errorf("页面资源 MultiMedia ID 重复: %d", id)
			}
			mediaType := element.SelectAttrValue("Type", "")
			if mediaType != "Image" && mediaType != "Audio" && mediaType != "Video" {
				return fmt.Errorf("页面资源 MultiMedia 类型无效: %q", mediaType)
			}
			s.mediaIDs[id] = true
			s.mediaTypes[id] = mediaType
			if mediaType == "Image" {
				s.pageImageIDs[id] = true
			}
			return nil
		}); err != nil {
			return err
		}
	}
	for _, element := range root.FindElements("CompositeGraphicUnits/CompositeGraphicUnit") {
		if err := register(element, "CompositeGraphicUnit", func(id uint64, element *etree.Element) error {
			if _, exists := s.compositeIDs[id]; exists {
				return fmt.Errorf("页面资源 CompositeGraphicUnit ID 重复: %d", id)
			}
			s.compositeIDs[id] = -1
			return nil
		}); err != nil {
			return err
		}
	}
	for id := range seenIDs {
		if id >= s.nextID {
			s.nextID = id + 1
		}
	}
	return nil
}

func (s *buildState) collectRawResourceReferences(element *etree.Element, kind string, pageIndex, resourceIndex int) {
	source := fmt.Sprintf("页面 %d 资源文件 %d", pageIndex+1, resourceIndex+1)
	if pageIndex < 0 {
		source = fmt.Sprintf("公共资源 %d", resourceIndex+1)
	}
	var walk func(*etree.Element)
	add := func(referenceKind, raw, context string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		id, err := strconv.ParseUint(raw, 10, 64)
		if err == nil {
			s.rawReferences = append(s.rawReferences, rawResourceReference{kind: referenceKind, value: id, text: raw, source: source + context})
		}
	}
	walk = func(node *etree.Element) {
		if node.Tag == "DrawParam" {
			if relative := strings.TrimSpace(node.SelectAttrValue("Relative", "")); relative != "" {
				if childID, err := strconv.ParseUint(strings.TrimSpace(node.SelectAttrValue("ID", "")), 10, 64); err == nil {
					if parentID, err := strconv.ParseUint(relative, 10, 64); err == nil {
						s.rawDrawRelations[childID] = parentID
					}
				}
			}
		}
		for _, attribute := range []struct {
			name string
			kind string
		}{
			{name: "ColorSpace", kind: "color-space"},
			{name: "DrawParam", kind: "draw-param"},
			{name: "Relative", kind: "draw-param"},
			{name: "Font", kind: "font"},
		} {
			if value := node.SelectAttrValue(attribute.name, ""); value != "" {
				add(attribute.kind, value, " "+attribute.name)
			}
		}
		if raw := node.SelectAttrValue("ResourceID", ""); raw != "" {
			kind := "media-or-composite"
			if node.Tag == "CompositeObject" {
				kind = "composite"
			} else if node.Tag == "ImageObject" {
				kind = "image"
			}
			add(kind, raw, " ResourceID")
		}
		for _, name := range []string{"Thumbnail", "Substitution"} {
			if child := node.FindElement(name); child != nil {
				add("image", child.Text(), " "+name)
			}
		}
		for _, child := range node.ChildElements() {
			walk(child)
		}
	}
	walk(element)
}

func (s *buildState) validateRawResourceReferences() error {
	relations := make(map[uint64]uint64, len(s.rawDrawRelations)+len(s.drawParams))
	for child, parent := range s.rawDrawRelations {
		relations[child] = parent
	}
	for _, param := range s.drawParams {
		if param.relative != 0 {
			relations[param.id] = param.relative
		}
	}
	for start := range relations {
		seen := make(map[uint64]bool)
		current := start
		for current != 0 {
			if seen[current] {
				return fmt.Errorf("原始资源 DrawParam 继承关系包含循环: %d", current)
			}
			seen[current] = true
			next, ok := relations[current]
			if !ok {
				break
			}
			current = next
		}
	}
	for _, reference := range s.rawReferences {
		switch reference.kind {
		case "color-space":
			if _, ok := s.colorSpaces[reference.value]; !ok {
				return fmt.Errorf("%s ColorSpace 引用了不存在的颜色空间 ID %s", reference.source, reference.text)
			}
		case "draw-param":
			found := false
			for _, id := range s.drawParamIDs {
				if id == reference.value {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("%s Relative 引用了不存在的 DrawParam ID %s", reference.source, reference.text)
			}
		case "font":
			found := false
			for _, id := range s.fontIDs {
				if id == reference.value {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("%s Font 引用了不存在的字体 ID %s", reference.source, reference.text)
			}
		case "image":
			if !s.isImageReference(reference.value) {
				return fmt.Errorf("%s 引用了不存在的图片或复合图元 ID %s", reference.source, reference.text)
			}
		case "composite":
			if _, ok := s.compositeIDs[reference.value]; !ok {
				return fmt.Errorf("%s 引用了不存在的复合图元 ID %s", reference.source, reference.text)
			}
		case "media-or-composite":
			if s.mediaTypes[reference.value] == "" {
				if _, ok := s.compositeIDs[reference.value]; !ok {
					return fmt.Errorf("%s ResourceID 引用了不存在的媒体或复合图元 ID %s", reference.source, reference.text)
				}
			}
		}
	}
	return nil
}

func (s *buildState) isImageReference(id uint64) bool {
	if s.mediaTypes[id] == "Image" || s.pageImageIDs[id] || s.pendingPageImageIDs[id] {
		return true
	}
	_, ok := s.compositeIDs[id]
	return ok
}

func pageResourceIDUsed(s *buildState, id uint64) bool {
	if s.usedIDs[id] {
		return true
	}
	if s.mediaIDs[id] || s.pageImageIDs[id] || s.pendingPageImageIDs[id] {
		return true
	}
	if _, ok := s.compositeIDs[id]; ok {
		return true
	}
	if _, ok := s.colorSpaces[id]; ok {
		return true
	}
	for _, value := range s.drawParamIDs {
		if value == id {
			return true
		}
	}
	for _, value := range s.fontIDs {
		if value == id {
			return true
		}
	}
	return false
}

func preparePageResourceFiles(values []PageResourceFile, pageIndex, resourceIndex int) ([]pageResourceFile, error) {
	seen := make(map[string]bool, len(values))
	files := make([]pageResourceFile, 0, len(values))
	resourceName := fmt.Sprintf("Pages/Page_%d/PageRes_%d_%d.xml", pageIndex, pageIndex, resourceIndex)
	for index, value := range values {
		name, err := prepareResourceFilePath(value.Path, resourceName)
		if err != nil {
			return nil, fmt.Errorf("页面 %d 资源文件 %d 的文件 %d 路径无效", pageIndex+1, resourceIndex+1, index+1)
		}
		if len(value.Data) == 0 {
			return nil, fmt.Errorf("页面 %d 资源文件 %d 的文件 %d 数据不能为空", pageIndex+1, resourceIndex+1, index+1)
		}
		if seen[name] {
			return nil, fmt.Errorf("页面 %d 资源文件 %d 的文件路径重复: %s", pageIndex+1, resourceIndex+1, name)
		}
		seen[name] = true
		files = append(files, pageResourceFile{path: name, data: append([]byte(nil), value.Data...)})
	}
	return files, nil
}

func (s *buildState) prepareLayers(items []Item, layers []Layer, output *[]builtLayer, pageCount int, context string) error {
	if len(layers) == 0 {
		layers = []Layer{{Items: items}}
	}
	for layerIndex, layer := range layers {
		if err := validateLayerType(layer.Type); err != nil {
			return fmt.Errorf("%s 图层 %d 无效: %w", context, layerIndex+1, err)
		}
		layerType := layer.Type
		if layerType == "" {
			layerType = LayerBody
		}
		layerDrawParam, err := s.drawParamID(layer.DrawParam)
		if err != nil {
			return fmt.Errorf("%s 图层 %d 无效: %w", context, layerIndex+1, err)
		}
		layerResult := builtLayer{id: s.allocate(), layerType: layerType, drawParam: layerDrawParam}
		for itemIndex, item := range layer.Items {
			if item == nil {
				return fmt.Errorf("%s 图层 %d 的对象 %d 为空", context, layerIndex+1, itemIndex+1)
			}
			built := builtItem{id: s.allocate(), item: item}
			switch value := item.(type) {
			case Text:
				if err := validateText(value); err != nil {
					return fmt.Errorf("%s 图层 %d 的文字对象 %d 无效: %w", context, layerIndex+1, itemIndex+1, err)
				}
				if err := validateClips(value.Clips); err != nil {
					return fmt.Errorf("%s 图层 %d 的文字对象 %d 裁剪无效: %w", context, layerIndex+1, itemIndex+1, err)
				}
				if err := s.prepareClips(value.Clips); err != nil {
					return fmt.Errorf("%s 图层 %d 的文字对象 %d 裁剪资源无效: %w", context, layerIndex+1, itemIndex+1, err)
				}
				if err := validateActions(value.Actions, pageCount, s.mediaIDs, s.mediaTypes, s.attachmentIDs, s.bookmarkNames); err != nil {
					return fmt.Errorf("%s 图层 %d 的文字对象 %d 动作无效: %w", context, layerIndex+1, itemIndex+1, err)
				}
				font := strings.TrimSpace(value.Font)
				if font == "" {
					font = "SimSun"
				}
				built.font = s.fontID(font)
				built.drawParam, err = s.drawParamID(value.DrawParam)
			case Path:
				if err := validatePath(value); err != nil {
					return fmt.Errorf("%s 图层 %d 的路径对象 %d 无效: %w", context, layerIndex+1, itemIndex+1, err)
				}
				if err := validateClips(value.Clips); err != nil {
					return fmt.Errorf("%s 图层 %d 的路径对象 %d 裁剪无效: %w", context, layerIndex+1, itemIndex+1, err)
				}
				if err := s.prepareClips(value.Clips); err != nil {
					return fmt.Errorf("%s 图层 %d 的路径对象 %d 裁剪资源无效: %w", context, layerIndex+1, itemIndex+1, err)
				}
				if err := validateActions(value.Actions, pageCount, s.mediaIDs, s.mediaTypes, s.attachmentIDs, s.bookmarkNames); err != nil {
					return fmt.Errorf("%s 图层 %d 的路径对象 %d 动作无效: %w", context, layerIndex+1, itemIndex+1, err)
				}
				built.drawParam, err = s.drawParamID(value.DrawParam)
			case Image:
				if value.ResourceID != 0 {
					if !s.pageImageIDs[value.ResourceID] && !s.documentImageID(value.ResourceID) && s.mediaTypes[value.ResourceID] != "Image" {
						return fmt.Errorf("%s 图层 %d 的图片对象 %d 引用了不存在的图片资源 ID %d", context, layerIndex+1, itemIndex+1, value.ResourceID)
					}
					if len(value.Data) > 0 || value.Format != "" || value.Name != "" {
						return fmt.Errorf("%s 图层 %d 的图片对象 %d 不能同时设置 ResourceID 和图片数据或名称", context, layerIndex+1, itemIndex+1)
					}
				} else if err := validateImage(value); err != nil {
					return fmt.Errorf("%s 图层 %d 的图片对象 %d 无效: %w", context, layerIndex+1, itemIndex+1, err)
				}
				for _, reference := range []struct {
					name string
					id   uint64
				}{
					{name: "Substitution", id: value.Substitution},
					{name: "ImageMask", id: value.ImageMask},
				} {
					referenceName, referenceID := reference.name, reference.id
					if referenceID != 0 && !s.pageImageIDs[referenceID] && !s.documentImageID(referenceID) && s.mediaTypes[referenceID] != "Image" {
						return fmt.Errorf("%s 图层 %d 的图片对象 %d 引用了不存在的 %s 资源 ID %d", context, layerIndex+1, itemIndex+1, referenceName, referenceID)
					}
				}
				if err := validateClips(value.Clips); err != nil {
					return fmt.Errorf("%s 图层 %d 的图片对象 %d 裁剪无效: %w", context, layerIndex+1, itemIndex+1, err)
				}
				if err := s.prepareClips(value.Clips); err != nil {
					return fmt.Errorf("%s 图层 %d 的图片对象 %d 裁剪资源无效: %w", context, layerIndex+1, itemIndex+1, err)
				}
				if err := validateActions(value.Actions, pageCount, s.mediaIDs, s.mediaTypes, s.attachmentIDs, s.bookmarkNames); err != nil {
					return fmt.Errorf("%s 图层 %d 的图片对象 %d 动作无效: %w", context, layerIndex+1, itemIndex+1, err)
				}
				if value.ResourceID != 0 {
					built.image = value.ResourceID
				} else {
					built.image = s.imageID(value)
				}
				built.drawParam, err = s.drawParamID(value.DrawParam)
			case Composite:
				if err := validateComposite(value); err != nil {
					return fmt.Errorf("%s 图层 %d 的复合对象 %d 无效: %w", context, layerIndex+1, itemIndex+1, err)
				}
				if err := validateClips(value.Clips); err != nil {
					return fmt.Errorf("%s 图层 %d 的复合对象 %d 裁剪无效: %w", context, layerIndex+1, itemIndex+1, err)
				}
				if err := s.prepareClips(value.Clips); err != nil {
					return fmt.Errorf("%s 图层 %d 的复合对象 %d 裁剪资源无效: %w", context, layerIndex+1, itemIndex+1, err)
				}
				if err := validateActions(value.Actions, pageCount, s.mediaIDs, s.mediaTypes, s.attachmentIDs, s.bookmarkNames); err != nil {
					return fmt.Errorf("%s 图层 %d 的复合对象 %d 动作无效: %w", context, layerIndex+1, itemIndex+1, err)
				}
				if _, ok := s.compositeIDs[value.ResourceID]; !ok {
					return fmt.Errorf("%s 图层 %d 的复合对象 %d 未找到资源 ID %d", context, layerIndex+1, itemIndex+1, value.ResourceID)
				}
				built.composite = value.ResourceID
				built.drawParam, err = s.drawParamID(value.DrawParam)
			case PageBlock:
				var nestedLayers []builtLayer
				if err := s.prepareLayers(value.Items, nil, &nestedLayers, pageCount, fmt.Sprintf("%s 图层 %d 的嵌套页面块 %d", context, layerIndex+1, itemIndex+1)); err != nil {
					return err
				}
				for _, nestedLayer := range nestedLayers {
					built.pageBlock = append(built.pageBlock, nestedLayer.items...)
				}
			default:
				return fmt.Errorf("%s 图层 %d 的对象 %d 类型 %T 不受支持", context, layerIndex+1, itemIndex+1, item)
			}
			if err != nil {
				return fmt.Errorf("%s 图层 %d 的对象 %d 无效: %w", context, layerIndex+1, itemIndex+1, err)
			}
			layerResult.items = append(layerResult.items, built)
		}
		*output = append(*output, layerResult)
	}
	return nil
}

func (s *buildState) preparePattern(pattern *Pattern, pageCount int, context string) error {
	if pattern == nil {
		return nil
	}
	if !s.patternSet[pattern] {
		s.patternSet[pattern] = true
		s.patterns = append(s.patterns, pattern)
	}
	if pattern.prepared && pattern.builtState == s {
		return nil
	}
	if pattern.prepared && pattern.builtState != s {
		pattern.prepared = false
		pattern.builtItems = nil
	}
	if pattern.building {
		return fmt.Errorf("%s 包含循环引用", context)
	}
	if err := validatePattern(pattern); err != nil {
		return fmt.Errorf("%s 无效: %w", context, err)
	}
	pattern.building = true
	pattern.builtState = s
	var layers []builtLayer
	if err := s.prepareLayers(pattern.Items, pattern.Layers, &layers, pageCount, context+"图案单元"); err != nil {
		pattern.building = false
		return err
	}
	for _, layer := range layers {
		pattern.builtItems = append(pattern.builtItems, layer.items...)
	}
	pattern.building = false
	pattern.prepared = true
	return nil
}

func (s *buildState) prepareComposites() error {
	for index, composite := range s.document.Composites {
		if err := validateCompositeResource(composite); err != nil {
			return fmt.Errorf("复合图元资源 %d 无效: %w", index+1, err)
		}
		if _, exists := s.compositeIDs[composite.ID]; exists {
			return fmt.Errorf("复合图元资源 ID 重复: %d", composite.ID)
		}
		if composite.ID == ^uint64(0) {
			return fmt.Errorf("复合图元资源 ID 超出可分配范围: %d", composite.ID)
		}
		if composite.ID > maxOFDID {
			return fmt.Errorf("复合图元资源 ID 超出 OFD 范围: %d", composite.ID)
		}
		if s.reservedIDs[composite.ID] != "composite" || s.usedIDs[composite.ID] {
			return fmt.Errorf("复合图元资源 ID %d 与已有资源 ID 冲突", composite.ID)
		}
		delete(s.reservedIDs, composite.ID)
		s.compositeIDs[composite.ID] = len(s.composites)
		s.usedIDs[composite.ID] = true
		if composite.ID >= s.nextID {
			s.nextID = composite.ID + 1
		}
		for _, reference := range []struct {
			name string
			id   uint64
		}{
			{name: "Thumbnail", id: composite.Thumbnail},
			{name: "Substitution", id: composite.Substitution},
		} {
			referenceName, referenceID := reference.name, reference.id
			if referenceID != 0 && s.mediaTypes[referenceID] != "Image" {
				return fmt.Errorf("复合图元资源 %d 的 %s 引用了不存在的图片资源 ID %d", index+1, referenceName, referenceID)
			}
		}
		resource := compositeResource{id: composite.ID, width: composite.Width, height: composite.Height, thumbnail: composite.Thumbnail, substitution: composite.Substitution}
		if err := s.prepareLayers(composite.Items, nil, &resource.layers, len(s.document.Pages), fmt.Sprintf("复合图元资源 %d", index+1)); err != nil {
			return err
		}
		s.composites = append(s.composites, resource)
	}
	return nil
}

func (s *buildState) addDrawParam(param DrawParam) uint64 {
	name := strings.TrimSpace(param.Name)
	if id, ok := s.drawParamIDs[name]; ok {
		return id
	}
	id := s.allocate()
	s.drawParamIDs[name] = id
	s.drawParamIndexes[name] = len(s.drawParams)
	s.drawParams = append(s.drawParams, drawParamResource{
		id:           id,
		name:         name,
		relativeName: strings.TrimSpace(param.Relative),
		lineWidth:    param.LineWidth,
		join:         param.Join,
		cap:          param.Cap,
		dashOffset:   param.DashOffset,
		dashPattern:  append([]float64(nil), param.DashPattern...),
		miterLimit:   param.MiterLimit,
		fillColor:    param.FillColor,
		strokeColor:  param.StrokeColor,
	})
	return id
}

func (s *buildState) drawParamID(name string) (uint64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, nil
	}
	id, ok := s.drawParamIDs[name]
	if !ok {
		return 0, fmt.Errorf("未找到绘制参数 %q", name)
	}
	return id, nil
}

func (s *buildState) prepareClips(clips *Clips) error {
	if clips == nil {
		return nil
	}
	for clipIndex, clip := range clips.Items {
		for areaIndex, area := range clip.Areas {
			if area.DrawParam != "" {
				if _, err := s.drawParamID(area.DrawParam); err != nil {
					return fmt.Errorf("第 %d 个 Clip 的第 %d 个 Area: %w", clipIndex+1, areaIndex+1, err)
				}
			}
			if area.Text != nil {
				font := strings.TrimSpace(area.Text.Font)
				if font == "" {
					font = "SimSun"
				}
				s.fontID(font)
			}
		}
	}
	return nil
}

func (s *buildState) resolveDrawParamRelations() error {
	state := make([]uint8, len(s.drawParams))
	var visit func(int) error
	visit = func(index int) error {
		switch state[index] {
		case 1:
			return fmt.Errorf("绘制参数继承关系存在循环")
		case 2:
			return nil
		}
		state[index] = 1
		param := &s.drawParams[index]
		if param.relativeName != "" {
			relativeIndex, ok := s.drawParamIndexes[param.relativeName]
			if !ok {
				return fmt.Errorf("绘制参数 %q 引用了不存在的继承参数 %q", param.name, param.relativeName)
			}
			if err := visit(relativeIndex); err != nil {
				return err
			}
			param.relative = s.drawParams[relativeIndex].id
		}
		state[index] = 2
		return nil
	}
	for index := range s.drawParams {
		if err := visit(index); err != nil {
			return err
		}
	}
	return nil
}

func (s *buildState) fontID(name string) uint64 {
	if id, ok := s.fontIDs[name]; ok {
		return id
	}
	return s.addFont(Font{Name: name, Charset: "unicode"})
}

func (s *buildState) addFont(font Font) uint64 {
	name := strings.TrimSpace(font.Name)
	if id, ok := s.fontIDs[name]; ok {
		return id
	}
	id := s.allocate()
	resource := fontResource{
		id:         id,
		name:       name,
		familyName: strings.TrimSpace(font.FamilyName),
		charset:    fontCharset(font.Charset),
		italic:     font.Italic,
		bold:       font.Bold,
		serif:      font.Serif,
		fixedWidth: font.FixedWidth,
		data:       append([]byte(nil), font.Data...),
	}
	if len(resource.data) > 0 {
		digest := sha256.Sum256(resource.data)
		resource.fileName = "font-" + hex.EncodeToString(digest[:]) + "." + strings.ToLower(fontFormat(font))
	}
	s.fontIDs[name] = id
	s.fonts = append(s.fonts, resource)
	return id
}

func (s *buildState) imageID(image Image) uint64 {
	digest := sha256.Sum256(image.Data)
	key := hex.EncodeToString(digest[:])
	for _, resource := range s.images {
		if resource.name == key+"."+imageFormat(image) {
			return resource.id
		}
	}
	id := s.allocate()
	format := imageFormat(image)
	s.images = append(s.images, imageResource{
		id:     id,
		name:   key + "." + format,
		data:   append([]byte(nil), image.Data...),
		format: format,
	})
	return id
}

func (s *buildState) documentImageID(id uint64) bool {
	for _, image := range s.images {
		if image.id == id {
			return true
		}
	}
	return false
}
