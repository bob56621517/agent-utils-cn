// Package version 提供构建版本信息，可由链接器注入。
package version

// Version 是服务的构建版本，发布时通过 -ldflags -X 覆盖。
var Version = "dev"
