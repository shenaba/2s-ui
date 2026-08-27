package notify

import "strings"

// DefaultLang is what an unset or unknown notifyLang falls back to.
const DefaultLang = "en"

// Langs are the languages notifications can be written in, using the same keys
// as the panel's own locales (frontend/src/locales/index.ts) so the settings
// page can offer one list for both.
var Langs = []string{"en", "fa", "ru", "vi", "zhHans", "zhHant"}

// messages is the notification wording, keyed by language and then by the
// message key describe() returns.
//
// This table is the only i18n in the Go half of the panel -- everything else is
// translated in the SPA -- so it is deliberately a plain map rather than a
// dependency: seventeen strings do not justify pulling in a framework and an
// embedded catalogue.
//
// The notification language is set separately from the panel's, because the two
// have different audiences: the operator reads the panel, while alerts often get
// forwarded to someone else.
//
// Placeholders are {name} and are substituted verbatim, so a translation may
// reorder or drop them freely.
var messages = map[string]map[string]string{
	"en": {
		"node.down":              "\U0001F534 Node {subject} is down",
		"node.down.err":          "\U0001F534 Node {subject} is down\n{error}",
		"node.up":                "\U0001F7E2 Node {subject} is back online",
		"node.up.latency":        "\U0001F7E2 Node {subject} is back online ({latency} ms)",
		"core.crash":             "\U0001F525 sing-box failed to start",
		"core.crash.err":         "\U0001F525 sing-box failed to start\n{error}",
		"core.up":                "✅ sing-box is running again",
		"client.depleted":        "⛔ {count} client(s) disabled: {names}",
		"client.expiring":        "⏳ {subject} is about to run out",
		"client.expiring.days":   "⏳ {subject} expires in {days} day(s)",
		"client.expiring.volume": "⏳ {subject} has {volume} of traffic left",
		"cpu.high":               "\U0001F4C8 CPU at {percent}% (threshold {threshold}%)",
		"memory.high":            "\U0001F4C8 Memory at {percent}% (threshold {threshold}%)",
		"login.success":          "\U0001F513 {user} signed in from {ip}",
		"login.failed.one":       "⚠️ Failed login for {user} from {ip}",
		"login.failed":           "⚠️ {count} failed logins for {user} from {ip}",
		"login.banned":           "\U0001F6AB {ip} blocked for {minutes} minute(s) after repeated failed logins",
	},
	"zhHans": {
		"node.down":              "\U0001F534 节点 {subject} 已离线",
		"node.down.err":          "\U0001F534 节点 {subject} 已离线\n{error}",
		"node.up":                "\U0001F7E2 节点 {subject} 已恢复",
		"node.up.latency":        "\U0001F7E2 节点 {subject} 已恢复（{latency} 毫秒）",
		"core.crash":             "\U0001F525 sing-box 启动失败",
		"core.crash.err":         "\U0001F525 sing-box 启动失败\n{error}",
		"core.up":                "✅ sing-box 已恢复运行",
		"client.depleted":        "⛔ 已停用 {count} 个客户端：{names}",
		"client.expiring":        "⏳ {subject} 即将用尽",
		"client.expiring.days":   "⏳ {subject} 还有 {days} 天到期",
		"client.expiring.volume": "⏳ {subject} 剩余流量 {volume}",
		"cpu.high":               "\U0001F4C8 CPU 占用 {percent}%（阈值 {threshold}%）",
		"memory.high":            "\U0001F4C8 内存占用 {percent}%（阈值 {threshold}%）",
		"login.success":          "\U0001F513 {user} 从 {ip} 登录成功",
		"login.failed.one":       "⚠️ {user} 从 {ip} 登录失败",
		"login.failed":           "⚠️ {user} 从 {ip} 登录失败 {count} 次",
		"login.banned":           "\U0001F6AB {ip} 因多次登录失败被封禁 {minutes} 分钟",
	},
	"zhHant": {
		"node.down":              "\U0001F534 節點 {subject} 已離線",
		"node.down.err":          "\U0001F534 節點 {subject} 已離線\n{error}",
		"node.up":                "\U0001F7E2 節點 {subject} 已恢復",
		"node.up.latency":        "\U0001F7E2 節點 {subject} 已恢復（{latency} 毫秒）",
		"core.crash":             "\U0001F525 sing-box 啟動失敗",
		"core.crash.err":         "\U0001F525 sing-box 啟動失敗\n{error}",
		"core.up":                "✅ sing-box 已恢復運行",
		"client.depleted":        "⛔ 已停用 {count} 個客戶端：{names}",
		"client.expiring":        "⏳ {subject} 即將用盡",
		"client.expiring.days":   "⏳ {subject} 還有 {days} 天到期",
		"client.expiring.volume": "⏳ {subject} 剩餘流量 {volume}",
		"cpu.high":               "\U0001F4C8 CPU 佔用 {percent}%（閾值 {threshold}%）",
		"memory.high":            "\U0001F4C8 記憶體佔用 {percent}%（閾值 {threshold}%）",
		"login.success":          "\U0001F513 {user} 從 {ip} 登入成功",
		"login.failed.one":       "⚠️ {user} 從 {ip} 登入失敗",
		"login.failed":           "⚠️ {user} 從 {ip} 登入失敗 {count} 次",
		"login.banned":           "\U0001F6AB {ip} 因多次登入失敗被封鎖 {minutes} 分鐘",
	},
	"ru": {
		"node.down":              "\U0001F534 Узел {subject} недоступен",
		"node.down.err":          "\U0001F534 Узел {subject} недоступен\n{error}",
		"node.up":                "\U0001F7E2 Узел {subject} снова в сети",
		"node.up.latency":        "\U0001F7E2 Узел {subject} снова в сети ({latency} мс)",
		"core.crash":             "\U0001F525 Не удалось запустить sing-box",
		"core.crash.err":         "\U0001F525 Не удалось запустить sing-box\n{error}",
		"core.up":                "✅ sing-box снова работает",
		"client.depleted":        "⛔ Отключено клиентов: {count} — {names}",
		"client.expiring":        "⏳ Лимит клиента {subject} скоро закончится",
		"client.expiring.days":   "⏳ Срок действия {subject} истекает через {days} дн.",
		"client.expiring.volume": "⏳ У {subject} осталось трафика: {volume}",
		"cpu.high":               "\U0001F4C8 Загрузка CPU {percent}% (порог {threshold}%)",
		"memory.high":            "\U0001F4C8 Использование памяти {percent}% (порог {threshold}%)",
		"login.success":          "\U0001F513 {user} вошёл в панель с {ip}",
		"login.failed.one":       "⚠️ Неудачный вход {user} с {ip}",
		"login.failed":           "⚠️ Неудачных попыток входа {user} с {ip}: {count}",
		"login.banned":           "\U0001F6AB {ip} заблокирован на {minutes} мин. после серии неудачных входов",
	},
	"fa": {
		"node.down":              "\U0001F534 نود {subject} از دسترس خارج شد",
		"node.down.err":          "\U0001F534 نود {subject} از دسترس خارج شد\n{error}",
		"node.up":                "\U0001F7E2 نود {subject} دوباره آنلاین شد",
		"node.up.latency":        "\U0001F7E2 نود {subject} دوباره آنلاین شد ({latency} میلی‌ثانیه)",
		"core.crash":             "\U0001F525 اجرای sing-box با خطا مواجه شد",
		"core.crash.err":         "\U0001F525 اجرای sing-box با خطا مواجه شد\n{error}",
		"core.up":                "✅ sing-box دوباره در حال اجراست",
		"client.depleted":        "⛔ {count} کاربر غیرفعال شد: {names}",
		"client.expiring":        "⏳ سهمیه {subject} رو به پایان است",
		"client.expiring.days":   "⏳ {subject} تا {days} روز دیگر منقضی می‌شود",
		"client.expiring.volume": "⏳ {subject} {volume} ترافیک باقی دارد",
		"cpu.high":               "\U0001F4C8 مصرف پردازنده {percent}٪ (آستانه {threshold}٪)",
		"memory.high":            "\U0001F4C8 مصرف حافظه {percent}٪ (آستانه {threshold}٪)",
		"login.success":          "\U0001F513 {user} از {ip} وارد شد",
		"login.failed.one":       "⚠️ ورود ناموفق {user} از {ip}",
		"login.failed":           "⚠️ {count} ورود ناموفق {user} از {ip}",
		"login.banned":           "\U0001F6AB {ip} به دلیل ورودهای ناموفق برای {minutes} دقیقه مسدود شد",
	},
	"vi": {
		"node.down":              "\U0001F534 Node {subject} đã ngoại tuyến",
		"node.down.err":          "\U0001F534 Node {subject} đã ngoại tuyến\n{error}",
		"node.up":                "\U0001F7E2 Node {subject} đã trực tuyến trở lại",
		"node.up.latency":        "\U0001F7E2 Node {subject} đã trực tuyến trở lại ({latency} ms)",
		"core.crash":             "\U0001F525 sing-box khởi động thất bại",
		"core.crash.err":         "\U0001F525 sing-box khởi động thất bại\n{error}",
		"core.up":                "✅ sing-box đã chạy trở lại",
		"client.depleted":        "⛔ Đã vô hiệu hóa {count} client: {names}",
		"client.expiring":        "⏳ {subject} sắp hết hạn mức",
		"client.expiring.days":   "⏳ {subject} còn {days} ngày là hết hạn",
		"client.expiring.volume": "⏳ {subject} còn {volume} lưu lượng",
		"cpu.high":               "\U0001F4C8 CPU {percent}% (ngưỡng {threshold}%)",
		"memory.high":            "\U0001F4C8 Bộ nhớ {percent}% (ngưỡng {threshold}%)",
		"login.success":          "\U0001F513 {user} đã đăng nhập từ {ip}",
		"login.failed.one":       "⚠️ Đăng nhập thất bại: {user} từ {ip}",
		"login.failed":           "⚠️ {count} lần đăng nhập thất bại: {user} từ {ip}",
		"login.banned":           "\U0001F6AB {ip} bị chặn {minutes} phút sau nhiều lần đăng nhập thất bại",
	},
}

// translate looks key up in lang, falling back to English and finally to the
// key itself, then substitutes the placeholders.
//
// Falling back per key rather than per language matters: a key added here in
// English but not yet translated should render in English everywhere, not turn
// the whole message into a bare identifier.
func translate(lang, key string, params map[string]string) string {
	text := ""
	if table, ok := messages[lang]; ok {
		text = table[key]
	}
	if text == "" {
		text = messages[DefaultLang][key]
	}
	if text == "" {
		text = key
	}
	if len(params) == 0 {
		return text
	}
	pairs := make([]string, 0, len(params)*2)
	for k, v := range params {
		pairs = append(pairs, "{"+k+"}", v)
	}
	return strings.NewReplacer(pairs...).Replace(text)
}
