需求：
Go项目模块依赖自动更新和推送
1. 根据父模块依赖版本更新版本号，同时更新当前库版本号，以及下由库版本
2. 库存在多层级依赖
3. 使用Go语言实现

模块：lcl, cef, wv, energy
依赖关系，从左到右
energy > cef, wv > lcl
根据模块依赖关系写出和设计go的实现代码
例如 lcl 是基础库， cef wv, energy 都分别依赖它
cef 和 wv 依赖lcl
energy 依赖 cef , wv, lcl

场景 lcl 版本号变更,需要给 cef wv energy 三个库更新
wv 或 cef 其中某个版本号变更 ，需要给 energy 库更新 

这个更新流程可以是个通知 或以什么逻辑方试被动触发，被通知 和通知库都是以 tag 标签来控制的，
