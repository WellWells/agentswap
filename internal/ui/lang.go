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
	"windowModel":  {"%s (%s)", "%s（%s）"},
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

	"saved":                  {"Saved %s", "已儲存 %s"},
	"loginStart":             {"Signing in from a temporary folder; the account that is signed in now stays valid.", "在暫存資料夾中登入，目前登入中的帳號不會被撤銷。"},
	"loginNoAuth":            {"login finished but produced no credentials; nothing saved", "登入結束但沒有產生憑證（no credentials），沒有儲存任何帳號"},
	"loginSaved":             {"Saved %[1]s as #%[2]d. Switch with `%[3]s %[2]d`.", "已儲存 %[1]s，編號 %[2]d。切換：`%[3]s %[2]d`"},
	"restarted":              {"Restarted the Codex app-server daemon.", "已重新啟動 Codex app-server daemon。"},
	"restartFailed":          {"Could not restart the Codex app-server daemon (%v); run `%s` yourself.", "無法重新啟動 Codex app-server daemon（%v），請自行執行 `%s`。"},
	"noExec":                 {"cannot run external commands here", "無法在這裡執行外部指令"},
	"removed":                {"Removed %s", "已移除 %s"},
	"already":                {"Already using %s", "已經在使用 %s"},
	"switched":               {"Switched to %s", "已切換到 %s"},
	"hintCodex":              {"Restart Codex CLI sessions, the IDE extension and the desktop app if they are open; they keep using the old account until restarted.", "請重新啟動 Codex 已開啟的 CLI、IDE 擴充功能與桌面版，重啟前它們仍使用舊帳號。"},
	"emptyProvider":          {"No saved %[1]s accounts. Run `%[3]s add` to save the account signed in now, or `%[3]s login` to sign in to another one.", "尚未儲存任何 %[1]s 帳號。執行 `%[3]s add` 儲存目前登入的帳號，或執行 `%[3]s login` 登入其他帳號。"},
	"emptyAll":               {"No saved accounts yet. Log in to an agent (for example `codex login`, or `/login` in Claude Code), then run `cxswap add` or `ccswap add`.", "目前沒有任何已儲存的帳號。請先登入（例如 `codex login`，或在 Claude Code 執行 `/login`），再執行 `cxswap add` 或 `ccswap add`。"},
	"emptyClaude":            {"No saved Claude Code accounts. Sign in with Claude Code (`/login`), then run `%[3]s add`; or run `%[3]s import` to import existing accounts.", "尚未儲存任何 Claude Code 帳號。請先用 Claude Code 登入（`/login`），再執行 `%[3]s add`；或執行 `%[3]s import` 匯入既有帳號。"},
	"listAddMoreClaude":      {"To add another account, sign in to it with Claude Code (`/login`), then run `%[1]s add`.", "要加入其他帳號：先用 Claude Code 登入該帳號（`/login`），再執行 `%[1]s add`。"},
	"loginOfficial":          {"sign in with the official Claude Code app (`/login`), then run `%[1]s add` to save the account", "請用官方 Claude Code 登入（`/login`），再執行 `%[1]s add` 儲存帳號"},
	"unknownCommand":         {"unknown command %s", "不認識的指令 %s"},
	"hintClaude":             {"Running Claude Code sessions pick up the new account on their next request (up to ~30s on macOS); restart them to refresh the account shown on screen.", "執行中的 Claude Code 會在下一次請求時改用新帳號（macOS 最多約 30 秒）；畫面上顯示的帳號要重新啟動才會更新。"},
	"importMenu":             {"Import accounts from:\n  1) cswap (claude-swap)  %s\nEnter a number (Enter to cancel): ", "從哪裡匯入帳號：\n  1) cswap（claude-swap）  %s\n請輸入編號（直接 Enter 取消）："},
	"importNotFound":         {"(not found)", "（找不到）"},
	"importCancelled":        {"Cancelled.", "已取消。"},
	"cswapMissing":           {"no cswap data found (looked in %s)", "找不到 cswap 的資料（找過 %s）"},
	"imported":               {"Imported %s", "已匯入 %s"},
	"importSkipped":          {"Already saved, skipped: %s", "已存在，略過：%s"},
	"importFailed":           {"Not imported: %s (%s)", "未匯入：%s（%s）"},
	"importAliasDropped":     {"Alias %q for %s is already taken; imported without it", "%[2]s 的別名 %[1]q 已被使用，改為不帶別名匯入"},
	"cswapAfter":             {"Done. Stop using cswap from now on: both tools refresh the same tokens and would invalidate each other.\nThe files in %s are only base64, not encrypted. Once ccswap works for you, run `cswap purge` to delete them.", "完成。從現在起請不要再使用 cswap：兩個工具會刷新同一組 token，互相讓對方失效。\n%s 裡的檔案只是 base64，沒有加密。確認 ccswap 正常後，請執行 `cswap purge` 刪除。"},
	"claudeUnsupportedShort": {"API key / setup-token not supported yet", "尚未支援 API key／setup-token"},
	"claudeUnsupported":      {"only Claude subscription logins are supported; API key and setup-token logins are not supported yet", "目前只支援 Claude 訂閱帳號登入，API key 與 setup-token 尚未支援"},
	"claudeBusy":             {"Claude Code is updating its login; try again in a few seconds", "Claude Code 正在更新登入資料，請幾秒後再試"},
	"claudeWiped":            {"Claude Code cleared this login after its token was rejected; sign in again with `/login`", "這個登入的 token 已被拒絕，Claude Code 已清除登入資料，請用 `/login` 重新登入"},
	"vaultKey":               {"cannot access the encryption key for saved accounts; unlock your keychain or keyring and try again (nothing was changed)", "無法取得已存帳號的加密金鑰；請先解鎖系統的鑰匙圈（Keychain／keyring）再試一次，已存資料沒有被更動"},
	"mismatch":               {"the signed-in token belongs to a different saved account than the profile shown (a running session may have rewritten it); restart Claude Code or sign in again, then retry", "目前登入的 token 屬於另一個已存帳號，和顯示的帳號資料不一致（可能是執行中的工作階段寫回了舊資料）；請重新啟動 Claude Code 或重新登入後再試"},
	"vaultCorrupt":           {"saved account data cannot be decrypted by this user on this machine; sign in and `add` it again", "已存帳號無法在這台電腦、這個使用者下解密，請重新登入並 `add`"},
	"notLoggedIn":            {"Not logged in.", "尚未登入。"},
	"notSupported":           {"%s: %s is not supported yet", "%s：尚未支援 %s"},
	"unknownProvider":        {"agentswap: unknown provider %q (available: codex, claude)", "agentswap：不認識的 provider %q（可用：codex、claude）"},
	"needsArgs":              {"%s needs %d argument(s)", "%s 需要 %d 個參數"},
	"unknownFlag":            {"unknown flag %s", "不認識的參數 %s"},
	"notFound":               {"no matching account: %s", "找不到符合的帳號：%s"},
	"noNumber":               {"no account #%s; run `%s list` to see the numbers", "沒有編號 %s 的帳號，請執行 `%s list` 查看編號"},
	"ambiguous":              {"%s matches more than one account; be more specific", "「%s」符合多個帳號，請輸入更完整的名稱"},
	"noPrevious":             {"no previous account to switch back to", "沒有上一個帳號可以切回"},
	"noLive":                 {"no live credentials found; log in first (run `%s`)", "找不到目前的登入資料，請先執行 `%s` 登入"},
	"locked":                 {"another agentswap process is busy; try again", "另一個 agentswap 正在執行，請稍後再試"},
	"noExe":                  {"cannot locate the agentswap executable", "找不到 agentswap 執行檔的位置"},
	"linked":                 {"Linked %s in %s", "已在 %[2]s 建立 %[1]s"},
	"unlinked":               {"Removed %s from %s", "已從 %[2]s 移除 %[1]s"},
	"nothingLinked":          {"No command links to remove.", "沒有可移除的指令連結。"},
	"keyring":                {"Codex stores credentials in the OS keyring (cli_auth_credentials_store); only \"file\" is supported", "Codex 設定為把憑證存在系統鑰匙圈（cli_auth_credentials_store），目前只支援 \"file\" 模式"},
	"usageAgentswap":         {"Usage: agentswap [command]\n\n  agentswap                   show usage of every saved account of every agent\n  agentswap codex <command>   same as `cxswap <command>`\n  agentswap claude <command>  same as `ccswap <command>`\n  agentswap link              create cxswap, codexswap, ccswap and claudeswap next to agentswap\n  agentswap unlink            remove those commands\n  agentswap version           show the version\n\nRun `cxswap help` or `ccswap help` for the commands of each agent.\nLanguage: set AGENTSWAP_LANG=en or zh-TW\n", "用法：agentswap [指令]\n\n  agentswap                   顯示所有 agent 已存帳號的用量\n  agentswap codex <指令>      等同 `cxswap <指令>`\n  agentswap claude <指令>     等同 `ccswap <指令>`\n  agentswap link              在 agentswap 旁建立 cxswap、codexswap、ccswap、claudeswap\n  agentswap unlink            移除上述指令\n  agentswap version           顯示版本\n\n各 agent 的指令請執行 `cxswap help` 或 `ccswap help`。\n語言：可設定 AGENTSWAP_LANG=en 或 zh-TW\n"},
	"usage":                  {"Usage: %[1]s [command]\n\n  %[1]s                         show usage of every saved account\n  %[1]s status [account]        same; give an account to show only that one\n  %[1]s list                    show account numbers, aliases and switch commands\n  %[1]s <account>               switch to an account\n  %[1]s switch <account>        same as above, spelled out\n  %[1]s -                       switch back to the previous account\n  %[1]s add [alias]             save the account that is signed in now\n  %[1]s login [alias]           sign in to another account and save it; the current one stays signed in\n  %[1]s alias <account> [name]  set an alias; leave out the name to clear it\n  %[1]s rm <account>            forget a saved account (it stays signed in)\n  %[1]s version                 show the version\n\n<account> is the number shown by `%[1]s list`, an alias or an email; a unique part of an alias or email also works.\nAfter switching, restart open Codex CLI sessions, the IDE extension and the desktop app; the app-server daemon is restarted for you.\n\nOther commands: agentswap (all agents), agentswap <codex|claude> ..., cxswap/codexswap, ccswap/claudeswap\nSetup: `agentswap link` creates those commands next to agentswap; `agentswap unlink` removes them\nLanguage: set AGENTSWAP_LANG=en or zh-TW\n", "用法：%[1]s [指令]\n\n  %[1]s                         顯示所有已存帳號的用量\n  %[1]s status [帳號]           同上；指定帳號時只顯示該帳號\n  %[1]s list                    列出帳號編號、別名與切換指令\n  %[1]s <帳號>                  切換到指定帳號\n  %[1]s switch <帳號>           同上，完整寫法\n  %[1]s -                       切回上一個帳號\n  %[1]s add [別名]              儲存目前登入的帳號\n  %[1]s login [別名]            登入其他帳號並儲存，目前的帳號維持登入\n  %[1]s alias <帳號> [別名]     設定別名；省略別名則清除\n  %[1]s rm <帳號>               移除已存帳號（不會登出該帳號）\n  %[1]s version                 顯示版本\n\n<帳號> 可以是 `%[1]s list` 顯示的編號、別名或 email；別名或 email 只要能唯一辨識，打一部分也可以。\n切換後請重新啟動已開啟的 Codex CLI、IDE 擴充功能與桌面版；app-server daemon 會自動重新啟動。\n\n其他指令：agentswap（所有 agent）、agentswap <codex|claude> ...、cxswap/codexswap、ccswap/claudeswap\n設定：`agentswap link` 會在 agentswap 旁建立上述指令，`agentswap unlink` 則移除\n語言：可設定 AGENTSWAP_LANG=en 或 zh-TW\n"},
	"usageClaude":            {"Usage: %[1]s [command]\n\n  %[1]s                         show usage of every saved account\n  %[1]s status [account]        same; give an account to show only that one\n  %[1]s list                    show account numbers, aliases and switch commands\n  %[1]s <account>               switch to an account\n  %[1]s switch <account>        same as above, spelled out\n  %[1]s -                       switch back to the previous account\n  %[1]s add [alias]             save the account that is signed in now\n  %[1]s alias <account> [name]  set an alias; leave out the name to clear it\n  %[1]s rm <account>            forget a saved account (it stays signed in)\n  %[1]s import                  import accounts from another tool\n  %[1]s version                 show the version\n\n<account> is the number shown by `%[1]s list`, an alias or an email; a unique part of an alias or email also works.\nTo add another account, sign in to it with Claude Code (`/login`), then run `%[1]s add`.\nRunning Claude Code sessions follow a switch on their next request.\n\nOther commands: agentswap (all agents), agentswap <codex|claude> ..., cxswap/codexswap, ccswap/claudeswap\nSetup: `agentswap link` creates those commands next to agentswap; `agentswap unlink` removes them\nLanguage: set AGENTSWAP_LANG=en or zh-TW\n", "用法：%[1]s [指令]\n\n  %[1]s                         顯示所有已存帳號的用量\n  %[1]s status [帳號]           同上；指定帳號時只顯示該帳號\n  %[1]s list                    列出帳號編號、別名與切換指令\n  %[1]s <帳號>                  切換到指定帳號\n  %[1]s switch <帳號>           同上，完整寫法\n  %[1]s -                       切回上一個帳號\n  %[1]s add [別名]              儲存目前登入的帳號\n  %[1]s alias <帳號> [別名]     設定別名；省略別名則清除\n  %[1]s rm <帳號>               移除已存帳號（不會登出該帳號）\n  %[1]s import                  從其他工具匯入帳號\n  %[1]s version                 顯示版本\n\n<帳號> 可以是 `%[1]s list` 顯示的編號、別名或 email；別名或 email 只要能唯一辨識，打一部分也可以。\n要加入其他帳號：先用 Claude Code 登入（`/login`），再執行 `%[1]s add`。\n切換後，執行中的 Claude Code 會在下一次請求時改用新帳號。\n\n其他指令：agentswap（所有 agent）、agentswap <codex|claude> ...、cxswap/codexswap、ccswap/claudeswap\n設定：`agentswap link` 會在 agentswap 旁建立上述指令，`agentswap unlink` 則移除\n語言：可設定 AGENTSWAP_LANG=en 或 zh-TW\n"},
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

func (l Lang) WindowTitle(minutes int, label string) string {
	name := l.WindowName(minutes)
	if label == "" {
		return name
	}
	return l.T("windowModel", name, label)
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
