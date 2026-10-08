package ui

import (
	"fmt"
	"strings"
	"time"
)

type Lang int

const (
	En Lang = iota
	ZhTW
)

func DetectLang(getenv func(string) string, system string) Lang {
	for _, k := range []string{"AGENTSWAP_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		v := getenv(k)
		if v == "" || v == "C" || v == "POSIX" || strings.HasPrefix(v, "C.") {
			continue
		}
		return parseLang(v)
	}
	if system != "" {
		return parseLang(system)
	}
	return En
}

func parseLang(s string) Lang {
	if strings.HasPrefix(strings.ToLower(s), "zh") {
		return ZhTW
	}
	return En
}

var messages = map[string][2]string{
	"active":       {"● active", "● 使用中"},
	"suggest":      {"★ suggested: %s", "★ 建議：%s"},
	"unsaved":      {"not saved, run `%s`", "尚未儲存，請執行 `%s`"},
	"used":         {"%d%% used", "已用 %d%%"},
	"session":      {"Current session", "目前工作階段"},
	"week":         {"Current week", "本週額度"},
	"window":       {"Current %s window", "目前 %s視窗"},
	"days":         {"%dd", "%d 天"},
	"hours":        {"%dh", "%d 小時"},
	"minutes":      {"%dm", "%d 分鐘"},
	"fallback":     {"From local session log · %s", "依本機 session 紀錄・%s"},
	"loginExpired": {"Login expired, log in to this account again", "登入已失效，請重新登入此帳號"},
	"noUsage":      {"No usage data for API key logins", "API key 登入沒有用量資料"},
	"unavailable":  {"Usage unavailable (%s)", "無法取得用量（%s）"},
	"noData":       {"No usage data", "沒有用量資料"},
	"justNow":      {"just now", "剛剛"},
	"agoMinutes":   {"%dm ago", "%d 分鐘前"},
	"agoHours":     {"%dh ago", "%d 小時前"},
	"agoDays":      {"%dd ago", "%d 天前"},

	"saved":           {"Saved %s", "已儲存 %s"},
	"removed":         {"Removed %s", "已移除 %s"},
	"already":         {"Already using %s", "已經在使用 %s"},
	"switched":        {"Switched to %s", "已切換到 %s"},
	"hintCodex":       {"Restart Codex (CLI sessions, IDE extension, desktop app and its app-server daemon) to use the new account.", "請重新啟動 Codex（CLI、IDE 擴充功能、桌面版及其 app-server daemon）以使用新帳號。"},
	"emptyProvider":   {"No saved %s accounts. Log in with `%s`, then run `%s add`.", "尚未儲存任何 %s 帳號。請先執行 `%s` 登入，再執行 `%s add`。"},
	"emptyAll":        {"No saved accounts yet. Log in to an agent (for example `codex login`), then run `cxswap add`.", "目前沒有任何已儲存的帳號。請先登入（例如 `codex login`），再執行 `cxswap add`。"},
	"notLoggedIn":     {"Not logged in.", "尚未登入。"},
	"notSupported":    {"%s: %s is not supported yet", "%s：尚未支援 %s"},
	"unknownProvider": {"agentswap: unknown provider %q (available: codex, claude)", "agentswap：不認識的 provider %q（可用：codex、claude）"},
	"needsArgs":       {"%s needs %d argument(s)", "%s 需要 %d 個參數"},
	"unknownFlag":     {"unknown flag %s", "不認識的參數 %s"},
	"notFound":        {"no matching account: %s", "找不到符合的帳號：%s"},
	"ambiguous":       {"%s matches more than one account; be more specific", "「%s」符合多個帳號，請輸入更完整的名稱"},
	"noPrevious":      {"no previous account to switch back to", "沒有上一個帳號可以切回"},
	"noLive":          {"no live credentials found; log in first (run `%s`)", "找不到目前的登入資料，請先執行 `%s` 登入"},
	"locked":          {"another agentswap process is busy; try again", "另一個 agentswap 正在執行，請稍後再試"},
	"keyring":         {"Codex stores credentials in the OS keyring (cli_auth_credentials_store); only \"file\" is supported", "Codex 設定為把憑證存在系統鑰匙圈（cli_auth_credentials_store），目前只支援 \"file\" 模式"},
	"usage": {`Usage:
  %[1]s                    show all saved accounts with usage
  %[1]s add [alias]        save the currently logged-in account
  %[1]s <n|alias|email>    switch account (also: switch <q>)
  %[1]s -                  switch to the previous account
  %[1]s status             show the active account
  %[1]s alias <q> [name]   set or clear an alias
  %[1]s rm <q>             forget a saved account
  %[1]s version

Commands: agentswap (all agents), agentswap <codex|claude> ..., cxswap/codexswap, ccswap/claudeswap
Language: set AGENTSWAP_LANG=en or zh-TW
`, `用法：
  %[1]s                      列出所有已儲存帳號與用量
  %[1]s add [別名]           儲存目前登入的帳號
  %[1]s <編號|別名|email>    切換帳號（也可用 switch <查詢>）
  %[1]s -                    切回上一個帳號
  %[1]s status               顯示目前使用中的帳號
  %[1]s alias <查詢> [名稱]  設定或清除別名
  %[1]s rm <查詢>            移除已儲存的帳號
  %[1]s version

指令：agentswap（所有 agent）、agentswap <codex|claude> ...、cxswap/codexswap、ccswap/claudeswap
語言：可設定 AGENTSWAP_LANG=en 或 zh-TW
`},
}

func (l Lang) T(key string, args ...any) string {
	m, ok := messages[key]
	if !ok {
		return key
	}
	s := m[l]
	if s == "" {
		s = m[En]
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

func (l Lang) Ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return l.T("justNow")
	case d < time.Hour:
		return l.T("agoMinutes", int(d.Minutes()))
	case d < 48*time.Hour:
		return l.T("agoHours", int(d.Hours()))
	default:
		return l.T("agoDays", int(d.Hours()/24))
	}
}

func (l Lang) WindowName(minutes int) string {
	switch {
	case minutes > 0 && minutes <= 24*60:
		return l.T("session")
	case minutes == 7*24*60:
		return l.T("week")
	case minutes%(24*60) == 0:
		return l.T("window", l.T("days", minutes/(24*60)))
	case minutes%60 == 0:
		return l.T("window", l.T("hours", minutes/60))
	default:
		return l.T("window", l.T("minutes", minutes))
	}
}
