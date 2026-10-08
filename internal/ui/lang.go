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
	"week":         {"Weekly limit", "本週額度"},
	"limitDays":    {"%d-day limit", "%d 天額度"},
	"limitHours":   {"%d-hour limit", "%d 小時額度"},
	"limitMinutes": {"%d-minute limit", "%d 分鐘額度"},
	"fallback":     {"From local session log · %s", "依本機 session 紀錄・%s"},
	"loginExpired": {"Login expired, log in to this account again", "登入已失效，請重新登入此帳號"},
	"noUsage":      {"No usage data for API key logins", "API key 登入沒有用量資料"},
	"unavailable":  {"Usage unavailable (%s)", "無法取得用量（%s）"},
	"noData":       {"No usage data", "沒有用量資料"},
	"justNow":      {"just now", "剛剛"},
	"agoMinutes":   {"%dm ago", "%d 分鐘前"},
	"agoHours":     {"%dh ago", "%d 小時前"},
	"agoDays":      {"%dd ago", "%d 天前"},

	"listTitle":  {"%s accounts", "%s 帳號"},
	"colNumber":  {"#", "編號"},
	"colAccount": {"Account", "帳號"},
	"colPlan":    {"Plan", "方案"},
	"colSwitch":  {"Switch with", "切換指令"},
	"listHelp": {"Switch with `%[1]s <number>`. Numbers follow the order accounts were added and shift up when one is removed.\nYou can also use an alias or email (`%[1]s <alias|email>`); `%[1]s -` switches back to the previous account.",
		"切換帳號：`%[1]s <編號>`。編號依加入順序排列，移除帳號後，後面的編號會往前遞補。\n也可以用別名或 email 切換（`%[1]s <別名|email>`）；`%[1]s -` 切回上一個帳號。"},
	"listAddMore": {"To add another account, run `%[1]s login`. Do not run `%[2]s` directly: it revokes the account that is currently signed in.", "要加入其他帳號：執行 `%[1]s login`。不要直接執行 `%[2]s`，它會撤銷目前登入中帳號的 token。"},
	"usageHint":   {"Switch with `%[1]s <number>`; run `%[1]s list` to see which number is which account.", "切換帳號：`%[1]s <編號>`；執行 `%[1]s list` 查看編號對應的帳號。"},

	"saved":           {"Saved %s", "已儲存 %s"},
	"loginStart":      {"Signing in from a temporary folder; the account that is signed in now stays valid.", "在暫存資料夾中登入，目前登入中的帳號不會被撤銷。"},
	"loginNoAuth":     {"login finished but produced no credentials; nothing saved", "登入結束但沒有產生憑證（no credentials），沒有儲存任何帳號"},
	"loginSaved":      {"Saved %[1]s as #%[2]d. Switch with `%[3]s %[2]d`.", "已儲存 %[1]s，編號 %[2]d。切換：`%[3]s %[2]d`"},
	"restarted":       {"Restarted the Codex app-server daemon.", "已重新啟動 Codex app-server daemon。"},
	"restartFailed":   {"Could not restart the Codex app-server daemon (%v); run `%s` yourself.", "無法重新啟動 Codex app-server daemon（%v），請自行執行 `%s`。"},
	"noExec":          {"cannot run external commands here", "無法在這裡執行外部指令"},
	"removed":         {"Removed %s", "已移除 %s"},
	"already":         {"Already using %s", "已經在使用 %s"},
	"switched":        {"Switched to %s", "已切換到 %s"},
	"hintCodex":       {"Restart Codex CLI sessions, the IDE extension and the desktop app if they are open; they keep using the old account until restarted.", "請重新啟動 Codex 已開啟的 CLI、IDE 擴充功能與桌面版，重啟前它們仍使用舊帳號。"},
	"emptyProvider":   {"No saved %[1]s accounts. Run `%[3]s add` to save the account signed in now, or `%[3]s login` to sign in to another one.", "尚未儲存任何 %[1]s 帳號。執行 `%[3]s add` 儲存目前登入的帳號，或執行 `%[3]s login` 登入其他帳號。"},
	"emptyAll":        {"No saved accounts yet. Log in to an agent (for example `codex login`), then run `cxswap add`.", "目前沒有任何已儲存的帳號。請先登入（例如 `codex login`），再執行 `cxswap add`。"},
	"notLoggedIn":     {"Not logged in.", "尚未登入。"},
	"notSupported":    {"%s: %s is not supported yet", "%s：尚未支援 %s"},
	"unknownProvider": {"agentswap: unknown provider %q (available: codex, claude)", "agentswap：不認識的 provider %q（可用：codex、claude）"},
	"needsArgs":       {"%s needs %d argument(s)", "%s 需要 %d 個參數"},
	"unknownFlag":     {"unknown flag %s", "不認識的參數 %s"},
	"notFound":        {"no matching account: %s", "找不到符合的帳號：%s"},
	"noNumber":        {"no account #%s; run `%s list` to see the numbers", "沒有編號 %s 的帳號，請執行 `%s list` 查看編號"},
	"ambiguous":       {"%s matches more than one account; be more specific", "「%s」符合多個帳號，請輸入更完整的名稱"},
	"noPrevious":      {"no previous account to switch back to", "沒有上一個帳號可以切回"},
	"noLive":          {"no live credentials found; log in first (run `%s`)", "找不到目前的登入資料，請先執行 `%s` 登入"},
	"locked":          {"another agentswap process is busy; try again", "另一個 agentswap 正在執行，請稍後再試"},
	"noExe":           {"cannot locate the agentswap executable", "找不到 agentswap 執行檔的位置"},
	"linked":          {"Linked %s in %s", "已在 %[2]s 建立 %[1]s"},
	"unlinked":        {"Removed %s from %s", "已從 %[2]s 移除 %[1]s"},
	"nothingLinked":   {"No command links to remove.", "沒有可移除的指令連結。"},
	"keyring":         {"Codex stores credentials in the OS keyring (cli_auth_credentials_store); only \"file\" is supported", "Codex 設定為把憑證存在系統鑰匙圈（cli_auth_credentials_store），目前只支援 \"file\" 模式"},
	"usage": {`Usage:
  %[1]s                    show usage for all saved accounts
  %[1]s list               list account numbers and switch commands
  %[1]s <n|alias|email>    switch account (n is the number shown by list)
  %[1]s switch <q>         same as above, spelled out
  %[1]s add [alias]        save the currently logged-in account
  %[1]s login [alias]      sign in to another account and save it
  %[1]s -                  switch to the previous account
  %[1]s status             same as above: status and usage of every account
  %[1]s alias <q> [name]   set or clear an alias
  %[1]s rm <q>             forget a saved account
  %[1]s version

Commands: agentswap (all agents), agentswap <codex|claude> ..., cxswap/codexswap, ccswap/claudeswap
Setup: agentswap link creates those commands next to agentswap; agentswap unlink removes them
Language: set AGENTSWAP_LANG=en or zh-TW
`, `用法：
  %[1]s                      顯示所有已儲存帳號的用量
  %[1]s list                 列出帳號編號與切換指令
  %[1]s <編號|別名|email>    切換帳號（編號就是 list 顯示的編號）
  %[1]s switch <查詢>        同上，完整寫法
  %[1]s add [別名]           儲存目前登入的帳號
  %[1]s login [別名]         登入其他帳號並儲存（不影響目前帳號）
  %[1]s -                    切回上一個帳號
  %[1]s status               同上：所有帳號的狀態與用量
  %[1]s alias <查詢> [名稱]  設定或清除別名
  %[1]s rm <查詢>            移除已儲存的帳號
  %[1]s version

指令：agentswap（所有 agent）、agentswap <codex|claude> ...、cxswap/codexswap、ccswap/claudeswap
設定：agentswap link 會在 agentswap 旁建立上述指令，agentswap unlink 則移除
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
	case minutes == 7*24*60:
		return l.T("week")
	case minutes > 0 && minutes%(24*60) == 0:
		return l.T("limitDays", minutes/(24*60))
	case minutes > 0 && minutes%60 == 0:
		return l.T("limitHours", minutes/60)
	default:
		return l.T("limitMinutes", minutes)
	}
}
