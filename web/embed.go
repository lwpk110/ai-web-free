package web

import "embed"

// StaticFS 内嵌 OpenUI 前端控制台静态资源
//
//go:embed index.html styles/* js/*
var StaticFS embed.FS
