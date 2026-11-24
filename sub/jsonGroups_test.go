package sub

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/service"
	"gorm.io/gorm"
)

func TestJsonOutboundGroups(t *testing.T) {
	setupSubDB(t)
	seedMultiAddrClient(t, "multi", `[{"server":"192.0.2.1","server_port":443,"remark":"-jp"},
		{"server":"192.0.2.2","server_port":443,"remark":"-us"}]`)
	seedLocalIPv6Client(t, "local", "")
	if err := database.GetDB().Create(&model.Client{
		Name: "empty", Enable: true, Config: json.RawMessage(`{}`),
		Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Create(&model.Setting{Key: "subJsonExt", Value: `{
		"outbound_groups": [
			{"type":"selector","tag":"👉 手动选择","outbounds":["♻️ 自动选择","direct","$nodes","$nodes"],"default":"$nodes"},
			{"type":"urltest","tag":"♻️ 自动选择","outbounds":["$nodes"],"interval":"10m","idle_timeout":"30m"}
		],
		"rules": [{"action":"route","domain_suffix":["example.org"],"outbound":"👉 手动选择"}],
		"final": "👉 手动选择"
	}`}).Error; err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if err := NewSubHandler(router.Group("/sub")); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, defaultTag string
		nodes            []string
	}{
		{"multi", "multi-in-jp", []string{"multi-in-jp", "multi-in-us"}},
		{"local", "v6-in", []string{"v6-in"}},
		{"empty", "♻️ 自动选择", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/sub/"+tt.name+"?format=json", nil))
			if w.Code != 200 {
				t.Fatalf("订阅状态码为 %d，响应为 %s", w.Code, w.Body.String())
			}
			var result struct {
				Outbounds []struct {
					Type     string   `json:"type"`
					Tag      string   `json:"tag"`
					Members  []string `json:"outbounds"`
					Default  string   `json:"default"`
					Interval string   `json:"interval"`
				} `json:"outbounds"`
				Route struct {
					Final string `json:"final"`
					Rules []struct {
						Outbound string `json:"outbound"`
					} `json:"rules"`
				} `json:"route"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Route.Final != "👉 手动选择" || len(result.Route.Rules) != 1 || result.Route.Rules[0].Outbound != result.Route.Final {
				t.Fatalf("路由没有指向自定义分组: %+v", result.Route)
			}
			for _, group := range result.Outbounds {
				switch group.Tag {
				case "👉 手动选择":
					want := append([]string{"♻️ 自动选择", "direct"}, tt.nodes...)
					if !reflect.DeepEqual(group.Members, want) || group.Default != tt.defaultTag {
						t.Errorf("手动组成员或默认值错误: %+v", group)
					}
				case "♻️ 自动选择":
					if len(tt.nodes) == 0 {
						if group.Type != "selector" || group.Interval != "" || !reflect.DeepEqual(group.Members, []string{"direct"}) {
							t.Errorf("无节点时自动组应降级为直连选择组: %+v", group)
						}
					} else if group.Type != "urltest" || group.Interval != "10m" || !reflect.DeepEqual(group.Members, tt.nodes) {
						t.Errorf("自动组的测速选项或成员错误: %+v", group)
					}
				}
			}
			buildSingBox(t, w.Body.String())
		})
	}
}

func TestJsonOutboundGroupsSettingsValidation(t *testing.T) {
	setupSubDB(t)
	if err := database.GetDB().Create(&model.Setting{Key: "subJsonExt", Value: `{}`}).Error; err != nil {
		t.Fatal(err)
	}
	settings := &service.SettingService{}
	for _, tt := range []struct{ name, groups string }{
		{"空数组类型", `null`},
		{"保留标签", `[{"type":"selector","tag":"proxy","outbounds":["direct"]}]`},
		{"重复标签", `[{"type":"selector","tag":"g","outbounds":["direct"]},{"type":"selector","tag":"g","outbounds":["direct"]}]`},
		{"分组环路", `[{"type":"selector","tag":"a","outbounds":["b"]},{"type":"urltest","tag":"b","outbounds":["a"]}]`},
		{"不兼容选项", `[{"type":"urltest","tag":"g","outbounds":["$nodes"],"default":"$nodes"}]`},
		{"无效默认项", `[{"type":"selector","tag":"g","outbounds":["direct"],"default":"$nodes"}]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]string{"subJsonExt": `{"outbound_groups":` + tt.groups + `}`})
			if err != nil {
				t.Fatal(err)
			}
			if err := database.GetDB().Transaction(func(tx *gorm.DB) error { return settings.Save(tx, payload) }); err == nil {
				t.Fatal("保存时应拒绝无效的分组模板")
			}
		})
	}
	seedLocalIPv6Client(t, "local", "")
	if err := database.GetDB().Model(&model.Setting{}).Where("key = ?", "subJsonExt").Update("value", `{
		"outbound_groups":[{"type":"selector","tag":"g","outbounds":["missing-node"]}]
	}`).Error; err != nil {
		t.Fatal(err)
	}
	if result, _, err := (&JsonService{}).GetJson("local", "json"); result != nil || err == nil || !strings.Contains(err.Error(), "missing-node") {
		t.Fatalf("应在生成时报告客户端缺少的出站，实际结果 %v，错误 %v", result, err)
	}
}
