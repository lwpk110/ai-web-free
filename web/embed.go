package web

import (
	"embed"
	"strings"
)

// StaticFS 内嵌基于 @openuidev/react-lang 与 @openuidev/react-ui 构建的前端产物
//
//go:embed all:dist
var StaticFS embed.FS

// rawSystemPrompt 内嵌由 OpenUI 组件库生成的默认 System Prompt
//
//go:embed system-prompt.txt
var rawSystemPrompt string

// OpenUISystemPrompt 返回去除首尾空白的 OpenUI Lang 系统提示词
func OpenUISystemPrompt() string {
	return strings.TrimSpace(rawSystemPrompt)
}
