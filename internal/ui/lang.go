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
	ZhCN
)

var Langs = []Lang{En, ZhTW, ZhCN}

var langInfo = [...]struct{ code, alias, name string }{
	En:   {"en", "", "English"},
	ZhTW: {"zh-TW", "zht", "正體中文"},
	ZhCN: {"zh-CN", "zhc", "简体中文"},
}

func (l Lang) Code() string  { return langInfo[l].code }
func (l Lang) Alias() string { return langInfo[l].alias }
func (l Lang) Name() string  { return langInfo[l].name }

func (l Lang) Codes() string {
	if l.Alias() == "" {
		return l.Code()
	}
	return l.Code() + "/" + l.Alias()
}

func ParseLang(s string) (Lang, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "_", "-")
	for _, l := range Langs {
		if strings.EqualFold(s, l.Code()) || (l.Alias() != "" && strings.EqualFold(s, l.Alias())) {
			return l, true
		}
	}
	return En, false
}

func LookupLang(s string) (Lang, bool) {
	if l, ok := ParseLang(s); ok {
		return l, true
	}
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "_", "-"))
	is := func(base string) bool {
		return s == base || strings.HasPrefix(s, base+"-") || strings.HasPrefix(s, base+".") || strings.HasPrefix(s, base+"@")
	}
	switch {
	case is("en"):
		return En, true
	case is("zh-cn"), is("zh-sg"), is("zh-hans"):
		return ZhCN, true
	case is("zh"):
		return ZhTW, true
	}
	return En, false
}

func DetectLang(getenv func(string) string, system func() []string) Lang {
	for _, k := range []string{"AGENTSWAP_LANG", "LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		for _, v := range strings.Split(getenv(k), ":") {
			if v == "" || v == "C" || v == "POSIX" || strings.HasPrefix(v, "C.") {
				continue
			}
			if l, ok := LookupLang(v); ok {
				return l
			}
		}
	}
	if system != nil {
		for _, v := range system() {
			if l, ok := LookupLang(v); ok {
				return l
			}
		}
	}
	return En
}

var messages = map[string][3]string{
	"active":       {"● active", "● 使用中", "● 使用中"},
	"suggest":      {"★ suggested: %s", "★ 建議：%s", "★ 建议：%s"},
	"unsaved":      {"not saved, run `%s`", "尚未儲存，請執行 `%s`", "尚未保存，请运行 `%s`"},
	"used":         {"%d%% used", "已用 %d%%", "已用 %d%%"},
	"week":         {"Weekly limit", "本週額度", "本周额度"},
	"limitDays":    {"%d-day limit", "%d 天額度", "%d 天额度"},
	"limitHours":   {"%d-hour limit", "%d 小時額度", "%d 小时额度"},
	"limitMinutes": {"%d-minute limit", "%d 分鐘額度", "%d 分钟额度"},
	"fallback":     {"From local session log · %s", "依本機 session 紀錄・%s", "根据本机 session 记录・%s"},
	"zoneNote":     {"Reset times are in %s", "重置時間為 %s", "重置时间为 %s"},
	"compactHint":  {"Reset times and details: `%s status <account>`", "重置時間與詳細資料：`%s status <帳號>`", "重置时间和详细信息：`%s status <账号>`"},
	"loginExpired": {"Login expired, log in to this account again", "登入已失效，請重新登入此帳號", "登录已失效，请重新登录此账号"},
	"noUsage":      {"No usage data for API key logins", "API key 登入沒有用量資料", "API key 登录没有用量数据"},
	"unavailable":  {"Usage unavailable (%s)", "無法取得用量（%s）", "无法获取用量（%s）"},
	"noData":       {"No usage data", "沒有用量資料", "没有用量数据"},
	"windowModel":  {"%s (%s)", "%s（%s）", "%s（%s）"},
	"justNow":      {"just now", "剛剛", "刚刚"},
	"agoMinutes":   {"%dm ago", "%d 分鐘前", "%d 分钟前"},
	"agoHours":     {"%dh ago", "%d 小時前", "%d 小时前"},
	"agoDays":      {"%dd ago", "%d 天前", "%d 天前"},

	"listTitle":  {"%s accounts", "%s 帳號", "%s 账号"},
	"colNumber":  {"#", "編號", "编号"},
	"colAccount": {"Account", "帳號", "账号"},
	"colPlan":    {"Plan", "方案", "套餐"},
	"colSwitch":  {"Switch with", "切換指令", "切换命令"},
	"listHelp": {"Switch with `%[1]s <number>`. Numbers follow the order accounts were added and shift up when one is removed.\nYou can also use an alias or email (`%[1]s <alias|email>`); `%[1]s -` switches back to the previous account.",
		"切換帳號：`%[1]s <編號>`。編號依加入順序排列，移除帳號後，後面的編號會往前遞補。\n也可以用別名或 email 切換（`%[1]s <別名|email>`）；`%[1]s -` 切回上一個帳號。", "切换账号：`%[1]s <编号>`。编号按添加顺序排列，删除账号后，后面的编号会依次前移。\n也可以用别名或 email 切换（`%[1]s <别名|email>`）；`%[1]s -` 切回上一个账号。"},
	"listAddMore": {"To add another account, run `%[1]s login`. Do not run `%[2]s` directly: it revokes the account that is currently signed in.", "要加入其他帳號：執行 `%[1]s login`。不要直接執行 `%[2]s`，它會撤銷目前登入中帳號的 token。", "要添加其他账号：运行 `%[1]s login`。不要直接运行 `%[2]s`，它会撤销当前登录账号的 token。"},
	"usageHint":   {"Switch with `%[1]s <number>`; run `%[1]s list` to see which number is which account.", "切換帳號：`%[1]s <編號>`；執行 `%[1]s list` 查看編號對應的帳號。", "切换账号：`%[1]s <编号>`；运行 `%[1]s list` 查看编号对应的账号。"},

	"saved":                  {"Saved %s", "已儲存 %s", "已保存 %s"},
	"loginStart":             {"Signing in from a temporary folder; the account that is signed in now stays valid.", "在暫存資料夾中登入，目前登入中的帳號不會被撤銷。", "在临时文件夹中登录，当前登录的账号不会被撤销。"},
	"loginNoAuth":            {"login finished but produced no credentials; nothing saved", "登入結束但沒有產生憑證（no credentials），沒有儲存任何帳號", "登录结束但没有生成凭证（no credentials），没有保存任何账号"},
	"loginSaved":             {"Saved %[1]s as #%[2]d. Switch with `%[3]s %[2]d`.", "已儲存 %[1]s，編號 %[2]d。切換：`%[3]s %[2]d`", "已保存 %[1]s，编号 %[2]d。切换：`%[3]s %[2]d`"},
	"restarted":              {"Restarted the Codex app-server daemon.", "已重新啟動 Codex app-server daemon。", "已重启 Codex app-server daemon。"},
	"restartFailed":          {"Could not restart the Codex app-server daemon (%v); run `%s` yourself.", "無法重新啟動 Codex app-server daemon（%v），請自行執行 `%s`。", "无法重启 Codex app-server daemon（%v），请自行运行 `%s`。"},
	"noExec":                 {"cannot run external commands here", "無法在這裡執行外部指令", "无法在这里运行外部命令"},
	"removed":                {"Removed %s", "已移除 %s", "已移除 %s"},
	"already":                {"Already using %s", "已經在使用 %s", "已经在使用 %s"},
	"switched":               {"Switched to %s", "已切換到 %s", "已切换到 %s"},
	"hintCodex":              {"Restart Codex CLI sessions, the IDE extension and the desktop app if they are open; they keep using the old account until restarted.", "請重新啟動 Codex 已開啟的 CLI、IDE 擴充功能與桌面版，重啟前它們仍使用舊帳號。", "请重启已打开的 Codex CLI、IDE 扩展和桌面版，重启前它们仍使用旧账号。"},
	"emptyProvider":          {"No saved %[1]s accounts. Run `%[3]s add` to save the account signed in now, or `%[3]s login` to sign in to another one.", "尚未儲存任何 %[1]s 帳號。執行 `%[3]s add` 儲存目前登入的帳號，或執行 `%[3]s login` 登入其他帳號。", "尚未保存任何 %[1]s 账号。运行 `%[3]s add` 保存当前登录的账号，或运行 `%[3]s login` 登录其他账号。"},
	"version":                {"agentswap %s\n\n  Source  https://github.com/WellWells/agentswap\n  Author  WellsTsai · https://wellstsai.com\n", "agentswap %s\n\n  原始碼  https://github.com/WellWells/agentswap\n  作者    WellsTsai · https://wellstsai.com\n", "agentswap %s\n\n  源代码  https://github.com/WellWells/agentswap\n  作者    WellsTsai · https://wellstsai.com\n"},
	"emptyAll":               {"No saved accounts yet. Log in to an agent (for example `codex login`, or `/login` in Claude Code), then run `cxswap add` or `ccswap add`.", "目前沒有任何已儲存的帳號。請先登入（例如 `codex login`，或在 Claude Code 執行 `/login`），再執行 `cxswap add` 或 `ccswap add`。", "目前没有任何已保存的账号。请先登录（例如 `codex login`，或在 Claude Code 中运行 `/login`），再运行 `cxswap add` 或 `ccswap add`。"},
	"emptyClaude":            {"No saved Claude Code accounts. Sign in with Claude Code (`/login`), then run `%[3]s add`; or run `%[3]s import` to import existing accounts.", "尚未儲存任何 Claude Code 帳號。請先用 Claude Code 登入（`/login`），再執行 `%[3]s add`；或執行 `%[3]s import` 匯入既有帳號。", "尚未保存任何 Claude Code 账号。请先用 Claude Code 登录（`/login`），再运行 `%[3]s add`；或运行 `%[3]s import` 导入已有账号。"},
	"listAddMoreClaude":      {"To add another account, sign in to it with Claude Code (`/login`), then run `%[1]s add`.", "要加入其他帳號：先用 Claude Code 登入該帳號（`/login`），再執行 `%[1]s add`。", "要添加其他账号：先用 Claude Code 登录该账号（`/login`），再运行 `%[1]s add`。"},
	"loginOfficial":          {"sign in with the official Claude Code app (`/login`), then run `%[1]s add` to save the account", "請用官方 Claude Code 登入（`/login`），再執行 `%[1]s add` 儲存帳號", "请用官方 Claude Code 登录（`/login`），再运行 `%[1]s add` 保存账号"},
	"unknownCommand":         {"unknown command %s", "不認識的指令 %s", "未知命令 %s"},
	"hintClaude":             {"Running Claude Code sessions pick up the new account on their next request (up to ~30s on macOS); restart them to refresh the account shown on screen.", "執行中的 Claude Code 會在下一次請求時改用新帳號（macOS 最多約 30 秒）；畫面上顯示的帳號要重新啟動才會更新。", "运行中的 Claude Code 会在下一次请求时改用新账号（macOS 最多约 30 秒）；界面上显示的账号需要重启才会更新。"},
	"importMenu":             {"Import accounts from:\n  1) cswap (claude-swap)  %s\nEnter a number (Enter to cancel): ", "從哪裡匯入帳號：\n  1) cswap（claude-swap）  %s\n請輸入編號（直接 Enter 取消）：", "从哪里导入账号：\n  1) cswap（claude-swap）  %s\n请输入编号（直接按 Enter 取消）："},
	"importNotFound":         {"(not found)", "（找不到）", "（未找到）"},
	"importCancelled":        {"Cancelled.", "已取消。", "已取消。"},
	"cswapMissing":           {"no cswap data found (looked in %s)", "找不到 cswap 的資料（找過 %s）", "未找到 cswap 的数据（已查找 %s）"},
	"imported":               {"Imported %s", "已匯入 %s", "已导入 %s"},
	"importSkipped":          {"Already saved, skipped: %s", "已存在，略過：%s", "已存在，跳过：%s"},
	"importFailed":           {"Not imported: %s (%s)", "未匯入：%s（%s）", "未导入：%s（%s）"},
	"importAliasDropped":     {"Alias %q for %s is already taken; imported without it", "%[2]s 的別名 %[1]q 已被使用，改為不帶別名匯入", "%[2]s 的别名 %[1]q 已被占用，改为不带别名导入"},
	"cswapAfter":             {"Done. Stop using cswap from now on: both tools refresh the same tokens and would invalidate each other.\nThe files in %s are only base64, not encrypted. Once ccswap works for you, run `cswap purge` to delete them.", "完成。從現在起請不要再使用 cswap：兩個工具會刷新同一組 token，互相讓對方失效。\n%s 裡的檔案只是 base64，沒有加密。確認 ccswap 正常後，請執行 `cswap purge` 刪除。", "完成。从现在起请不要再使用 cswap：两个工具会刷新同一组 token，导致对方失效。\n%s 里的文件只是 base64，并未加密。确认 ccswap 正常后，请运行 `cswap purge` 删除。"},
	"claudeUnsupportedShort": {"API key / setup-token not supported yet", "尚未支援 API key／setup-token", "暂不支持 API key／setup-token"},
	"claudeUnsupported":      {"only Claude subscription logins are supported; API key and setup-token logins are not supported yet", "目前只支援 Claude 訂閱帳號登入，API key 與 setup-token 尚未支援", "目前只支持 Claude 订阅账号登录，暂不支持 API key 与 setup-token"},
	"claudeBusy":             {"Claude Code is updating its login; try again in a few seconds", "Claude Code 正在更新登入資料，請幾秒後再試", "Claude Code 正在更新登录数据，请几秒后再试"},
	"claudeWiped":            {"Claude Code cleared this login after its token was rejected; sign in again with `/login`", "這個登入的 token 已被拒絕，Claude Code 已清除登入資料，請用 `/login` 重新登入", "这个登录的 token 已被拒绝，Claude Code 已清除登录数据，请用 `/login` 重新登录"},
	"vaultKey":               {"cannot access the encryption key for saved accounts; unlock your keychain or keyring and try again (nothing was changed)", "無法取得已存帳號的加密金鑰；請先解鎖系統的鑰匙圈（Keychain／keyring）再試一次，已存資料沒有被更動", "无法获取已保存账号的加密密钥；请先解锁系统钥匙串（Keychain／keyring）再试一次，已保存的数据没有被改动"},
	"mismatch":               {"the signed-in token belongs to a different saved account than the profile shown (a running session may have rewritten it); restart Claude Code or sign in again, then retry", "目前登入的 token 屬於另一個已存帳號，和顯示的帳號資料不一致（可能是執行中的工作階段寫回了舊資料）；請重新啟動 Claude Code 或重新登入後再試", "当前登录的 token 属于另一个已保存账号，与显示的账号信息不一致（可能是运行中的会话写回了旧数据）；请重启 Claude Code 或重新登录后再试"},
	"vaultCorrupt":           {"saved account data cannot be decrypted by this user on this machine; sign in and `add` it again", "已存帳號無法在這台電腦、這個使用者下解密，請重新登入並 `add`", "已保存的账号无法在这台电脑、这个用户下解密，请重新登录并 `add`"},
	"notSupported":           {"%s: %s is not supported yet", "%s：尚未支援 %s", "%s：暂不支持 %s"},
	"unknownProvider":        {"agentswap: unknown provider %q (available: codex, claude)", "agentswap：不認識的 provider %q（可用：codex、claude）", "agentswap：未知的 provider %q（可用：codex、claude）"},
	"needsArgs":              {"%s needs %d argument(s)", "%s 需要 %d 個參數", "%s 需要 %d 个参数"},
	"unknownFlag":            {"unknown flag %s", "不認識的參數 %s", "未知参数 %s"},
	"notFound":               {"no matching account: %s", "找不到符合的帳號：%s", "找不到匹配的账号：%s"},
	"noNumber":               {"no account #%s; run `%s list` to see the numbers", "沒有編號 %s 的帳號，請執行 `%s list` 查看編號", "没有编号 %s 的账号，请运行 `%s list` 查看编号"},
	"ambiguous":              {"%s matches more than one account; be more specific", "「%s」符合多個帳號，請輸入更完整的名稱", "“%s”匹配多个账号，请输入更完整的名称"},
	"noPrevious":             {"no previous account to switch back to", "沒有上一個帳號可以切回", "没有上一个账号可以切回"},
	"noLive":                 {"no live credentials found; log in first (run `%s`)", "找不到目前的登入資料，請先執行 `%s` 登入", "找不到当前的登录数据，请先运行 `%s` 登录"},
	"locked":                 {"another agentswap process is busy; try again", "另一個 agentswap 正在執行，請稍後再試", "另一个 agentswap 正在运行，请稍后再试"},
	"noExe":                  {"cannot locate the agentswap executable", "找不到 agentswap 執行檔的位置", "找不到 agentswap 可执行文件的位置"},
	"linked":                 {"Linked %s in %s", "已在 %[2]s 建立 %[1]s", "已在 %[2]s 创建 %[1]s"},
	"unlinked":               {"Removed %s from %s", "已從 %[2]s 移除 %[1]s", "已从 %[2]s 移除 %[1]s"},
	"nothingLinked":          {"No command links to remove.", "沒有可移除的指令連結。", "没有可移除的命令链接。"},
	"langCurrent":            {"Language: %s (%s), %s", "介面語言：%s（%s），%s", "界面语言：%s（%s），%s"},
	"langFromEnv":            {"set by AGENTSWAP_LANG", "由 AGENTSWAP_LANG 指定", "由 AGENTSWAP_LANG 指定"},
	"langFromConfig":         {"set with `agentswap lang`", "由 `agentswap lang` 設定", "由 `agentswap lang` 设置"},
	"langFromSystem":         {"follows the system locale", "跟隨系統語系", "跟随系统语言"},
	"langHelp":               {"Change with `agentswap lang <number|code>`; `agentswap lang auto` follows the system locale again.", "變更：`agentswap lang <編號|代碼>`；`agentswap lang auto` 恢復跟隨系統語系。", "更改：`agentswap lang <编号|代码>`；`agentswap lang auto` 恢复跟随系统语言。"},
	"langSet":                {"Language set to %s (%s).", "介面語言已設為 %s（%s）。", "界面语言已设置为 %s（%s）。"},
	"langAuto":               {"Language follows the system locale again: %s (%s).", "介面語言恢復跟隨系統語系：%s（%s）。", "界面语言恢复跟随系统语言：%s（%s）。"},
	"langEnvOverride":        {"AGENTSWAP_LANG=%s is set and takes precedence; unset it for this setting to apply.", "目前設定了 AGENTSWAP_LANG=%s，它的優先順序較高；要套用這個設定，請先移除該環境變數。", "当前设置了 AGENTSWAP_LANG=%s，它的优先级更高；要使用此设置，请先删除该环境变量。"},
	"langUnknown":            {"unknown language %q; run `agentswap lang` to see the numbers and codes", "不認識的語言 %q，請執行 `agentswap lang` 查看編號與代碼", "未知的语言 %q，请运行 `agentswap lang` 查看编号和代码"},
	"keyring":                {"Codex stores credentials in the OS keyring (cli_auth_credentials_store); only \"file\" is supported", "Codex 設定為把憑證存在系統鑰匙圈（cli_auth_credentials_store），目前只支援 \"file\" 模式", "Codex 设置为把凭证存放在系统钥匙串（cli_auth_credentials_store），目前只支持 \"file\" 模式"},
	"usageAgentswap":         {"Usage: agentswap [command]\n\n  agentswap status            show usage of every saved account of every agent\n  agentswap codex <command>   same as `cxswap <command>`\n  agentswap claude <command>  same as `ccswap <command>`\n  agentswap lang [language]   list the languages, or set one by number or code\n  agentswap link              create cxswap, codexswap, ccswap and claudeswap next to agentswap\n  agentswap unlink            remove those commands\n  agentswap version           show the version\n\nRun `cxswap help` or `ccswap help` for the commands of each agent.\n", "用法：agentswap [指令]\n\n  agentswap status            顯示所有 agent 已存帳號的用量\n  agentswap codex <指令>      等同 `cxswap <指令>`\n  agentswap claude <指令>     等同 `ccswap <指令>`\n  agentswap lang [語言]       列出可用語言，或以編號、代碼設定\n  agentswap link              在 agentswap 旁建立 cxswap、codexswap、ccswap、claudeswap\n  agentswap unlink            移除上述指令\n  agentswap version           顯示版本\n\n各 agent 的指令請執行 `cxswap help` 或 `ccswap help`。\n", "用法：agentswap [命令]\n\n  agentswap status            显示所有 agent 已保存账号的用量\n  agentswap codex <命令>      等同 `cxswap <命令>`\n  agentswap claude <命令>     等同 `ccswap <命令>`\n  agentswap lang [语言]       列出可用语言，或以编号、代码设置\n  agentswap link              在 agentswap 旁创建 cxswap、codexswap、ccswap、claudeswap\n  agentswap unlink            移除上述命令\n  agentswap version           显示版本\n\n各 agent 的命令请运行 `cxswap help` 或 `ccswap help`。\n"},
	"usage":                  {"Usage: %[1]s [command]\n\n  %[1]s status [account]        show usage of every saved account; give an account to show only that one\n  %[1]s list                    show account numbers, aliases and switch commands\n  %[1]s <account>               switch to an account\n  %[1]s switch <account>        same as above, spelled out\n  %[1]s -                       switch back to the previous account\n  %[1]s add [alias]             save the account that is signed in now\n  %[1]s login [alias]           sign in to another account and save it; the current one stays signed in\n  %[1]s alias <account> [name]  set an alias; leave out the name to clear it\n  %[1]s rm <account>            forget a saved account (it stays signed in)\n  %[1]s version                 show the version\n\n<account> is the number shown by `%[1]s list`, an alias or an email; a unique part of an alias or email also works.\nAfter switching, restart open Codex CLI sessions, the IDE extension and the desktop app; the app-server daemon is restarted for you.\n\nOther commands: agentswap status (all agents), agentswap <codex|claude> ..., cxswap/codexswap, ccswap/claudeswap\nSetup: `agentswap link` creates those commands next to agentswap; `agentswap unlink` removes them\nLanguage: `agentswap lang` lists the languages and sets one\n", "用法：%[1]s [指令]\n\n  %[1]s status [帳號]           顯示所有已存帳號的用量；指定帳號時只顯示該帳號\n  %[1]s list                    列出帳號編號、別名與切換指令\n  %[1]s <帳號>                  切換到指定帳號\n  %[1]s switch <帳號>           同上，完整寫法\n  %[1]s -                       切回上一個帳號\n  %[1]s add [別名]              儲存目前登入的帳號\n  %[1]s login [別名]            登入其他帳號並儲存，目前的帳號維持登入\n  %[1]s alias <帳號> [別名]     設定別名；省略別名則清除\n  %[1]s rm <帳號>               移除已存帳號（不會登出該帳號）\n  %[1]s version                 顯示版本\n\n<帳號> 可以是 `%[1]s list` 顯示的編號、別名或 email；別名或 email 只要能唯一辨識，打一部分也可以。\n切換後請重新啟動已開啟的 Codex CLI、IDE 擴充功能與桌面版；app-server daemon 會自動重新啟動。\n\n其他指令：agentswap status（所有 agent）、agentswap <codex|claude> ...、cxswap/codexswap、ccswap/claudeswap\n設定：`agentswap link` 會在 agentswap 旁建立上述指令，`agentswap unlink` 則移除\n語言：`agentswap lang` 列出可用語言並設定\n", "用法：%[1]s [命令]\n\n  %[1]s status [账号]           显示所有已保存账号的用量；指定账号时只显示该账号\n  %[1]s list                    列出账号编号、别名和切换命令\n  %[1]s <账号>                  切换到指定账号\n  %[1]s switch <账号>           同上，完整写法\n  %[1]s -                       切回上一个账号\n  %[1]s add [别名]              保存当前登录的账号\n  %[1]s login [别名]            登录其他账号并保存，当前账号保持登录\n  %[1]s alias <账号> [别名]     设置别名；省略别名则清除\n  %[1]s rm <账号>               移除已保存的账号（不会退出该账号的登录）\n  %[1]s version                 显示版本\n\n<账号> 可以是 `%[1]s list` 显示的编号、别名或 email；别名或 email 只要能唯一识别，输入一部分也可以。\n切换后请重启已打开的 Codex CLI、IDE 扩展和桌面版；app-server daemon 会自动重启。\n\n其他命令：agentswap status（所有 agent）、agentswap <codex|claude> ...、cxswap/codexswap、ccswap/claudeswap\n设置：`agentswap link` 会在 agentswap 旁创建上述命令，`agentswap unlink` 则移除\n语言：`agentswap lang` 列出可用语言并设置\n"},
	"usageClaude":            {"Usage: %[1]s [command]\n\n  %[1]s status [account]        show usage of every saved account; give an account to show only that one\n  %[1]s list                    show account numbers, aliases and switch commands\n  %[1]s <account>               switch to an account\n  %[1]s switch <account>        same as above, spelled out\n  %[1]s -                       switch back to the previous account\n  %[1]s add [alias]             save the account that is signed in now\n  %[1]s alias <account> [name]  set an alias; leave out the name to clear it\n  %[1]s rm <account>            forget a saved account (it stays signed in)\n  %[1]s import                  import accounts from another tool\n  %[1]s version                 show the version\n\n<account> is the number shown by `%[1]s list`, an alias or an email; a unique part of an alias or email also works.\nTo add another account, sign in to it with Claude Code (`/login`), then run `%[1]s add`.\nRunning Claude Code sessions follow a switch on their next request.\n\nOther commands: agentswap status (all agents), agentswap <codex|claude> ..., cxswap/codexswap, ccswap/claudeswap\nSetup: `agentswap link` creates those commands next to agentswap; `agentswap unlink` removes them\nLanguage: `agentswap lang` lists the languages and sets one\n", "用法：%[1]s [指令]\n\n  %[1]s status [帳號]           顯示所有已存帳號的用量；指定帳號時只顯示該帳號\n  %[1]s list                    列出帳號編號、別名與切換指令\n  %[1]s <帳號>                  切換到指定帳號\n  %[1]s switch <帳號>           同上，完整寫法\n  %[1]s -                       切回上一個帳號\n  %[1]s add [別名]              儲存目前登入的帳號\n  %[1]s alias <帳號> [別名]     設定別名；省略別名則清除\n  %[1]s rm <帳號>               移除已存帳號（不會登出該帳號）\n  %[1]s import                  從其他工具匯入帳號\n  %[1]s version                 顯示版本\n\n<帳號> 可以是 `%[1]s list` 顯示的編號、別名或 email；別名或 email 只要能唯一辨識，打一部分也可以。\n要加入其他帳號：先用 Claude Code 登入（`/login`），再執行 `%[1]s add`。\n切換後，執行中的 Claude Code 會在下一次請求時改用新帳號。\n\n其他指令：agentswap status（所有 agent）、agentswap <codex|claude> ...、cxswap/codexswap、ccswap/claudeswap\n設定：`agentswap link` 會在 agentswap 旁建立上述指令，`agentswap unlink` 則移除\n語言：`agentswap lang` 列出可用語言並設定\n", "用法：%[1]s [命令]\n\n  %[1]s status [账号]           显示所有已保存账号的用量；指定账号时只显示该账号\n  %[1]s list                    列出账号编号、别名和切换命令\n  %[1]s <账号>                  切换到指定账号\n  %[1]s switch <账号>           同上，完整写法\n  %[1]s -                       切回上一个账号\n  %[1]s add [别名]              保存当前登录的账号\n  %[1]s alias <账号> [别名]     设置别名；省略别名则清除\n  %[1]s rm <账号>               移除已保存的账号（不会退出该账号的登录）\n  %[1]s import                  从其他工具导入账号\n  %[1]s version                 显示版本\n\n<账号> 可以是 `%[1]s list` 显示的编号、别名或 email；别名或 email 只要能唯一识别，输入一部分也可以。\n要添加其他账号：先用 Claude Code 登录（`/login`），再运行 `%[1]s add`。\n切换后，运行中的 Claude Code 会在下一次请求时改用新账号。\n\n其他命令：agentswap status（所有 agent）、agentswap <codex|claude> ...、cxswap/codexswap、ccswap/claudeswap\n设置：`agentswap link` 会在 agentswap 旁创建上述命令，`agentswap unlink` 则移除\n语言：`agentswap lang` 列出可用语言并设置\n"},
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
