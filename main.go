// ==============================================================================
// 📚 应用启动入口文件
// 📌 该文件在创建项目时创建
// ✏️ 可在此文件中添加业务逻辑
// ==============================================================================

package main

import (
	"embed"
	"energy-release/app"
	_ "energy-release/resources"
	"github.com/energye/energy/v3/application"
	"github.com/energye/energy/v3/wv"
)

//go:embed resources
var resources embed.FS

func main() {
	app.ConfigureBackendLogger()
	// 全局初始化
	wvApp := wv.Init(nil, nil)
	wvApp.SetOptions(application.Options{
		Frameless:          true,
		DisableResize:      true,
		DisableContextMenu: true,
		Caption:            "Go Release Manager",
		DefaultURL:         "fs://energy/index.html",
	})
	wvApp.SetLocalLoad(application.LocalLoad{
		Scheme:     "fs",
		Domain:     "energy",
		ResRootDir: "resources",
		FS:         resources,
	})
	// 启动应用程序消息循环
	wv.Run(app.Forms...)
}
