package sub

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/service"
	"github.com/shenaba/2s-ui/util"
)

type SubService struct {
	service.SettingService
	LinkService
}

func (s *SubService) GetSubs(subId string) (*string, []string, error) {
	var err error

	client, err := s.getClientBySubId(subId)
	if err != nil {
		return nil, nil, err
	}

	clientInfo := ""
	subShowInfo, _ := s.SettingService.GetSubShowInfo()
	if subShowInfo {
		clientInfo = s.getClientInfo(client)
	}

	linksArray := s.LinkService.GetLinks(&client.Links, "all", clientInfo)
	result := strings.Join(linksArray, "\n")

	headers := s.getClientHeaders(client)

	subEncode, _ := s.SettingService.GetSubEncode()
	if subEncode {
		result = base64.StdEncoding.EncodeToString([]byte(result))
	}

	return &result, headers, nil
}

// ClientInfo is the payload served by the subscriber dashboard's JSON info
// endpoint. It carries everything the Vue page needs to render live status:
// expiry/remaining days, used vs total traffic, and the config links.
type ClientInfo struct {
	Name             string `json:"name"`
	Remark           string `json:"remark,omitempty"`
	Title            string `json:"title"`
	Enable           bool   `json:"enable"`
	Expiry           int64  `json:"expiry"`
	RemainingDays    int64  `json:"remainingDays"`
	Volume           int64  `json:"volume"`
	Up               int64  `json:"up"`
	Down             int64  `json:"down"`
	Used             int64  `json:"used"`
	RemainingTraffic int64  `json:"remainingTraffic"`
	Unlimited        bool   `json:"unlimited"`
	// Expired is decided here rather than from Expiry in the browser, so the
	// page cannot disagree with the panel over a skewed client clock.
	Expired bool     `json:"expired"`
	Links   []string `json:"links"`
}

// getClientInfoJSON resolves the given sub id and returns the live info the
// subscriber dashboard displays. Links are the plain URIs (no injected traffic
// info), matching what a proxy client would consume from the raw subscribe.
func (s *SubService) getClientInfoJSON(subId string) (*ClientInfo, error) {
	client, err := s.getAnyClientBySubId(subId)
	if err != nil {
		return nil, err
	}

	info := &ClientInfo{
		Name:      client.Name,
		Remark:    client.Remark,
		Enable:    client.Enable,
		Expiry:    client.Expiry,
		Volume:    client.Volume,
		Up:        client.Up,
		Down:      client.Down,
		Used:      client.Up + client.Down,
		Title:     client.Remark,
		Unlimited: client.Volume <= 0,
	}
	if info.Title == "" {
		info.Title = client.Name
	}

	info.RemainingTraffic = client.Volume - info.Used
	if info.RemainingTraffic < 0 {
		info.RemainingTraffic = 0
	}

	now := time.Now().Unix()
	if client.Expiry > 0 {
		info.Expired = client.Expiry <= now
		info.RemainingDays = (client.Expiry - now) / 86400
		if info.RemainingDays < 0 {
			info.RemainingDays = 0
		}
	}

	// Links stay behind the enable flag: the page has to explain a disabled
	// subscription, not hand out working configs for one.
	if client.Enable {
		info.Links = s.LinkService.GetLinks(&client.Links, "all", "")
	}

	return info, nil
}

// getAnyClientBySubId resolves a sub id without getClientBySubId's enable
// filter. Only the subscriber dashboard uses it: a client that has been
// disabled asking why its subscription stopped working is precisely what that
// page exists for, and the filter turns that into a bare "Error!". Everything
// that hands out a config keeps the filter.
func (j *SubService) getAnyClientBySubId(subId string) (*model.Client, error) {
	db := database.GetDB()
	client := &model.Client{}
	err := db.Model(model.Client{}).Where("name = ?", subId).First(client).Error
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (j *SubService) getClientBySubId(subId string) (*model.Client, error) {
	db := database.GetDB()
	client := &model.Client{}
	err := db.Model(model.Client{}).Where("enable = true and name = ?", subId).First(client).Error
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (s *SubService) getClientHeaders(client *model.Client) []string {
	updateInterval, _ := s.SettingService.GetSubUpdates()
	return util.GetHeaders(client, updateInterval)
}

func (s *SubService) getClientInfo(c *model.Client) string {
	now := time.Now().Unix()

	var result []string
	if vol := c.Volume - (c.Up + c.Down); vol > 0 {
		result = append(result, fmt.Sprintf("%s%s", s.formatTraffic(vol), "📊"))
	}
	if c.Expiry > 0 {
		result = append(result, fmt.Sprintf("%d%s⏳", (c.Expiry-now)/86400, "Days"))
	}
	if len(result) > 0 {
		return " " + strings.Join(result, " ")
	} else {
		return " ♾"
	}
}

func (s *SubService) formatTraffic(trafficBytes int64) string {
	if trafficBytes < 1024 {
		return fmt.Sprintf("%.2fB", float64(trafficBytes)/float64(1))
	} else if trafficBytes < (1024 * 1024) {
		return fmt.Sprintf("%.2fKB", float64(trafficBytes)/float64(1024))
	} else if trafficBytes < (1024 * 1024 * 1024) {
		return fmt.Sprintf("%.2fMB", float64(trafficBytes)/float64(1024*1024))
	} else if trafficBytes < (1024 * 1024 * 1024 * 1024) {
		return fmt.Sprintf("%.2fGB", float64(trafficBytes)/float64(1024*1024*1024))
	} else if trafficBytes < (1024 * 1024 * 1024 * 1024 * 1024) {
		return fmt.Sprintf("%.2fTB", float64(trafficBytes)/float64(1024*1024*1024*1024))
	} else {
		return fmt.Sprintf("%.2fEB", float64(trafficBytes)/float64(1024*1024*1024*1024*1024))
	}
}
