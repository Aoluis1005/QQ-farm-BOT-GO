package main

// 构建时由构建脚本(服务器 build 步骤)注入当前 git commit 短哈希与构建时间，
// 用于三端(源码仓库 / 线上运行服务 / 公开分发二进制)版本互证。
// 默认值 "dev" 仅用于本地未注入场景；正式构建会被覆盖，二进制内字符串不被混淆。
var buildVersion = "dev"
var buildTime = "dev"
