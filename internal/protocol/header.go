package protocol

// DefaultUserAgent 门户 WAF 的浏览器 UA（Python _headers 同款，校事实）。
// 守护与 CLI 都从这里取——同一串字面量只放一处；异校用 ini 的
// user_agent 覆盖，不改代码。
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
