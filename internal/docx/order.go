// Package docx 提供生成 OOXML WordprocessingML（.docx）所需的最小子集。
//
// 本包只负责写出，不解析 docx。结构体的字段按 ECMA-376 的 schema sequence
// 顺序声明，配合 encoding/xml 按字段顺序输出的行为，使生成顺序自动合法；
// order.go 的顺序常量表与 order_test.go 的反射断言共同保证这一点。
package docx

// OOXML 的复杂类型都是 xsd:sequence，子元素顺序由 schema 规定。顺序写错时
// LibreOffice 依然能正常打开并渲染（实测：w:pPr 把 w:jc 放到 w:spacing 之前、
// 甚至整份 document.xml 去掉命名空间，LibreOffice 都不报错），但 Word 与 WPS
// 会判定文档损坏并弹出「是否修复」。本地也没有 OOXML XSD 可供校验，
// LibreOffice 因此不能作为验收工具，只能证明 ZIP 结构完整。
//
// 结论：顺序不能依赖代码作者记住，必须由测试锁住。下面的常量表给出用到的每个
// 复杂类型的合法元素顺序，ooxml.go 里对应结构体按同一顺序声明字段，
// order_test.go 断言两者逐项一致。
var (
	// paragraphPropertiesOrder 是 CT_PPr 的元素顺序。
	paragraphPropertiesOrder = []string{
		"w:pStyle", "w:keepNext", "w:keepLines", "w:pageBreakBefore", "w:framePr",
		"w:widowControl", "w:numPr", "w:suppressLineNumbers", "w:pBdr", "w:shd",
		"w:tabs", "w:suppressAutoHyphens", "w:kinsoku", "w:wordWrap",
		"w:overflowPunct", "w:topLinePunct", "w:autoSpaceDE", "w:autoSpaceDN",
		"w:bidi", "w:adjustRightInd", "w:snapToGrid", "w:spacing", "w:ind",
		"w:contextualSpacing", "w:mirrorIndents", "w:suppressOverlap", "w:jc",
		"w:textDirection", "w:textAlignment", "w:textboxTightWrap", "w:outlineLvl",
		"w:divId", "w:cnfStyle", "w:rPr", "w:sectPr", "w:pPrChange",
	}

	// runPropertiesOrder 是 CT_RPr 的元素顺序。
	runPropertiesOrder = []string{
		"w:rStyle", "w:rFonts", "w:b", "w:bCs", "w:i", "w:iCs", "w:caps",
		"w:smallCaps", "w:strike", "w:dstrike", "w:outline", "w:shadow",
		"w:emboss", "w:imprint", "w:noProof", "w:snapToGrid", "w:vanish",
		"w:webHidden", "w:color", "w:spacing", "w:w", "w:kern", "w:position",
		"w:sz", "w:szCs", "w:highlight", "w:u", "w:effect", "w:bdr", "w:shd",
		"w:fitText", "w:vertAlign", "w:rtl", "w:cs", "w:em", "w:lang",
		"w:eastAsianLayout", "w:specVanish", "w:oMath", "w:rPrChange",
	}

	// numberingPropertiesOrder 是 CT_NumPr 的元素顺序。
	numberingPropertiesOrder = []string{
		"w:ilvl", "w:numId", "w:numberingChange", "w:ins",
	}

	// tableCellPropertiesOrder 是 CT_TcPr 的元素顺序。
	tableCellPropertiesOrder = []string{
		"w:cnfStyle", "w:tcW", "w:gridSpan", "w:hMerge", "w:vMerge",
		"w:tcBorders", "w:shd", "w:noWrap", "w:tcMar", "w:textDirection",
		"w:tcFitText", "w:vAlign", "w:hideMark", "w:tcPrChange",
	}

	// tablePropertiesOrder 是 CT_TblPr 的**子元素**顺序。注意 w:tblStyle 是
	// w:tblPr 的属性而不是子元素，所以不出现在这里——否则测试会把正确的
	// 属性声明误判成错误。
	tablePropertiesOrder = []string{
		"w:tblpPr", "w:tblOverlap", "w:bidiVisual",
		"w:tblStyleRowBandSize", "w:tblStyleColBandSize", "w:tblW", "w:jc",
		"w:tblCellSpacing", "w:tblInd", "w:tblBorders", "w:shd", "w:tblLayout",
		"w:tblCellMar", "w:tblLook", "w:tblCaption", "w:tblDescription",
		"w:tblPrChange",
	}

	// tableBordersOrder 是 CT_TblBorders 的元素顺序。
	tableBordersOrder = []string{
		"w:top", "w:start", "w:left", "w:bottom", "w:end", "w:right",
		"w:insideH", "w:insideV",
	}

	// sectionPropertiesOrder 是 CT_SectPr 的元素顺序。页眉页脚引用属于
	// EG_HdrFtrReferences 组，必须排在 EG_SectPrContents 之前。
	sectionPropertiesOrder = []string{
		"w:headerReference", "w:footerReference", "w:footnotePr", "w:endnotePr",
		"w:type", "w:pgSz", "w:pgMar", "w:paperSrc", "w:pgBorders", "w:lnNumType",
		"w:pgNumType", "w:cols", "w:formProt", "w:vAlign", "w:noEndnote",
		"w:titlePg", "w:textDirection", "w:bidi", "w:rtlGutter", "w:docGrid",
		"w:printerSettings", "w:sectPrChange",
	}

	// styleOrder 是 CT_Style 的元素顺序。
	styleOrder = []string{
		"w:name", "w:aliases", "w:basedOn", "w:next", "w:link", "w:autoRedefine",
		"w:hidden", "w:uiPriority", "w:semiHidden", "w:unhideWhenUsed",
		"w:qFormat", "w:locked", "w:personal", "w:personalCompose",
		"w:personalReply", "w:rsid", "w:pPr", "w:rPr", "w:tblPr", "w:trPr",
		"w:tcPr", "w:tblStylePr",
	}

	// numberingLevelOrder 是 CT_Lvl 的元素顺序。
	numberingLevelOrder = []string{
		"w:start", "w:numFmt", "w:lvlRestart", "w:pStyle", "w:isLgl", "w:suff",
		"w:lvlText", "w:lvlPicBulletId", "w:legacy", "w:lvlJc", "w:pPr", "w:rPr",
	}

	// abstractNumberingOrder 是 CT_AbstractNum 的元素顺序。
	abstractNumberingOrder = []string{
		"w:nsid", "w:multiLevelType", "w:tmpl", "w:name", "w:styleLink",
		"w:numStyleLink", "w:lvl",
	}

	// numberingOrder 是 CT_Numbering 的元素顺序。
	numberingOrder = []string{"w:numPicBullet", "w:abstractNum", "w:num"}

	// 下面的常量是内容模型（container）而非复杂类型的顺序，用来约束
	// 「父元素下依次出现哪些子元素」。
	//
	// paragraphOrder 是 CT_P 的子元素顺序：属性块在前，正文内容在后。
	paragraphOrder = []string{"w:pPr", "w:r", "w:hyperlink", "w:bookmarkStart", "w:bookmarkEnd"}

	// runOrder 是 CT_R 的子元素顺序（EG_RunInnerContent 的常用子集，
	// 顺序按规范从 br/t 到 drawing）。w:drawing 排在最后，因此一个 run 里
	// 文字与图片不可能同时出现，两个字段必须是指针。
	runOrder = []string{
		"w:rPr", "w:br", "w:t", "w:noBreakHyphen", "w:tab", "w:sym", "w:object",
		"w:drawing", "w:lastRenderedPageBreak",
	}

	// tableOrder 是 CT_Tbl 的子元素顺序。
	tableOrder = []string{"w:tblPr", "w:tblGrid", "w:tr"}

	// tableRowOrder 是 CT_Row 的子元素顺序。
	tableRowOrder = []string{"w:tblPrEx", "w:trPr", "w:tc"}

	// tableCellOrder 是 CT_Tc 的子元素顺序。
	tableCellOrder = []string{"w:tcPr", "w:p", "w:tbl"}

	// stylesOrder 是 CT_Styles 的子元素顺序。
	stylesOrder = []string{"w:docDefaults", "w:latentStyles", "w:style"}

	// inlineOrder 是 wp:inline（CT_Inline）的子元素顺序。
	inlineOrder = []string{
		"wp:extent", "wp:effectExtent", "wp:docPr", "wp:cNvGraphicFramePr",
		"a:graphic",
	}

	// pictureOrder 是 pic:pic 的子元素顺序。
	pictureOrder = []string{"pic:nvPicPr", "pic:blipFill", "pic:spPr"}

	// pictureNonVisualOrder 是 pic:nvPicPr 的子元素顺序。
	pictureNonVisualOrder = []string{"pic:cNvPr", "pic:cNvPicPr"}

	// pictureBlipFillOrder 是 pic:blipFill 的子元素顺序。
	pictureBlipFillOrder = []string{"a:blip", "a:srcRect", "a:tile", "a:stretch"}

	// shapePropertiesOrder 是 pic:spPr 的子元素顺序。
	shapePropertiesOrder = []string{"a:xfrm", "a:prstGeom", "a:noFill", "a:solidFill", "a:ln"}

	// transformOrder 是 a:xfrm 的子元素顺序。
	transformOrder = []string{"a:off", "a:ext"}

	// presetShapeOrder 是 a:prstGeom 的子元素顺序。
	presetShapeOrder = []string{"a:avLst"}

	// graphicDataOrder 是 a:graphicData 的子元素顺序。
	graphicDataOrder = []string{"pic:pic"}

	// tableRowPropertiesOrder 是 CT_TrPr 的元素顺序。
	tableRowPropertiesOrder = []string{
		"w:cnfStyle", "w:divId", "w:gridBefore", "w:gridAfter", "w:wBefore",
		"w:wAfter", "w:cantSplit", "w:trHeight", "w:tblHeader", "w:tblCellSpacing",
		"w:jc", "w:hidden",
	}
)
