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
		"digest.clients":     "Clients",
		"digest.enabled":     "enabled",
		"digest.online":      "online",
		"digest.nodes":       "Nodes",
		"digest.nodesOnline": "online",
		"digest.core":        "Core",
		"digest.running":     "running",
		"digest.stopped":     "stopped",
		"digest.depleted":    "Depleted",
		"digest.expiring":    "Expiring soon",
		"digest.more":        "and {count} more",

		"node.down":              "\U0001F534 Node {subject} is down",
		"node.down.err":          "\U0001F534 Node {subject} is down\n{error}",
		"node.up":                "\U0001F7E2 Node {subject} is back online",
		"node.up.latency":        "\U0001F7E2 Node {subject} is back online ({latency} ms)",
		"core.crash":             "\U0001F525 sing-box failed to start",
		"core.crash.err":         "\U0001F525 sing-box failed to start\n{error}",
		"core.up":                "✅ sing-box is running again",
		"outbound.down":          "\U0001F534 Outbound {subject} is unreachable",
		"outbound.down.err":      "\U0001F534 Outbound {subject} is unreachable\n{error}",
		"outbound.up":            "\U0001F7E2 Outbound {subject} is reachable again",
		"outbound.up.latency":    "\U0001F7E2 Outbound {subject} is reachable again ({latency} ms)",
		"client.depleted":        "⛔ {count} client(s) disabled: {names}",
		"client.expiring":        "⏳ {subject} is about to run out",
		"client.expiring.days":   "⏳ {subject} expires in {days} day(s)",
		"client.expiring.volume": "⏳ {subject} has {volume} of traffic left",
		"self.expiring.days":     "⏳ {name}: your subscription expires in {days} day(s).",
		"self.expiring.volume":   "⏳ {name}: you have {volume} of traffic left.",
		"self.depleted":          "⛔ {name}: your subscription has run out and is now disabled.",
		"cpu.high":               "\U0001F4C8 CPU at {percent}% (threshold {threshold}%)",
		"memory.high":            "\U0001F4C8 Memory at {percent}% (threshold {threshold}%)",
		"login.success":          "\U0001F513 {user} signed in from {ip}",
		"login.failed.one":       "⚠️ Failed login for {user} from {ip}",
		"login.failed":           "⚠️ {count} failed logins for {user} from {ip}",
		"login.banned":           "\U0001F6AB {ip} blocked for {minutes} minute(s) after repeated failed logins",
	},
	"zhHans": {
		"digest.clients":     "客户端",
		"digest.enabled":     "启用",
		"digest.online":      "在线",
		"digest.nodes":       "节点",
		"digest.nodesOnline": "在线",
		"digest.core":        "核心",
		"digest.running":     "运行中",
		"digest.stopped":     "已停止",
		"digest.depleted":    "已耗尽",
		"digest.expiring":    "即将到期",
		"digest.more":        "另有 {count} 个",

		"node.down":              "\U0001F534 节点 {subject} 已离线",
		"node.down.err":          "\U0001F534 节点 {subject} 已离线\n{error}",
		"node.up":                "\U0001F7E2 节点 {subject} 已恢复",
		"node.up.latency":        "\U0001F7E2 节点 {subject} 已恢复（{latency} 毫秒）",
		"core.crash":             "\U0001F525 sing-box 启动失败",
		"core.crash.err":         "\U0001F525 sing-box 启动失败\n{error}",
		"core.up":                "✅ sing-box 已恢复运行",
		"outbound.down":          "\U0001F534 出站 {subject} 不可达",
		"outbound.down.err":      "\U0001F534 出站 {subject} 不可达\n{error}",
		"outbound.up":            "\U0001F7E2 出站 {subject} 已恢复",
		"outbound.up.latency":    "\U0001F7E2 出站 {subject} 已恢复（{latency} 毫秒）",
		"client.depleted":        "⛔ 已停用 {count} 个客户端：{names}",
		"client.expiring":        "⏳ {subject} 即将用尽",
		"client.expiring.days":   "⏳ {subject} 还有 {days} 天到期",
		"client.expiring.volume": "⏳ {subject} 剩余流量 {volume}",
		"self.expiring.days":     "⏳ {name}：您的订阅还有 {days} 天到期。",
		"self.expiring.volume":   "⏳ {name}：您的订阅剩余流量 {volume}。",
		"self.depleted":          "⛔ {name}：您的订阅已用尽，现已停用。",
		"cpu.high":               "\U0001F4C8 CPU 占用 {percent}%（阈值 {threshold}%）",
		"memory.high":            "\U0001F4C8 内存占用 {percent}%（阈值 {threshold}%）",
		"login.success":          "\U0001F513 {user} 从 {ip} 登录成功",
		"login.failed.one":       "⚠️ {user} 从 {ip} 登录失败",
		"login.failed":           "⚠️ {user} 从 {ip} 登录失败 {count} 次",
		"login.banned":           "\U0001F6AB {ip} 因多次登录失败被封禁 {minutes} 分钟",
	},
	"zhHant": {
		"digest.clients":     "客戶端",
		"digest.enabled":     "啟用",
		"digest.online":      "線上",
		"digest.nodes":       "節點",
		"digest.nodesOnline": "線上",
		"digest.core":        "核心",
		"digest.running":     "運行中",
		"digest.stopped":     "已停止",
		"digest.depleted":    "已耗盡",
		"digest.expiring":    "即將到期",
		"digest.more":        "另有 {count} 個",

		"node.down":              "\U0001F534 節點 {subject} 已離線",
		"node.down.err":          "\U0001F534 節點 {subject} 已離線\n{error}",
		"node.up":                "\U0001F7E2 節點 {subject} 已恢復",
		"node.up.latency":        "\U0001F7E2 節點 {subject} 已恢復（{latency} 毫秒）",
		"core.crash":             "\U0001F525 sing-box 啟動失敗",
		"core.crash.err":         "\U0001F525 sing-box 啟動失敗\n{error}",
		"core.up":                "✅ sing-box 已恢復運行",
		"outbound.down":          "\U0001F534 出站 {subject} 無法連線",
		"outbound.down.err":      "\U0001F534 出站 {subject} 無法連線\n{error}",
		"outbound.up":            "\U0001F7E2 出站 {subject} 已恢復",
		"outbound.up.latency":    "\U0001F7E2 出站 {subject} 已恢復（{latency} 毫秒）",
		"client.depleted":        "⛔ 已停用 {count} 個客戶端：{names}",
		"client.expiring":        "⏳ {subject} 即將用盡",
		"client.expiring.days":   "⏳ {subject} 還有 {days} 天到期",
		"client.expiring.volume": "⏳ {subject} 剩餘流量 {volume}",
		"self.expiring.days":     "⏳ {name}：您的訂閱還有 {days} 天到期。",
		"self.expiring.volume":   "⏳ {name}：您的訂閱剩餘流量 {volume}。",
		"self.depleted":          "⛔ {name}：您的訂閱已用盡，現已停用。",
		"cpu.high":               "\U0001F4C8 CPU 佔用 {percent}%（閾值 {threshold}%）",
		"memory.high":            "\U0001F4C8 記憶體佔用 {percent}%（閾值 {threshold}%）",
		"login.success":          "\U0001F513 {user} 從 {ip} 登入成功",
		"login.failed.one":       "⚠️ {user} 從 {ip} 登入失敗",
		"login.failed":           "⚠️ {user} 從 {ip} 登入失敗 {count} 次",
		"login.banned":           "\U0001F6AB {ip} 因多次登入失敗被封鎖 {minutes} 分鐘",
	},
	"ru": {
		"digest.clients":     "Клиенты",
		"digest.enabled":     "включено",
		"digest.online":      "онлайн",
		"digest.nodes":       "Узлы",
		"digest.nodesOnline": "онлайн",
		"digest.core":        "Ядро",
		"digest.running":     "работает",
		"digest.stopped":     "остановлено",
		"digest.depleted":    "Исчерпаны",
		"digest.expiring":    "Скоро закончатся",
		"digest.more":        "и ещё {count}",

		"node.down":              "\U0001F534 Узел {subject} недоступен",
		"node.down.err":          "\U0001F534 Узел {subject} недоступен\n{error}",
		"node.up":                "\U0001F7E2 Узел {subject} снова в сети",
		"node.up.latency":        "\U0001F7E2 Узел {subject} снова в сети ({latency} мс)",
		"core.crash":             "\U0001F525 Не удалось запустить sing-box",
		"core.crash.err":         "\U0001F525 Не удалось запустить sing-box\n{error}",
		"core.up":                "✅ sing-box снова работает",
		"outbound.down":          "\U0001F534 Исходящее соединение {subject} недоступно",
		"outbound.down.err":      "\U0001F534 Исходящее соединение {subject} недоступно\n{error}",
		"outbound.up":            "\U0001F7E2 Исходящее соединение {subject} снова доступно",
		"outbound.up.latency":    "\U0001F7E2 Исходящее соединение {subject} снова доступно ({latency} мс)",
		"client.depleted":        "⛔ Отключено клиентов: {count} — {names}",
		"client.expiring":        "⏳ Лимит клиента {subject} скоро закончится",
		"client.expiring.days":   "⏳ Срок действия {subject} истекает через {days} дн.",
		"client.expiring.volume": "⏳ У {subject} осталось трафика: {volume}",
		"self.expiring.days":     "⏳ {name}: ваша подписка истекает через {days} дн.",
		"self.expiring.volume":   "⏳ {name}: у вас осталось трафика: {volume}.",
		"self.depleted":          "⛔ {name}: ваша подписка исчерпана и отключена.",
		"cpu.high":               "\U0001F4C8 Загрузка CPU {percent}% (порог {threshold}%)",
		"memory.high":            "\U0001F4C8 Использование памяти {percent}% (порог {threshold}%)",
		"login.success":          "\U0001F513 {user} вошёл в панель с {ip}",
		"login.failed.one":       "⚠️ Неудачный вход {user} с {ip}",
		"login.failed":           "⚠️ Неудачных попыток входа {user} с {ip}: {count}",
		"login.banned":           "\U0001F6AB {ip} заблокирован на {minutes} мин. после серии неудачных входов",
	},
	"fa": {
		"digest.clients":     "کاربران",
		"digest.enabled":     "فعال",
		"digest.online":      "آنلاین",
		"digest.nodes":       "نودها",
		"digest.nodesOnline": "آنلاین",
		"digest.core":        "هسته",
		"digest.running":     "در حال اجرا",
		"digest.stopped":     "متوقف",
		"digest.depleted":    "تمام‌شده",
		"digest.expiring":    "نزدیک به پایان",
		"digest.more":        "و {count} مورد دیگر",

		"node.down":              "\U0001F534 نود {subject} از دسترس خارج شد",
		"node.down.err":          "\U0001F534 نود {subject} از دسترس خارج شد\n{error}",
		"node.up":                "\U0001F7E2 نود {subject} دوباره آنلاین شد",
		"node.up.latency":        "\U0001F7E2 نود {subject} دوباره آنلاین شد ({latency} میلی‌ثانیه)",
		"core.crash":             "\U0001F525 اجرای sing-box با خطا مواجه شد",
		"core.crash.err":         "\U0001F525 اجرای sing-box با خطا مواجه شد\n{error}",
		"core.up":                "✅ sing-box دوباره در حال اجراست",
		"outbound.down":          "\U0001F534 خروجی {subject} در دسترس نیست",
		"outbound.down.err":      "\U0001F534 خروجی {subject} در دسترس نیست\n{error}",
		"outbound.up":            "\U0001F7E2 خروجی {subject} دوباره در دسترس است",
		"outbound.up.latency":    "\U0001F7E2 خروجی {subject} دوباره در دسترس است ({latency} میلی‌ثانیه)",
		"client.depleted":        "⛔ {count} کاربر غیرفعال شد: {names}",
		"client.expiring":        "⏳ سهمیه {subject} رو به پایان است",
		"client.expiring.days":   "⏳ {subject} تا {days} روز دیگر منقضی می‌شود",
		"client.expiring.volume": "⏳ {subject} {volume} ترافیک باقی دارد",
		"self.expiring.days":     "⏳ {name}: اشتراک شما تا {days} روز دیگر منقضی می‌شود.",
		"self.expiring.volume":   "⏳ {name}: {volume} ترافیک برای شما باقی مانده است.",
		"self.depleted":          "⛔ {name}: اشتراک شما به پایان رسید و غیرفعال شد.",
		"cpu.high":               "\U0001F4C8 مصرف پردازنده {percent}٪ (آستانه {threshold}٪)",
		"memory.high":            "\U0001F4C8 مصرف حافظه {percent}٪ (آستانه {threshold}٪)",
		"login.success":          "\U0001F513 {user} از {ip} وارد شد",
		"login.failed.one":       "⚠️ ورود ناموفق {user} از {ip}",
		"login.failed":           "⚠️ {count} ورود ناموفق {user} از {ip}",
		"login.banned":           "\U0001F6AB {ip} به دلیل ورودهای ناموفق برای {minutes} دقیقه مسدود شد",
	},
	"vi": {
		"digest.clients":     "Client",
		"digest.enabled":     "bật",
		"digest.online":      "trực tuyến",
		"digest.nodes":       "Node",
		"digest.nodesOnline": "trực tuyến",
		"digest.core":        "Lõi",
		"digest.running":     "đang chạy",
		"digest.stopped":     "đã dừng",
		"digest.depleted":    "Đã hết",
		"digest.expiring":    "Sắp hết",
		"digest.more":        "và {count} client nữa",

		"node.down":              "\U0001F534 Node {subject} đã ngoại tuyến",
		"node.down.err":          "\U0001F534 Node {subject} đã ngoại tuyến\n{error}",
		"node.up":                "\U0001F7E2 Node {subject} đã trực tuyến trở lại",
		"node.up.latency":        "\U0001F7E2 Node {subject} đã trực tuyến trở lại ({latency} ms)",
		"core.crash":             "\U0001F525 sing-box khởi động thất bại",
		"core.crash.err":         "\U0001F525 sing-box khởi động thất bại\n{error}",
		"core.up":                "✅ sing-box đã chạy trở lại",
		"outbound.down":          "\U0001F534 Outbound {subject} không kết nối được",
		"outbound.down.err":      "\U0001F534 Outbound {subject} không kết nối được\n{error}",
		"outbound.up":            "\U0001F7E2 Outbound {subject} đã kết nối lại được",
		"outbound.up.latency":    "\U0001F7E2 Outbound {subject} đã kết nối lại được ({latency} ms)",
		"client.depleted":        "⛔ Đã vô hiệu hóa {count} client: {names}",
		"client.expiring":        "⏳ {subject} sắp hết hạn mức",
		"client.expiring.days":   "⏳ {subject} còn {days} ngày là hết hạn",
		"client.expiring.volume": "⏳ {subject} còn {volume} lưu lượng",
		"self.expiring.days":     "⏳ {name}: gói của bạn còn {days} ngày là hết hạn.",
		"self.expiring.volume":   "⏳ {name}: bạn còn {volume} lưu lượng.",
		"self.depleted":          "⛔ {name}: gói của bạn đã hết và đã bị vô hiệu hóa.",
		"cpu.high":               "\U0001F4C8 CPU {percent}% (ngưỡng {threshold}%)",
		"memory.high":            "\U0001F4C8 Bộ nhớ {percent}% (ngưỡng {threshold}%)",
		"login.success":          "\U0001F513 {user} đã đăng nhập từ {ip}",
		"login.failed.one":       "⚠️ Đăng nhập thất bại: {user} từ {ip}",
		"login.failed":           "⚠️ {count} lần đăng nhập thất bại: {user} từ {ip}",
		"login.banned":           "\U0001F6AB {ip} bị chặn {minutes} phút sau nhiều lần đăng nhập thất bại",
	},
}

// Label translates one alert-table key. The digest labels live in this table
// rather than the bot's because the scheduled report is an alert, and the two
// callers (that report and the bot's /status) must not word it differently.
func Label(lang, key string) string {
	return translate(lang, key, nil)
}

// LabelWith is Label for the few digest lines that carry a value, such as the
// count of names a truncated list dropped.
func LabelWith(lang, key string, params map[string]string) string {
	return translate(lang, key, params)
}

func translate(lang, key string, params map[string]string) string {
	return Translate(messages, lang, key, params)
}

// Translate looks key up in table[lang], falling back to English and finally to
// the key itself, then substitutes the {name} placeholders.
//
// Exported because service/tgbot keeps its own table: the bot's wording is not
// the alert wording, and neither package should carry the other's strings. What
// should not be written twice is this -- the fallback rules and the placeholder
// syntax.
//
// Falling back per key rather than per language matters: a key added in English
// but not yet translated should render in English everywhere, not turn the whole
// message into a bare identifier.
func Translate(table map[string]map[string]string, lang, key string, params map[string]string) string {
	text := ""
	if entries, ok := table[lang]; ok {
		text = entries[key]
	}
	if text == "" {
		text = table[DefaultLang][key]
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
