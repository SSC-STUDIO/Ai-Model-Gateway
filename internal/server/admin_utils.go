package server

import (
	"strings"

	"ai-model-gateway/internal/config"
)

// adminHTMLLang returns the HTML lang attribute value based on the admin language setting
func adminHTMLLang(language string) string {
	switch config.NormalizeAdminLanguage(language) {
	case config.AdminLanguageEnglish:
		return "en"
	case config.AdminLanguageJapanese:
		return "ja"
	case config.AdminLanguageKorean:
		return "ko"
	case config.AdminLanguageSpanish:
		return "es"
	case config.AdminLanguageFrench:
		return "fr"
	case config.AdminLanguageGerman:
		return "de"
	}
	return "zh-CN"
}

// renderAdminHTML renders the admin HTML page with the given settings view flag and language
func renderAdminHTML(settingsView bool, language string) string {
	language = config.NormalizeAdminLanguage(language)
	useChinese := language == config.AdminLanguageChinese
	pick := func(zh, en string) string {
		if useChinese {
			return zh
		}
		return en
	}
	bodyClass := ""
	topnavLinks := strings.Join([]string{
		`<a href="#performance" data-topnav-target="performance">` + pick("性能", "Performance") + `</a>`,
		`<a href="#economics" data-topnav-target="economics">` + pick("成本", "Economics") + `</a>`,
		`<a href="#upstreams-card" data-topnav-target="upstreams-card">` + pick("上游", "Upstreams") + `</a>`,
		`<a href="#requests-card" data-topnav-target="requests-card">` + pick("请求", "Requests") + `</a>`,
		`<a href="/admin/settings">` + pick("设置", "Settings") + `</a>`,
	}, "")
	heroEyebrow := pick("AI 模型网关管理台", "AI Gateway Admin")
	heroTitle := pick("运维、成本、吞吐。", "Ops, Cost, Throughput.")
	heroSub := pick("先看吞吐、延迟、错误和上游健康，再往下追成本与缓存。", "Check throughput, latency, errors, and upstream health first, then drill into cost and cache.")
	heroMetaPrimary := `<div class="pill" id="generatedAt">` + pick("加载中", "Loading") + `</div>`
	heroMetaSecondary := `<div class="pill" id="pricingSource">` + pick("价格来源", "Pricing source") + `</div>`
	heroMetaTertiary := ``
	routerStrategyHealthWeightedRR := pick("健康加权轮询", "Health-Weighted Round Robin")
	routerStrategyRoundRobin := pick("轮询", "Round Robin")
	if settingsView {
		bodyClass = "page-settings"
		topnavLinks = strings.Join([]string{
			`<a href="/admin">` + pick("总览", "Overview") + `</a>`,
			`<a href="#cfg-health" data-topnav-target="cfg-health">` + pick("探活", "Health") + `</a>`,
			`<a href="#cfg-bridge" data-topnav-target="cfg-bridge">` + pick("桥接", "Bridge") + `</a>`,
			`<a href="#cfg-router" data-topnav-target="cfg-router">` + pick("路由", "Router") + `</a>`,
			`<a href="#cfg-intercepts" data-topnav-target="cfg-intercepts">` + pick("拦截", "Intercepts") + `</a>`,
			`<a href="#cfg-upstreams" data-topnav-target="cfg-upstreams">` + pick("服务商", "Providers") + `</a>`,
			`<a href="#cfg-history" data-topnav-target="cfg-history">` + pick("历史", "History") + `</a>`,
		}, "")
		heroEyebrow = pick("配置中心", "Configuration Center")
		heroTitle = pick("运行路由、探活、服务商。", "Runtime Routing, Health, Providers.")
		heroSub = pick("集中维护探活、桥接、恢复和服务商，不再在多个面板里来回切换。", "Manage probes, bridge, recovery, and providers in one place instead of jumping across surfaces.")
		heroMetaPrimary = ``
		heroMetaSecondary = ``
		heroMetaTertiary = ``
	}
	pageTitle := "AI Gateway Admin"
	if settingsView {
		pageTitle = "AI Gateway Settings"
	}
	if useChinese {
		pageTitle = "AI 模型网关管理台"
		if settingsView {
			pageTitle = "AI 模型网关设置"
		}
	}
	return strings.NewReplacer(
		"{{HTML_LANG}}", adminHTMLLang(language),
		"{{PAGE_TITLE}}", pageTitle,
		"{{BOOTSTRAP_LANGUAGE}}", language,
		"{{BODY_CLASS}}", bodyClass,
		"{{TOPNAV_LINKS}}", topnavLinks,
		"{{HERO_EYEBROW}}", heroEyebrow,
		"{{HERO_TITLE}}", heroTitle,
		"{{HERO_SUB}}", heroSub,
		"{{HERO_META_PRIMARY}}", heroMetaPrimary,
		"{{HERO_META_SECONDARY}}", heroMetaSecondary,
		"{{HERO_META_TERTIARY}}", heroMetaTertiary,
		"{{ROUTER_STRATEGY_HEALTH_WEIGHTED_RR}}", routerStrategyHealthWeightedRR,
		"{{ROUTER_STRATEGY_ROUND_ROBIN}}", routerStrategyRoundRobin,
	).Replace(adminHTMLTemplate)
}
