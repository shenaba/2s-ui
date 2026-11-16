package middleware

import (
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func DomainValidator(domain string) gin.HandlerFunc {
	domain = strings.TrimSuffix(strings.TrimPrefix(domain, "["), "]")
	return func(c *gin.Context) {
		host := c.Request.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		// An IPv6 Host on the default port has no port, only its brackets, and
		// SplitHostPort fails on it: that used to leave host empty, a 403.
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")

		// 域名大小写不敏感(network/tls.go 的 SNI 校验也用 EqualFold)。精确比对会在
		// 设置里存了大小写混杂的域名时把整个面板 403 掉——TLS 握手反而成功,极难定位。
		if !strings.EqualFold(host, domain) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		c.Next()
	}
}
