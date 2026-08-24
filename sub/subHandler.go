package sub

import (
    "encoding/json"
    "fmt"
    "io/fs"
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

func NewSubHandler(g *gin.RouterGroup) {
    a := &SubHandler{}
    a.initRouter(g)
}

func (s *SubHandler) initRouter(g *gin.RouterGroup) {
    g.GET("/:subid", s.subs)
    g.HEAD("/:subid", s.subHeaders)
}

func (s *SubHandler) subs(c *gin.Context) {
    var headers []string
    var result *string
    var err error
    subId := c.Param("subid")
    format, isFormat := c.GetQuery("format")

    // JSON info endpoint backing the subscriber dashboard. It is a new,
    // opt-in format (`?format=info`) distinct from the existing `?format=json`
    // (JsonService) and `?format=clash` (ClashService).
    if isFormat && format == "info" {
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
        return
    }

    // A browser opening the subscription URL gets the Vue dashboard page
    // instead of raw config text. Proxy clients and tools (non-browser UAs)
    // fall through to the unchanged raw-config path below.
    if !isFormat && isBrowserUA(c.Request.UserAgent()) {
        s.serveDashboard(c, subId)
        return
    }

    if isFormat {
        switch format {
        case "json":
            result, headers, err = s.JsonService.GetJson(subId, format)
        case "clash":
            result, headers, err = s.ClashService.GetClash(subId)
        }
        if err != nil || result == nil {
            logger.Error(err)
            c.String(400, "Error!")
            return
        }
    } else {
        result, headers, err = s.SubService.GetSubs(subId)
        if err != nil || result == nil {
            logger.Error(err)
            c.String(400, "Error!")
            return
        }
    }

    s.addHeaders(c, headers)

    c.String(200, *result)
}

// serveDashboard answers a browser request to the subscription URL with the
// embedded Vue subscriber dashboard. The usual profile headers are still set
// so the page is self-describing alongside the raw endpoint.
func (s *SubHandler) serveDashboard(c *gin.Context, subId string) {
    subContent, err := subscriberContent()
    if err != nil {
        logger.Error("sub: error opening embedded dashboard:", err)
        c.String(500, "Error!")
        return
    }

    // Set the subscription profile headers for completeness.
    if client, err := s.SubService.getClientBySubId(subId); err == nil {
        headers := s.SubService.getClientHeaders(client)
        s.addHeaders(c, headers)
    }

    page, err := fs.ReadFile(subContent, "index.html")
    if err != nil {
        logger.Error("sub: error reading dashboard index.html:", err)
        c.String(500, "Error!")
        return
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

func (s *SubHandler) addHeaders(c *gin.Context, headers []string) {
    c.Writer.Header().Set("Subscription-Userinfo", headers[0])
    c.Writer.Header().Set("Profile-Update-Interval", headers[1])
    c.Writer.Header().Set("Profile-Title", headers[2])
    c.Writer.Header().Set("Content-Disposition", contentDispositionHeader(headers[2]))
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
