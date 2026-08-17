# ACE/Tsdk 纯 Go 复刻笔记（协议层核心难点，已攻克）

## 背景
腾讯农场网关 `wss://gate-obt.nqf.qq.com/prod/ws` 要求所有请求 body 用 ACE 安全模块（TSDK WASM）加密，
登录请求的 auth_token 用 `getEncryptedInitInfo()` 凭据，否则返回 `1020001 网络繁忙`（明文被拒）。

## 关键实现（Go）
- 用 `github.com/tetratelabs/wazero` 加载官方 `tsdk-v3.8.2.wasm`（SHA256 `705e326...`），纯 Go、无 CGO。
- 项目导入依赖需设 `GOPATH`+`GOMODCACHE` 且 `GOPROXY=https://goproxy.cn,direct`（proxy.golang.org 不可达）。
- module: `github.com/Aoluis1005/go-farm-bot/ace`，文件：`constants.go` + `ace.go`。

## 参数（对齐 Node utils/tsdk-runtime.js）
- gameId=3167, appKey="0", appId=DEFAULT_APP_ID="1112386029"
- 导出符号映射：initRuntime=G, getEncryptedInitInfo=H, encryptData=ba, decryptData=ca, generateToken=aa, memory=w, createBuffer=A, destroyBuffer=B, sendHeartbeatTick=M
- 需解混淆 17 段 MERGED_DATA_SEGMENTS（key=1871261153）+ call_ctors + initRuntime

## host imports（模块 "a"，22 个函数）精确签名（经 WASM import 解析确认）
- **i32=0x7F(127), i64=0x7E(126), f64=0x7C(124)**（注意没有 125=f32 参与）
- a:(i32,i32,i32,i32)->void assert(panic) / b:(i32,i32,i32)->i32 filewrite / c:(i32,i32)->i32 stack
- d:(i32,i32)->i32 version / e:(i32)->i32 jsint(return0) / f:()->void sensors
- g:(i32,i32,i32,i32)->i32 fileread / h:(i32,i32,i32,i32)->i32 clock
- i:(i32,i32)->i32 userpath / j:(i32,i32)->i32 device / k:(i32,i32)->i32 table / l:()->i32(=2)
- m:(i32,i32)->i32 appid / n:(i32,i32)->i32 defaultappid / o:(i32,i32,i32,i32)->void funcinteg
- p:(i32)->i32 stat / q:(i32)->i32 servertime / r:(i32)->i32 grow(panic) / s:()->f64 now
- t:(i32,i32,i32)->i32 fileappend / u:()->void abort(panic) / v:(i32,i32)->i32 tqos(return0)

## 踩坑（必读）
1. **device(j) 必须与 Node 完全一致**：`"Linux x64;linux;<os.release>;Node.js;"`。
   os.release = 完整内核版本，读 `/proc/sys/kernel/osrelease`。若给 "Linux" 则 initInfo 变短(100 vs 144字节)。
   初始 host 值导致 initInfo 字节差异，进而加密密钥不同。
2. **mem.Read 返回 wasm 底层共享切片**！`defer free()`（destroyBuffer）之后读取该切片会被释放/复用成垃圾值。
   必须 `append([]byte(nil), raw...)` 拷贝后再返回（Encrypt/Decrypt）。
   → 曾经的 `b44c0000b44c0000` 假"加密错误"就是 free 后读共享视图所致。
3. **wazero Memory 方法不收 ctx**：`Read(off,size) ([]byte,bool)`、`Write(off,[]byte)`、`WriteUint64Le(off,v)`。
   `ExportedFunction()` 单值返回（无 ok，判 nil）。
   `InstantiateModuleFromBinary` 已改名 → `CompileModule` + `InstantiateModule`。
4. clock host：clockID==0 → epoch 纳秒(UnixNano)；else → 自进程启动纳秒(Sime(start).Nanoseconds)。
   node 的 performance.now() 是小值。

## 验证结果（全对）
- Go enc == Node enc（字节级一致）
- Go.Decrypt(Go.enc)==明文，Go.Decrypt(Node.enc)==明文（互通）

## 集成（gw/client.go）
- Connect：`ace.New("gw",3167,"0")` → `Init` → `EncryptedInitInfo` 存 firstToken
- Request：body 先 `ace.Encrypt`；auth_token 首次=firstToken（登录），之后=随机 gatewayToken
- readLoop：响应 body 用 `ace.Decrypt` 后再解析
