package sub

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"

	"github.com/gin-gonic/gin"
)

type SubHandler struct {
	service.SettingService
	SubService
	JsonService
	ClashService
}

func NewSubHandler(g *gin.RouterGroup) error {
	assets, err := subscriberAssets()
	if err != nil {
		return err
	}
	a := &SubHandler{}
	return a.initRouter(g, assets)
}

// initRouter takes the asset FS rather than reading the embedded one itself so
// a test can supply a populated fixture: a bare checkout has no dashboard/assets
// directory, and CI builds from one, so the served-a-real-asset half would
// otherwise never be exercised anywhere automatic.
//
// The dashboard's index.html references its bundle relatively, so a browser on
// /{subPath}/{clientName} asks for /{subPath}/assets/... — without that route
// the page loads and then renders blank. Registering a literal path segment
// next to :subid is fine for gin's tree; the one cost is that a client named
// exactly "assets" cannot be reached below its own path.
func (s *SubHandler) initRouter(g *gin.RouterGroup, assets fs.FS) error {
	// Every emitted filename carries a content hash, so a year is safe and an
	// upgrade invalidates only what changed — same deal as the panel's own
	// assets in web.go. The header is set only for a name the embedded FS
	// actually holds: a miss is answered 404 by the static handler below, and
	// a 404 carrying explicit freshness is cacheable, so a request that lands
	// mid-upgrade would otherwise pin an empty dashboard for a year.
	assetsPrefix := path.Join(g.BasePath(), "assets") + "/"
	g.Use(func(c *gin.Context) {
		if !strings.HasPrefix(c.Request.URL.Path, assetsPrefix) {
			return
		}
		if _, err := fs.Stat(assets, strings.TrimPrefix(c.Request.URL.Path, assetsPrefix)); err == nil {
			c.Header("Cache-Control", "max-age=31536000")
		}
	})
	g.StaticFS("/assets", http.FS(assets))

	g.GET("/:subid", s.subs)
	g.HEAD("/:subid", s.subHeaders)
	return nil
}

func (s *SubHandler) subs(c *gin.Context) {
	var headers []string
	var result *string
	var err error
	subId := c.Param("subid")
	format, isFormat := c.GetQuery("format")

	// With no ?format, a well-known client gets the format it reads -- Clash
	// and mihomo their YAML, sing-box its JSON, Happ its links -- and anything
	// else the plain links, as before (formatForUserAgent). The answer then
	// depends on the User-Agent, which a cache has to be told: Vary for the
	// caches that honour it, and private for the ones that do not -- a CDN
	// rule caching the sub path (Cloudflare ignores Vary outside images)
	// would otherwise hand the first Clash client's YAML to everyone.
	byAgent := false
	if !isFormat {
		c.Writer.Header().Add("Vary", "User-Agent")
		c.Writer.Header().Set("Cache-Control", "private")
		format = formatForUserAgent(c.Request.UserAgent())
		byAgent = format != ""
	}
	switch {
	// The dashboard and the JSON behind it stay behind an explicit ?format:
	// no User-Agent picks them (formatForUserAgent).
	case isFormat && format == "page":
		s.serveDashboard(c, subId)
		return
	case isFormat && format == "info":
		s.serveClientInfo(c, subId)
		return
	case format == "json":
		result, headers, err = s.JsonService.GetJson(subId, format)
	case format == "clash":
		result, headers, err = s.ClashService.GetClash(subId)
	case format == "happ":
		result, headers, err = s.SubService.getSubs(subId, happLinks)
	case !isFormat:
		result, headers, err = s.SubService.GetSubs(subId)
	}
	// A format the agent picked falls back to the plain links when it cannot
	// be built: those are what this URL served before it looked at agents,
	// and a mihomo proxy-provider reads them. One broken inbound row fails
	// the JSON and Clash builds but not the stored links. A format named in
	// the query is answered as asked, error included.
	if byAgent && (err != nil || result == nil) {
		logger.Warning("sub: ", format, " for ", subId, " failed, serving the plain links: ", err)
		result, headers, err = s.SubService.GetSubs(subId)
	}
	if err != nil || result == nil {
		logger.Error(err)
		c.String(400, "Error!")
		return
	}

	s.addHeaders(c, headers)

	c.String(200, *result)
}

// serveClientInfo answers with the live figures the dashboard renders.
func (s *SubHandler) serveClientInfo(c *gin.Context, subId string) {
	info, err := s.getClientInfoJSON(subId)
	if err != nil {
		logger.Error(err)
		c.String(400, "Error!")
		return
	}

	jsonInfo, err := json.Marshal(info)
	if err != nil {
		logger.Error(err)
		c.String(500, "Error!")
		return
	}

	c.Data(200, "application/json; charset=utf-8", jsonInfo)
}

// serveDashboard answers ?format=page with the embedded Vue subscriber
// dashboard.
func (s *SubHandler) serveDashboard(c *gin.Context, subId string) {
	// Resolve the client first: an unknown sub id must stay a 400, rather than
	// becoming a 200 that renders a dashboard which then fails to load its own
	// data. A disabled one does resolve -- explaining that is the page's job,
	// and getAnyClientBySubId is what the info endpoint answers with too.
	client, err := s.SubService.getAnyClientBySubId(subId)
	if err != nil {
		logger.Error(err)
		c.String(400, "Error!")
		return
	}

	page, err := subscriberIndex()
	if err != nil {
		logger.Error("sub: error reading dashboard index.html:", err)
		c.String(500, "Error!")
		return
	}

	// Profile headers only, and only for a live client. Content-Disposition is
	// deliberately left off: it is what makes a browser download the page
	// instead of rendering it. Subscription-Userinfo carries the same counters
	// getClientInfoJSON withholds from a disabled client, so sending it here
	// would hand them straight back on the very URL that was revoked.
	if client.Enable {
		s.addProfileHeaders(c, s.SubService.getClientHeaders(client))
	}
	c.Data(200, "text/html; charset=utf-8", page)
}

func (s *SubHandler) subHeaders(c *gin.Context) {
	subId := c.Param("subid")
	client, err := s.SubService.getClientBySubId(subId)
	if err != nil {
		logger.Error(err)
		c.String(400, "Error!")
		return
	}

	headers := s.SubService.getClientHeaders(client)
	s.addHeaders(c, headers)

	c.Status(200)
}

// addHeaders is the raw-subscription set: the operator's custom headers, the
// profile headers, and the Content-Disposition that makes a proxy client save
// the payload to a file. The custom ones go first and the panel's own after,
// though Save already refuses a custom line naming one of those. Not on the
// dashboard page: these are meant for proxy clients, which never load it.
func (s *SubHandler) addHeaders(c *gin.Context, headers []string) {
	if custom, err := s.SettingService.GetSubCustomHeaders(); err != nil {
		logger.Warning("sub: error reading subCustomHeaders:", err)
	} else {
		for _, h := range custom {
			c.Writer.Header().Set(h.Name, h.Value)
		}
	}
	s.addProfileHeaders(c, headers)
	c.Writer.Header().Set("Content-Disposition", contentDispositionHeader(headers[2]))
}

// addProfileHeaders sets only the descriptive headers, for responses that are
// meant to be rendered rather than downloaded.
func (s *SubHandler) addProfileHeaders(c *gin.Context, headers []string) {
	c.Writer.Header().Set("Subscription-Userinfo", headers[0])
	c.Writer.Header().Set("Profile-Update-Interval", headers[1])
	c.Writer.Header().Set("Profile-Title", headers[2])
}

func contentDispositionHeader(name string) string {
	filename := strings.TrimSpace(name)
	if filename == "" {
		filename = "subscription"
	}

	return fmt.Sprintf("attachment; filename=\"%s\"; filename*=UTF-8''%s", asciiSafeFilename(filename), rfc5987Encode(filename))
}

func asciiSafeFilename(filename string) string {
	var builder strings.Builder
	for _, r := range filename {
		switch {
		case r == '"' || r == '\\':
			builder.WriteByte('_')
		case r >= 0x20 && r <= 0x7e:
			builder.WriteRune(r)
		}
	}

	fallback := strings.TrimSpace(builder.String())
	if fallback == "" {
		return "subscription"
	}

	return fallback
}

func rfc5987Encode(filename string) string {
	const hex = "0123456789ABCDEF"

	var builder strings.Builder
	for _, b := range []byte(filename) {
		if isRFC5987AttrChar(b) {
			builder.WriteByte(b)
			continue
		}

		builder.WriteByte('%')
		builder.WriteByte(hex[b>>4])
		builder.WriteByte(hex[b&0x0f])
	}

	return builder.String()
}

func isRFC5987AttrChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= 'A' && b <= 'Z':
		return true
	case b >= '0' && b <= '9':
		return true
	}

	switch b {
	case '!', '#', '$', '&', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	default:
		return false
	}
}
