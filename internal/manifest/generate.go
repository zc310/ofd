package manifest

// manifest.go 的字段说明只存在于文档注释里，构建产物读不到源码，因此 schema
// 子命令依赖 gen-fielddocs 生成的 FieldDocs。改动 manifest 的字段、类型或注释后
// 必须重新执行 go generate ./internal/manifest，否则生成的说明会与 schema 不符。
//
//go:generate go run ./fielddocs/cmd/gen-fielddocs
