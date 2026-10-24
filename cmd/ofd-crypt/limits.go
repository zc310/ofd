package main

import (
	"github.com/spf13/pflag"
	"github.com/zc310/ofd/pkg/crypt"
)

func registerLimitFlags(flags *pflag.FlagSet, limits *crypt.Limits) {
	flags.Int64Var(&limits.MaxInputBytes, "max-input-bytes", 0, "输入 ZIP 大小上限；0 使用库默认值")
	flags.Int64Var(&limits.MaxEntryBytes, "max-entry-bytes", 0, "单条解压/输出大小上限；0 使用默认值")
	flags.Int64Var(&limits.MaxTotalBytes, "max-total-bytes", 0, "输入解压及输出总大小分别限制；0 使用默认值")
	flags.IntVar(&limits.MaxEntries, "max-entries", 0, "ZIP 条目数量上限；0 使用默认值")
	flags.Int64Var(&limits.MaxMetadataBytes, "max-metadata-bytes", 0, "单份加密元数据大小上限；0 使用默认值")
	flags.IntVar(&limits.MaxCredentialAttempts, "max-credential-attempts", 0, "所有 seed 累计凭据尝试上限；0 使用默认 32")
	flags.IntVar(&limits.MaxRecipients, "max-recipients", 0, "所有 seed 累计接收者上限；0 使用默认 1024")
	flags.Int64Var(&limits.MaxProcessedBytes, "max-processed-bytes", 0, "视图累计处理字节上限；0 使用默认值")
	flags.IntVar(&limits.MaxLayers, "max-layers", 0, "加密依赖深度上限；0 使用默认值")
}
