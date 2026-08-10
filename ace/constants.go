package ace

// TSDK / ACE 常量，对齐 Node utils/tsdk-runtime.js

const (
	OfficialVersion  = "v3.8.2.1783066265"
	OfficialSHA256   = "705e326caad538d6cccb40cb1bd54573525a42d12215c9da9c9c513ec4850a5f"
	DefaultAppID     = "1112386029"
	DefaultGameID    = int64(3167)
	DefaultAppKey    = "0"
	MergedDataKey    = 1871261153
	DefaultDataDir   = "/tmp/tsdk"
)

// 导出函数符号映射（WASM 导出名）
var exportsMap = map[string]string{
	"memory":              "w",
	"createStats":         "y",
	"reportUrls":          "z",
	"createBuffer":        "A",
	"destroyBuffer":       "B",
	"getResult":           "C",
	"reportStackHash":     "D",
	"sendStatus":          "E",
	"setFeatureGrayValue": "F",
	"initRuntime":         "G",
	"getEncryptedInitInfo": "H",
	"getMsgLen":           "I",
	"getMsg":              "J",
	"checkFuncArray":      "K",
	"addJsInfo":           "L",
	"sendHeartbeatTick":   "M",
	"getDataToServer":     "N",
	"sendDataFromServer":  "O",
	"processReceivedData": "P",
	"sendToGs":            "Q",
	"sendToGsFast":        "R",
	"notify":              "S",
	"notifyUpper":         "T",
	"generateToken":       "aa",
	"encryptData":         "ba",
	"decryptData":         "ca",
	"encryptDataV2":       "da",
	"decryptDataV2":       "ea",
	"detectSpeedHack":     "fa",
}

var requiredExports = []string{
	"memory", "createBuffer", "destroyBuffer", "getResult", "initRuntime",
	"sendHeartbeatTick", "getDataToServer", "sendDataFromServer",
	"generateToken", "encryptData", "decryptData",
}

// 需要解混淆的内存段 [ptr, length]
var mergedDataSegments = [][2]int{
	{1024, 5541}, {6580, 8989}, {15585, 33}, {15643, 1}, {15655, 21},
	{15701, 1}, {15713, 21}, {15759, 1}, {15771, 30}, {15826, 14},
	{15875, 1}, {15887, 21}, {15933, 1}, {15945, 671}, {16632, 400},
	{17040, 103}, {67371008, 404},
}

// 官方运行时常量表
var officialRuntimeTable = []byte{
	93, 86, 110, 34, 65, 129, 8, 113, 53, 192, 121, 32, 86, 162, 255, 139,
	217, 70, 223, 0, 45, 176, 85, 103, 234, 116, 120, 194, 206, 7, 176, 222,
	56, 6, 161, 159, 154, 231, 93, 229, 39, 107, 197, 136, 167, 52, 155, 228,
	209, 117, 218, 8, 107, 241, 32, 62, 53, 200, 238,
}
