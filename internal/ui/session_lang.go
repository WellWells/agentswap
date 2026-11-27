package ui

func init() {
	for k, v := range map[string][3]string{
		"sessionMissingIdentity":  {"Claude auth status did not provide a verifiable account ID or email; the session was not started", "Claude 登入狀態缺少可核對的帳號 ID 或電子郵件，已停止啟動", "Claude 登录状态缺少可核对的账号 ID 或电子邮件，已停止启动"},
		"sessionSettingsOverride": {"%s in %s overrides the isolated Claude login", "%s（%s）會覆寫隔離的 Claude 登入", "%s（%s）会覆盖隔离的 Claude 登录"},
		"sessionClaudeOnly":       {"%s is available for Claude Code only", "%s 僅支援 Claude Code", "%s 仅支持 Claude Code"},
		"sessionRunUsage":         {"use %s run [account] [-- Claude arguments]", "用法：%s run [帳號] [-- Claude 參數]", "用法：%s run [账号] [-- Claude 参数]"},
		"sessionMapUsage":         {"use %[1]s map [account [directory]] or %[1]s unmap [directory]", "用法：%[1]s map [帳號 [目錄]] 或 %[1]s unmap [目錄]", "用法：%[1]s map [账号 [目录]] 或 %[1]s unmap [目录]"},
		"sessionStaleMap":         {"the account mapped to this directory was removed; update or remove the mapping", "此目錄對應的帳號已移除，請更新或解除對應", "此目录对应的账号已移除，请更新或解除对应"},
		"sessionAuthOverride":     {"unset %s before starting an isolated Claude subscription session", "請先取消設定 %s，再啟動隔離的 Claude 訂閱帳號", "请先取消设置 %s，再启动隔离的 Claude 订阅账号"},
		"sessionAuthStatus":       {"Claude could not confirm the selected subscription login; check Claude authentication and settings", "Claude 無法確認所選的訂閱帳號，請檢查 Claude 登入狀態及設定", "Claude 无法确认所选的订阅账号，请检查 Claude 登录状态及设置"},
		"sessionOverlapsDefault":  {"the isolated Claude directory overlaps the default configuration or is not a directory", "Claude 隔離目錄與預設設定重疊，或該路徑不是目錄", "Claude 隔离目录与默认设置重叠，或该路径不是目录"},
		"sessionSignIn":           {"Sign in to %s in the Claude window for this isolated session.", "請在 Claude 登入畫面使用 %s，供此隔離終端使用。", "请在 Claude 登录页面使用 %s，供此隔离终端使用。"},
		"sessionWrongAccount":     {"the login belongs to a different account or organization; run again and sign in to %s", "登入的帳號或組織不符，請重新執行並登入 %s", "登录的账号或组织不符，请重新运行并登录 %s"},
		"sessionBare":             {"--bare disables subscription login in Claude Code", "Claude Code 的 --bare 會停用訂閱登入", "Claude Code 的 --bare 会停用订阅登录"},
		"sessionNoMaps":           {"No directory mappings.", "尚無目錄對應。", "暂无目录对应。"},
		"sessionRemoved":          {"(account removed)", "（帳號已移除）", "（账号已移除）"},
		"sessionMapMissing":       {"no mapping for %s", "%s 沒有目錄對應", "%s 没有目录对应"},
		"sessionMapped":           {"Mapped %s to %s.", "已將 %s 對應到 %s。", "已将 %s 对应到 %s。"},
		"sessionUnmapped":         {"Removed mapping for %s.", "已解除 %s 的對應。", "已解除 %s 的对应。"},
	} {
		messages[k] = v
	}
}
