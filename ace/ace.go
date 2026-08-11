// Package ace 复刻 Node TSDK 安全运行时，用 wazero 跑官方 WASM 实现 ACE 加密。
package ace

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

//go:embed tsdk-v3.8.2.wasm
var wasmBin []byte

// 进程启动时间，用于模拟 performance.now()
var startTS = time.Now()

// Runtime TSDK 运行时封装
type Runtime struct {
	ctx    context.Context
	rt     wazero.Runtime
	mod    api.Module
	mem    api.Memory
	ready  bool
	userID string
	appID  string
	gameID int64
	appKey string
	dir    string
}

// New 创建运行时
func New(accountID string, gameID int64, appKey string) *Runtime {
	return &Runtime{
		userID: accountID,
		appID:  DefaultAppID,
		gameID: gameID,
		appKey: appKey,
		dir:    filepath.Join(DefaultDataDir, accountID),
	}
}

// Init 初始化：构建 host、加载 WASM、解混淆、initRuntime
func (r *Runtime) Init(ctx context.Context) error {
	r.ctx = ctx
	r.rt = wazero.NewRuntime(ctx)

	mb := r.rt.NewHostModuleBuilder("a")
	r.exportHost(mb)
	if _, err := mb.Instantiate(ctx); err != nil {
		return fmt.Errorf("host instantiate: %w", err)
	}

	compiled, err := r.rt.CompileModule(ctx, wasmBin)
	if err != nil {
		return fmt.Errorf("wasm compile: %w", err)
	}
	mod, err := r.rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		return fmt.Errorf("wasm instantiate: %w", err)
	}
	r.mod = mod
	r.mem = mod.Memory()

	// 解混淆字符串段
	decryptSeg := mod.ExportedFunction("__mergewasm_shared____wasm_decrypt_strings")
	if decryptSeg == nil {
		return fmt.Errorf("missing decrypt segment exporter")
	}
	for _, seg := range mergedDataSegments {
		if _, err := decryptSeg.Call(ctx, uint64(seg[0]), uint64(seg[1]), uint64(MergedDataKey)); err != nil {
			return fmt.Errorf("decrypt segment %v: %w", seg, err)
		}
	}
	// call_ctors
	if ct := mod.ExportedFunction("__wasm_call_ctors"); ct != nil {
		if _, err := ct.Call(ctx); err != nil {
			return fmt.Errorf("call_ctors: %w", err)
		}
	}
	// initRuntime(gameId, appKeyPtr)
	keyPtr, err := r.allocCBytes(r.appKey)
	if err != nil {
		return err
	}
	defer r.free(keyPtr)
	initFn := mod.ExportedFunction(exportsMap["initRuntime"])
	if _, err := initFn.Call(ctx, uint64(r.gameID), uint64(keyPtr)); err != nil {
		return fmt.Errorf("initRuntime: %w", err)
	}
	r.ready = true
	return nil
}

// exportHost 注册全部 host 导入函数（模块 "a"）
func (r *Runtime) exportHost(mb wazero.HostModuleBuilder) {
	// a: assertion
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, expr, file, line, fun uint32) {
		es := r.readCS(ctx, m, expr, 256)
		fs := "unknown"
		if file != 0 {
			fs = r.readCS(ctx, m, file, 256)
		}
		panic(fmt.Sprintf("TSDK assertion: %s at %s:%d %s", es, fs, line, r.readCS(ctx, m, fun, 256)))
	}).Export("a")
	// b: file write
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, filePtr, dataPtr, encPtr uint32) uint32 {
		path, err := r.resolvePath(r.readCS(ctx, m, filePtr, 2048))
		if err != nil {
			return 0
		}
		data := r.readCS(ctx, m, dataPtr, 1<<20)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			return 0
		}
		return 1
	}).Export("b")
	// c: stack
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, ptr, capacity uint32) uint32 {
		stk := "TSDK stack capture"
		if r.writeCS(ctx, m, ptr, capacity, stk) == 0 {
			return 0
		}
		return uint32(len(stk) + 1)
	}).Export("c")
	// d: version string
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, ptr, capacity uint32) uint32 {
		return r.writeCS(ctx, m, ptr, capacity, OfficialVersion)
	}).Export("d")
	// e: JS integrity VM -> 0
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, p1 uint32) uint32 { return 0 }).Export("e")
	// f: sensors no-op
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module) {}).Export("f")
	// g: file read
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, filePtr, outPtr, capacity, encPtr uint32) uint32 {
		path, err := r.resolvePath(r.readCS(ctx, m, filePtr, 2048))
		if err != nil {
			return 0
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return 0
		}
		return r.writeCS(ctx, m, outPtr, capacity, string(data))
	}).Export("g")
	// h: clock
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, clockID, low, high, outPtr uint32) uint32 {
		if clockID > 3 {
			return 28
		}
		var value int64
		if clockID == 0 {
			value = time.Now().UnixNano()
		} else {
			value = time.Since(startTS).Nanoseconds()
		}
		fmt.Fprintln(os.Stderr, "[host h] clockID =", clockID, "value =", value)
		m.Memory().WriteUint64Le(outPtr, uint64(value))
		return 0
	}).Export("h")
	// i: user path
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, ptr, capacity uint32) uint32 {
		return r.writeCS(ctx, m, ptr, capacity, r.dir+string(filepath.Separator))
	}).Export("i")
	// j: device
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, ptr, capacity uint32) uint32 {
		return r.writeCS(ctx, m, ptr, capacity, r.deviceString())
	}).Export("j")
	// k: runtime table
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, ptr, capacity uint32) uint32 {
		return r.writeBytes(ctx, m, ptr, capacity, officialRuntimeTable)
	}).Export("k")
	// l: -> 2
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module) uint32 { return 2 }).Export("l")
	// m: appId
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, ptr, capacity uint32) uint32 {
		return r.writeCS(ctx, m, ptr, capacity, r.appID)
	}).Export("m")
	// n: defaultAppId
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, ptr, capacity uint32) uint32 {
		return r.writeCS(ctx, m, ptr, capacity, DefaultAppID)
	}).Export("n")
	// o: function integrity no-op
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, a, b, x, y uint32) {}).Export("o")
	// p: stat
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, filePtr uint32) uint32 {
		path, err := r.resolvePath(r.readCS(ctx, m, filePtr, 2048))
		if err != nil {
			return 0
		}
		st, err := os.Stat(path)
		if err != nil {
			return 0
		}
		fn := m.ExportedFunction(exportsMap["createStats"])
		if fn == nil {
			return 0
		}
		out, err := fn.Call(ctx, uint64(st.Mode()), uint64(st.Size()), uint64(st.ModTime().UnixMilli()), uint64(st.ModTime().UnixMilli()))
		if err != nil || len(out) == 0 {
			return 0
		}
		return uint32(out[0])
	}).Export("p")
	// q: server time (写本地秒，保真可后续 http)
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, outPtr uint32) uint32 {
		now := time.Now().Unix()
		fmt.Fprintln(os.Stderr, "[host q] server time second =", now)
		m.Memory().WriteUint32Le(outPtr, uint32(now))
		return 1
	}).Export("q")
	// r: memory grow -> throw
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, size uint32) uint32 { panic("TSDK cannot grow memory") }).Export("r")
	// s: now ms
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module) float64 {
		return float64(time.Now().UnixMilli())
	}).Export("s")
	// t: file append
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, filePtr, dataPtr, encPtr uint32) uint32 {
		path, err := r.resolvePath(r.readCS(ctx, m, filePtr, 2048))
		if err != nil {
			return 0
		}
		data := r.readCS(ctx, m, dataPtr, 1<<20)
		os.MkdirAll(filepath.Dir(path), 0o755)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return 0
		}
		defer f.Close()
		if _, err := f.WriteString(data); err != nil {
			return 0
		}
		return 1
	}).Export("t")
	// u: abort
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module) { panic("TSDK aborted") }).Export("u")
	// v: tqos report -> 0
	mb.NewFunctionBuilder().WithFunc(func(ctx context.Context, m api.Module, ptr, length uint32) uint32 { return 0 }).Export("v")
}

func (r *Runtime) deviceString() string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	model := "Linux " + arch
	system := readKernelRelease() // os.release()
	return fmt.Sprintf("%s;linux;%s;Node.js;", model, system)
}

func readKernelRelease() string {
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return "Linux"
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "Linux"
	}
	return s
}

// resolvePath 防路径逃逸
func (r *Runtime) resolvePath(input string) (string, error) {
	v := strings.ReplaceAll(input, "\\", "/")
	rel := strings.TrimLeft(v, "/")
	target := filepath.Join(r.dir, filepath.FromSlash(rel))
	root := filepath.Clean(r.dir) + string(filepath.Separator)
	if target != filepath.Clean(r.dir) && !strings.HasPrefix(target, root) {
		return "", fmt.Errorf("path escaped data dir")
	}
	return target, nil
}

// ---- 内存辅助 ----

func (r *Runtime) readCS(ctx context.Context, m api.Module, ptr uint32, max uint32) string {
	mem := m.Memory()
	size := mem.Size()
	limit := ptr + max
	if limit > size {
		limit = size
	}
	end := ptr
	for end < limit {
		b, ok := mem.ReadByte(end)
		if !ok || b == 0 {
			break
		}
		end++
	}
	if end == limit {
		return ""
	}
	buf, _ := mem.Read(ptr, uint32(end-ptr))
	return string(buf)
}

func (r *Runtime) writeCS(ctx context.Context, m api.Module, ptr, capacity uint32, s string) uint32 {
	data := []byte(s)
	if len(data)+1 > int(capacity) {
		return 0
	}
	mem := m.Memory()
	mem.Write(ptr, data)
	mem.WriteByte(ptr+uint32(len(data)), 0)
	return ptr
}

func (r *Runtime) writeBytes(ctx context.Context, m api.Module, ptr, capacity uint32, data []byte) uint32 {
	if uint32(len(data)) > capacity {
		return 0
	}
	m.Memory().Write(ptr, data)
	return uint32(len(data))
}

// alloc 分配 buffer 并拷贝 data，返回 ptr
func (r *Runtime) alloc(data []byte) (uint32, error) {
	fn := r.mod.ExportedFunction(exportsMap["createBuffer"])
	n := len(data)
	if n == 0 {
		n = 1
	}
	out, err := fn.Call(r.ctx, uint64(n))
	if err != nil || len(out) == 0 {
		return 0, fmt.Errorf("createBuffer failed")
	}
	ptr := uint32(out[0])
	if len(data) > 0 {
		r.mem.Write(ptr, data)
	}
	return ptr, nil
}

func (r *Runtime) allocCBytes(s string) (uint32, error) {
	data := append([]byte(s), 0)
	return r.alloc(data)
}

func (r *Runtime) free(ptr uint32) {
	fn := r.mod.ExportedFunction(exportsMap["destroyBuffer"])
	if fn == nil {
		return
	}
	fn.Call(r.ctx, uint64(ptr))
}

func (r *Runtime) fn(symbol string) api.Function { return r.mod.ExportedFunction(symbol) }


// Ready 是否初始化完成
func (r *Runtime) Ready() bool { return r.ready }

// Encrypt 加密请求体（对应 Node cryptoWasm.encryptBuffer）
func (r *Runtime) Encrypt(data []byte) ([]byte, error) {
	if !r.ready {
		return nil, fmt.Errorf("TSDK runtime not ready")
	}
	ptr, err := r.alloc(data)
	if err != nil {
		return nil, err
	}
	defer r.free(ptr)
	enc := r.fn(exportsMap["encryptData"])
	if _, err := enc.Call(r.ctx, uint64(ptr), uint64(len(data))); err != nil {
		return nil, fmt.Errorf("encryptData: %w", err)
	}
	raw, _ := r.mem.Read(ptr, uint32(len(data)))
	return append([]byte(nil), raw...), nil
}

// Decrypt 解密响应体
func (r *Runtime) Decrypt(data []byte) ([]byte, error) {
	if !r.ready {
		return nil, fmt.Errorf("TSDK runtime not ready")
	}
	ptr, err := r.alloc(data)
	if err != nil {
		return nil, err
	}
	defer r.free(ptr)
	dec := r.fn(exportsMap["decryptData"])
	if _, err := dec.Call(r.ctx, uint64(ptr), uint64(len(data))); err != nil {
		return nil, fmt.Errorf("decryptData: %w", err)
	}
	raw, _ := r.mem.Read(ptr, uint32(len(data)))
	return append([]byte(nil), raw...), nil
}

// EncryptedInitInfo 初始化加密凭据（登录 auth_token）
func (r *Runtime) EncryptedInitInfo() (string, error) {
	if !r.ready {
		return "", fmt.Errorf("TSDK runtime not ready")
	}
	fn := r.fn(exportsMap["getEncryptedInitInfo"])
	out, err := fn.Call(r.ctx)
	if err != nil || len(out) == 0 || out[0] == 0 {
		return "", fmt.Errorf("getEncryptedInitInfo failed")
	}
	return r.readCS(r.ctx, r.mod, uint32(out[0]), 64*1024), nil
}

// BindUser 绑定用户 openId
func (r *Runtime) BindUser(openID string) error {
	if !r.ready {
		return fmt.Errorf("TSDK runtime not ready")
	}
	input, err := r.allocCBytes(openID)
	if err != nil {
		return err
	}
	defer r.free(input)
	initFn := r.fn(exportsMap["initRuntime"])
	if _, err := initFn.Call(r.ctx, uint64(r.gameID), uint64(input)); err != nil {
		return fmt.Errorf("bindUser: %w", err)
	}
	return nil
}

// HeartbeatTick 心跳
func (r *Runtime) HeartbeatTick() error {
	if !r.ready {
		return fmt.Errorf("TSDK runtime not ready")
	}
	fn := r.fn(exportsMap["sendHeartbeatTick"])
	_, err := fn.Call(r.ctx)
	return err
}

// ProcessReceivedData 处理下行数据队列（对齐 Node tsdk-runtime.js processReceivedData）
func (r *Runtime) ProcessReceivedData() error {
	if !r.ready {
		return fmt.Errorf("TSDK runtime not ready")
	}
	fn := r.fn(exportsMap["processReceivedData"])
	_, err := fn.Call(r.ctx)
	return err
}

// SendStatus 主动上报状态（对齐 Node tsdk-runtime.js sendStatus）
func (r *Runtime) SendStatus() error {
	if !r.ready {
		return fmt.Errorf("TSDK runtime not ready")
	}
	fn := r.fn(exportsMap["sendStatus"])
	_, err := fn.Call(r.ctx)
	return err
}

// DetectSpeedHack 速度检测（对齐 Node tsdk-runtime.js detectSpeedHack）
func (r *Runtime) DetectSpeedHack(elapsedMs int64) error {
	if !r.ready {
		return fmt.Errorf("TSDK runtime not ready")
	}
	if elapsedMs < 0 {
		elapsedMs = 0
	}
	fn := r.fn(exportsMap["detectSpeedHack"])
	_, err := fn.Call(r.ctx, uint64(elapsedMs))
	return err
}

// GetDataToServer 取待上报的 ACE 数据（对齐 Node tsdk-runtime.js getDataToServer）：
// wasm 通过 lengthPtr 写出数据长度，返回数据指针；无数据返回空切片。
func (r *Runtime) GetDataToServer() ([]byte, error) {
	if !r.ready {
		return nil, fmt.Errorf("TSDK runtime not ready")
	}
	// 分配 4 字节 int32 长度槽（初始 0）
	lenPtr, err := r.alloc(make([]byte, 4))
	if err != nil {
		return nil, err
	}
	defer r.free(lenPtr)
	fn := r.fn(exportsMap["getDataToServer"])
	out, err := fn.Call(r.ctx, uint64(lenPtr))
	if err != nil {
		return nil, fmt.Errorf("getDataToServer: %w", err)
	}
	if len(out) == 0 || out[0] == 0 {
		return nil, nil
	}
	dataPtr := uint32(out[0])
	length, ok := r.mem.ReadUint32Le(lenPtr)
	if !ok || int32(length) <= 0 {
		return nil, nil
	}
	buf, ok := r.mem.Read(dataPtr, length)
	if !ok {
		return nil, fmt.Errorf("getDataToServer: out of bounds")
	}
	return append([]byte(nil), buf...), nil
}

// SendDataFromServer 回灌服务端下发数据（对齐 Node tsdk-runtime.js sendDataFromServer）
func (r *Runtime) SendDataFromServer(data []byte) error {
	if !r.ready {
		return fmt.Errorf("TSDK runtime not ready")
	}
	if len(data) == 0 {
		return nil
	}
	ptr, err := r.alloc(data)
	if err != nil {
		return err
	}
	defer r.free(ptr)
	fn := r.fn(exportsMap["sendDataFromServer"])
	_, err = fn.Call(r.ctx, uint64(ptr), uint64(len(data)))
	return err
}

// CheckFunctionArray 完整性校验（对齐 Node tsdk-runtime.js checkFunctionArray）。
// names 为函数名列表（Node 传方法 toString 的字符串数组；Go 侧以导出名等价替代）。
func (r *Runtime) CheckFunctionArray(names []string, typeFlag int64) error {
	if !r.ready {
		return fmt.Errorf("TSDK runtime not ready")
	}
	if len(names) == 0 {
		return nil
	}
	fn := r.fn(exportsMap["checkFuncArray"])
	if fn == nil {
		return nil
	}
	// 构建字符串指针数组（与 Node allocCString + u32 指针数组一致）
	strPtrs := make([]uint32, 0, len(names))
	cleanup := func() {
		for _, p := range strPtrs {
			r.free(p)
		}
	}
	for _, n := range names {
		p, err := r.allocCBytes(n)
		if err != nil {
			cleanup()
			return err
		}
		strPtrs = append(strPtrs, p)
	}
	ptrArr, err := r.alloc(make([]byte, len(strPtrs)*4))
	if err != nil {
		cleanup()
		return err
	}
	defer func() {
		r.free(ptrArr)
		cleanup()
	}()
	for i, p := range strPtrs {
		r.mem.WriteUint32Le(ptrArr+uint32(i*4), p)
	}
	_, err = fn.Call(r.ctx, uint64(ptrArr), uint64(len(strPtrs)), uint64(typeFlag))
	return err
}

// Close 释放
func (r *Runtime) Close() {
	if r.rt != nil {
		r.rt.Close(r.ctx)
	}
}
