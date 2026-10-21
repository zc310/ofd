# OFD XML 读写

`ofdxml` 面向 OFD 包内的 XML，支持顺序生成，也支持读取已有 XML、修改节点树并写回。

## 读取后修改

```go
doc, err := ofdxml.Parse(data)
if err != nil {
	return err
}

// 节点树使用 etree API。按命名空间 URI 区分 OFD 元素和同名扩展元素。
for element := range doc.Root().FindElementsSeq("./Fonts/Font") {
	if element.Tag == "Font" && element.NamespaceURI() == ofdxml.Namespace {
		element.CreateAttr("FontName", "新字体名称")
	}
}

output, err := ofdxml.Marshal(doc)
if err != nil {
	return err
}
// output 可独立长期持有。
```

`Parse` 返回 `*etree.Document`，可使用 `CreateAttr`、`SetText`、`InsertChild`、
`RemoveChild`、`Copy` 等方法编辑。跨文档复制或移动子树时，需要补齐原先从祖先
继承的命名空间声明；节点的顺序及 OFD 资源引用由调用方维护。

`Marshal` 保留原有名称和命名空间声明，保留节点顺序、文本空白、注释和处理指令，
不缩进或修改输入树。CDATA 读取为普通文本，写回统一使用文本转义；即使外部
etree 树带有 CDATA 标记，也按普通文本写出。XML 声明统一为 UTF-8；实体的写法、
引号等形式可能变化。文本中的回车以字符引用写出，以保证重新读取后的文字相同。
编辑后出现多个根元素、根外文本、非法名称或命名空间、非法注释/处理指令、
非法 XML 字符时返回错误。`WriteElement` 同样报告这些错误及底层流式写入错误。
顺序生成用的 `Writer.Text`/`Attr` 继续按其约定替换非法字符；树写回拒绝非法
字符，避免编辑内容被静默替换。

读取结果不借用输入切片，写回结果不借用树或池中的缓冲区。节点树的并发编辑需要
调用方同步。

## 性能路径

UTF-8 XML 1.0 使用已有的 `tdewolff/parse/v2/xml` lexer 直接构建 etree 树，
不调用 etree 的解析器，也没有读取回退。同一文档内复用元素和属性名称，名称
只校验一次。XML 声明的单双引号、空白、`standalone` 以及 CRLF、制表符和属性
换行均使用同一条读取路径。

属性值从原始输入读取，避免 lexer 原地替换属性空白影响结果。按 XML 1.0
§2.11、§3.3.3 归一化：字面 CR/CRLF 转换为 LF，属性中的字面制表符和换行转换
为空格，CRLF 只产生一个空格；字符引用产生的 CR、LF 和制表符按原值保留。
注释和处理指令中的字面换行同样归一化。注意 `encoding/xml` 本身没有完整实现
这些规则，对照测试会补齐这部分归一化，并使用独立 XML 解析器复核。

读取范围是 OFD XML 所需的元素、属性、文本、预定义实体和数字字符引用，并保留
注释和处理指令。CDATA 内容作为文本读取，其中的 `&amp;` 等内容保持字面值，
不作为实体再次解码。DOCTYPE、自定义实体及非 UTF-8 编码返回错误。
节点树只负责 XML 读取和编辑，不进行 OFD 模型或 XSD 校验；GB/T 33190—2016
§7.1 的 OFD 命名空间及各类节点结构由上层模型、校验器和业务操作维护。

写回复用 `Writer` 的缓冲区、转义和数值输出能力，直接写前缀和本地名，避免拼接
临时名称。标准 `ofd` 前缀的 `Marshal` 路径递归复用声明；扩展前缀及归一化写回
按源树解析命名空间。大型输出直接移交超出池容量上限的缓冲区。

已有 `MarshalDocument` 和 `WriteElement` 用于将 OFD 元素转换为 `ofd:` 前缀的
写入场景，归一化时按元素作用域补齐声明。对于 `ofd` 已绑定外部 URI 的非规范
输入，保留该绑定及当前 OFD 元素的原名称，避免改变扩展元素、属性或 QName 值
的含义；子作用域重新正确声明 `ofd` 后可继续归一化。读取后编辑优先使用
`Parse` / `Marshal`。

## 验证

```sh
go test ./internal/ofdxml -count=1
go test ./internal/ofdxml -run '^$' -bench 'BenchmarkXML' -benchmem
go test ./internal/ofdxml -run '^$' -fuzz FuzzParseMatchesEtree -fuzztime=10s
```

`BenchmarkXMLEdit` 比较包含中文、实体和属性的页面 XML 在读取、修改一个对象 ID、
写回整个文档时的耗时和分配，同时覆盖 CRLF 格式化、属性换行及不同声明写法；
`BenchmarkXMLMarshal` 单独比较树写回成本。两者使用内存数据，不包含 ZIP 解压
或文件读写。
