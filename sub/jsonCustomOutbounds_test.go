package sub

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sagernet/sing-box/protocol/group"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/service"
	"gorm.io/gorm"
)

func TestJsonCustomOutboundsSwitchBetweenDirectAndBlock(t *testing.T) {
	setupSubDB(t)
	if err := database.GetDB().Create(&model.Client{
		Name: "empty", Enable: true, Config: json.RawMessage(`{}`),
		Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Create(&model.Setting{Key: "subJsonExt", Value: "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	const direct = "🎯 直接连接"
	const block = "⛔️ 禁止连接"
	if err := saveCustomOutboundsTestTemplate(t, `{
		"inbounds": [],
		"custom_outbounds": [
			{"type":"direct","tag":"`+direct+`","connect_timeout":"10s"},
			{"type":"block","tag":"`+block+`"}
		],
		"outbound_groups": [
			{"type":"selector","tag":"广告","outbounds":["`+block+`","`+direct+`","$nodes"],"default":"`+block+`"}
		],
		"rules": [], "final": "广告"
	}`); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if err := NewSubHandler(router.Group("/sub")); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sub/empty?format=json", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("订阅状态码为 %d，响应为 %s", response.Code, response.Body.String())
	}
	outbounds := outboundsOf(t, response.Body.String())
	namedDirect, found := outboundByTag(outbounds, direct)
	if !found || namedDirect["type"] != "direct" || namedDirect["connect_timeout"] != "10s" {
		t.Fatalf("具名直连没有保留类型或连接超时: %v", namedDirect)
	}
	if _, found := outboundByTag(outbounds, "direct"); !found {
		t.Fatal("应保留内置 direct 出站")
	}
	instance := newSubscriptionBox(t, response.Body.String())
	t.Cleanup(func() { _ = instance.Close() })
	if err := instance.Start(); err != nil {
		t.Fatal(err)
	}
	outbound, _ := instance.Outbound().Outbound("广告")
	selector, ok := outbound.(*group.Selector)
	if !ok || selector.Selected("tcp").Tag() != block {
		t.Fatalf("广告选择组没有默认选中阻断: %v", outbound)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(target.Close)
	transport := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return selector.DialContext(ctx, network, M.ParseSocksaddr(addr))
		},
	}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	for _, tag := range []string{block, direct, block} {
		if !selector.SelectOutbound(tag) {
			t.Fatalf("选择组没有出站 %q", tag)
		}
		resp, err := client.Get(target.URL)
		if resp != nil {
			resp.Body.Close()
		}
		if tag == block {
			if resp != nil || !errors.Is(err, syscall.EPERM) {
				t.Fatalf("选择阻断时应拒绝连接，实际响应 %v，错误 %v", resp, err)
			}
		} else if err != nil || resp.StatusCode != http.StatusNoContent {
			t.Fatalf("选择直连时应收到 HTTP 204，实际响应 %v，错误 %v", resp, err)
		}
	}
}

func TestJsonCustomOutboundsSettingsValidation(t *testing.T) {
	setupSubDB(t)
	if err := database.GetDB().Create(&model.Setting{Key: "subJsonExt", Value: "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, template string }{
		{"无效数组", `{"custom_outbounds":null}`},
		{"未知类型", `{"custom_outbounds":[{"type":"socks","tag":"custom"}]}`},
		{"保留标签", `{"custom_outbounds":[{"type":"direct","tag":"direct"}]}`},
		{"重复出站标签", `{"custom_outbounds":[{"type":"direct","tag":"custom"},{"type":"block","tag":"custom"}]}`},
		{"与分组重名", `{"custom_outbounds":[{"type":"block","tag":"custom"}],"outbound_groups":[{"type":"selector","tag":"custom","outbounds":["direct"]}]}`},
		{"未知选项", `{"custom_outbounds":[{"type":"direct","tag":"custom","server":"192.0.2.1"}]}`},
		{"负数超时", `{"custom_outbounds":[{"type":"direct","tag":"custom","connect_timeout":"-1s"}]}`},
		{"阻断出站接收超时", `{"custom_outbounds":[{"type":"block","tag":"custom","connect_timeout":"10s"}]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := saveCustomOutboundsTestTemplate(t, tt.template); err == nil {
				t.Fatal("保存时应拒绝无效的自定义出站")
			}
		})
	}
	seedLocalIPv6Client(t, "local", "")
	if err := saveCustomOutboundsTestTemplate(t, `{"custom_outbounds":[{"type":"block","tag":"v6-in"}]}`); err != nil {
		t.Fatal(err)
	}
	if result, _, err := (&JsonService{}).GetJson("local", "json"); result != nil || err == nil || !strings.Contains(err.Error(), "v6-in") {
		t.Fatalf("生成时应拒绝与节点重名的出站，实际结果 %v，错误 %v", result, err)
	}
}

func saveCustomOutboundsTestTemplate(t *testing.T, template string) error {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"subJsonExt": template})
	if err != nil {
		t.Fatal(err)
	}
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		return (&service.SettingService{}).Save(tx, payload)
	})
}
