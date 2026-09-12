package web

import (
	"regexp"
	"strings"
	"testing"
)

func seoEnrichmentPage(t *testing.T, path string) publicSEOPage {
	t.Helper()
	for _, page := range publicSEOPages {
		if page.Path == path {
			return page
		}
	}
	t.Fatalf("missing SEO page %q", path)
	return publicSEOPage{}
}

func TestCodexSEOContentHasSafeUserConfigExample(t *testing.T) {
	page := seoEnrichmentPage(t, "/learn/codex")

	if page.Title != "Codex 接入 ModelPort 指南｜API 地址、密钥与模型检查" {
		t.Fatalf("Codex title changed unexpectedly: %q", page.Title)
	}
	required := []string{
		"~/.codex/config.toml",
		"model_provider = \"modelport\"",
		"model = \"&lt;MODELPORT_MODEL_ID&gt;\"",
		"[model_providers.modelport]",
		"base_url = \"https://api.modelport.top/v1\"",
		"env_key = \"MODELPORT_API_KEY\"",
		"wire_api = \"responses\"",
		"备份后合并，不覆盖原配置",
		"GET /v1/models",
		"目录响应不保证",
		"上下文压缩",
	}
	for _, want := range required {
		if !strings.Contains(page.Body, want) {
			t.Errorf("Codex body missing %q", want)
		}
	}

	if strings.Contains(page.Body, "api_key =") || regexp.MustCompile(`sk-[A-Za-z0-9]{16,}`).MatchString(page.Body) {
		t.Fatal("Codex SEO body must not embed an API key value")
	}
}

func TestPricingAndTroubleshootingSEOContentStayActionable(t *testing.T) {
	pricing := seoEnrichmentPage(t, "/learn/pricing")
	for _, want := range []string{"假设", "非当前报价", "0.05", "实际分组", "不要重复乘折扣"} {
		if !strings.Contains(pricing.Body, want) {
			t.Errorf("pricing body missing %q", want)
		}
	}

	troubleshooting := seoEnrichmentPage(t, "/learn/troubleshooting")
	for _, want := range []string{
		"MODELPORT_API_KEY:-",
		"MODELPORT_API_KEY 未设置",
		"curl -i --max-time 20",
		"响应为 401",
		"响应为 403",
		"/v1/models</code> 端点本身返回 404",
		"响应为 429",
		"响应为 5xx",
		"不要公开日志中的密钥和提示词",
	} {
		if !strings.Contains(troubleshooting.Body, want) {
			t.Errorf("troubleshooting body missing %q", want)
		}
	}
}
