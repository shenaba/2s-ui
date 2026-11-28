package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sagernet/sing-box/experimental/deprecated"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	singservice "github.com/sagernet/sing/service"
	"github.com/shenaba/2s-ui/core"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/service"
	"gorm.io/gorm"
)

func TestJsonSubscriptionClientAPIAndDomainResolver(t *testing.T) {
	setupSubDB(t)
	if err := database.GetDB().Create(&model.Client{
		Name: "fixture", Enable: true, Config: json.RawMessage(`{}`),
		Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Create(&model.Setting{Key: "subJsonExt", Value: `{}`}).Error; err != nil {
		t.Fatal(err)
	}
	dashboard := t.TempDir()
	const dashboardHTML = "<html><body>PR3 fixture dashboard</body></html>"
	if err := os.WriteFile(filepath.Join(dashboard, "index.html"), []byte(dashboardHTML), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	path, err := json.Marshal(dashboard)
	if err != nil {
		t.Fatal(err)
	}
	template := fmt.Sprintf(`{
		"inbounds": [],
		"dns": {"servers": [
			{"type":"hosts","tag":"dns-proxy","predefined":{"fixture.invalid":"127.0.0.2"}},
			{"type":"hosts","tag":"dns-direct","predefined":{"fixture.invalid":["127.0.0.1","::1"]}}
		],"final":"dns-proxy"},
		"default_domain_resolver":{"server":"dns-direct","strategy":"ipv4_only"},
		"http_clients":[{"tag":"dl"}],
		"services":[{"type":"api","tag":"dashboard-api","listen":"127.0.0.1","listen_port":%d,
			"dashboard":{"enabled":true,"path":%s,"http_client":"dl","update_interval":"1d"}}]
	}`, port, path)
	if err := saveSubClientOptionsTemplate(t, template); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if err := NewSubHandler(router.Group("/sub")); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/sub/fixture?format=json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("订阅状态为 %d，响应为 %s", w.Code, w.Body.String())
	}
	ctx := core.Context(t.Context(), core.InboundRegistry(), core.OutboundRegistry(),
		core.EndpointRegistry(), core.DNSTransportRegistry(), core.ServiceRegistry(), core.CertificateProviderRegistry())
	ctx = singservice.ContextWith[deprecated.Manager](ctx, discardDeprecatedNotes{})
	var opts option.Options
	if err := opts.UnmarshalJSONContext(ctx, w.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	if opts.Route == nil || opts.Route.DefaultDomainResolver == nil || opts.Route.DefaultDomainResolver.Server != "dns-direct" {
		t.Fatal("订阅未保留对象指定的 DNS 解析器")
	}
	strategy, err := json.Marshal(opts.Route.DefaultDomainResolver.Strategy)
	if err != nil || string(strategy) != `"ipv4_only"` {
		t.Fatalf("订阅未保留 IPv4 解析策略，实际为 %s，错误 %v", strategy, err)
	}
	if len(opts.Services) != 1 || opts.Services[0].Type != "api" || opts.Services[0].Tag != "dashboard-api" {
		t.Fatalf("订阅未保留 API 服务: %+v", opts.Services)
	}
	instance, err := core.NewBox(core.Options{Context: ctx, Options: opts})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	if err = instance.Start(); err != nil {
		t.Fatal(err)
	}
	local := &http.Client{Transport: &http.Transport{}, Timeout: 3 * time.Second}
	t.Cleanup(local.CloseIdleConnections)
	resp, err := local.Get(fmt.Sprintf("http://127.0.0.1:%d/dashboard/", port))
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK || string(content) != dashboardHTML {
		t.Fatalf("客户端未正确提供订阅配置的网页面板，状态 %d，内容 %q，错误 %v", resp.StatusCode, content, err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(target.Close)
	direct, ok := instance.Outbound().Outbound("direct")
	if !ok {
		t.Fatal("订阅缺少内置直连出站")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		return direct.DialContext(ctx, network, M.ParseSocksaddr(addr))
	}}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	targetPort := strconv.Itoa(target.Listener.Addr().(*net.TCPAddr).Port)
	resp, err = client.Get("http://" + net.JoinHostPort("fixture.invalid", targetPort))
	if err != nil {
		t.Fatalf("应使用 dns-direct 解析并连接至本机探针: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("探针状态为 %d，期望 HTTP 204", resp.StatusCode)
	}
}

func TestJsonSubscriptionClientOptionsValidation(t *testing.T) {
	setupSubDB(t)
	if err := database.GetDB().Create(&model.Setting{Key: "subJsonExt", Value: `{}`}).Error; err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, template, field string }{
		{"服务不是数组", `{"services":{}}`, "services"},
		{"服务类型错误", `{"services":[{"type":"resolved"}]}`, "services[0]"},
		{"未知面板选项", `{"services":[{"type":"api","dashboard":{"bad_option":true}}]}`, "services[0]"},
		{"服务标签重复", `{"services":[{"type":"api","tag":"one"},{"type":"api","tag":"one"}]}`, "one"},
		{"解析器没有服务器", `{"default_domain_resolver":{"strategy":"ipv4_only"}}`, "default_domain_resolver"},
		{"解析策略错误", `{"default_domain_resolver":{"server":"a","strategy":"bad"}}`, "default_domain_resolver"},
		{"解析器类型错误", `{"default_domain_resolver":true}`, "default_domain_resolver"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := saveSubClientOptionsTemplate(t, tt.template); err == nil || !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("保存时应报告字段 %q，实际错误 %v", tt.field, err)
			}
		})
	}
	if err := saveSubClientOptionsTemplate(t, `{"default_domain_resolver":"","services":[]}`); err != nil {
		t.Fatalf("应接受空解析器及空服务列表: %v", err)
	}
}

func saveSubClientOptionsTemplate(t *testing.T, template string) error {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"subJsonExt": template})
	if err != nil {
		t.Fatal(err)
	}
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		return (&service.SettingService{}).Save(tx, payload)
	})
}
