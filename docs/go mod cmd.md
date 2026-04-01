#### 代理服务器配置
```
export GOPROXY=https://goproxy.cn,direct
```

#### 1. 初始化模块
go mod init github.com/xxx/energy

#### 2. 自动分析依赖、下载缺失、清理无用
go mod tidy

#### 3. 下载所有依赖到本地
go mod download

#### 4. 查看所有依赖（含版本）
go list -m all

#### 5. 查看依赖为什么被引入
go mod why 依赖名

#### 查看所有直接/间接依赖（含版本）
go list -m all

#### 查看依赖树（层级结构）
go mod graph

#### 查看某个依赖的所有可用版本（Git 标签/分支）
go list -m -versions github.com/energye/energy

#### 升级某个依赖到最新版本
go get github.com/xxx/energy@latest

#### 升级某个依赖到指定标签
go get github.com/xxx/energy@v1.0.5

#### 升级到指定分支
go get github.com/xxx/energy@main

#### 升级到指定 commit
go get github.com/xxx/energy@a1b2c3d

#### 升级所有依赖到最新次要版本（不破坏兼容）
go get -u

#### 升级所有依赖到最新版（含不兼容）
go get -u=patch


#### 本地替换依赖（开发调试用）
```
go mod edit -replace=原库=本地路径
go mod edit -replace=github.com/xxx/energy=./energy
```

#### 取消 replace
go mod edit -dropreplace=github.com/xxx/energy

#### 格式化 go.mod
go mod edit -fmt

#### 强制重新整理依赖
go mod tidy -e

#### 查看依赖缓存路径
go env GOMODCACHE

#### 清理所有缓存（慎用）
go clean -modcache

#### 查看模块缓存位置
```
go mod download -json 模块名

go mod download -json github.com/energye/lcl@latest
go mod download -json github.com/energye/designer@latest
go mod download -json github.com/energye/cef@v1.0.1
go mod download -json github.com/energye/wv@v1.0.1
go mod download -json github.com/energye/energy/v3@latest

```

#### 创建一个临时的环境查询
GO111MODULE=on go list -m -json github.com/energye/lcl@latest

#### 查看当前模块版本
go list -m

#### 查看某个依赖当前使用的版本
```
go list -m github.com/energye/energy

go list -m github.com/energye/cef
go list -m github.com/energye/lcl
go list -m github.com/energye/wv
```

#### 检查依赖是否有新版本
```
go list -m -u github.com/energye/energy

go list -m -u github.com/energye/cef
go list -m -u github.com/energye/lcl
go list -m -u github.com/energye/wv
```

#### 1. 更新某个框架到指定版本
```
go get github.com/xxx/energy@v1.0.5
go mod tidy

```

#### 2. 查看所有依赖版本
go list -m all

#### 3. 查看依赖最新可用版
go list -m -u all
