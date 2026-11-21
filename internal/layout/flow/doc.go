// Package flow 将顺序内容排版为 OFD 固定版式页面。
//
// 本包负责字体度量、UAX #14 断行、分栏分页、表格和导航重排；
// 对外创作 API 位于 pkg/creator/flow，格式导入器统一经该公开入口使用排版。
package flow
