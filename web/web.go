package web

import (
	"context"
	"crypto/tls"
	"embed"
	"html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/shenaba/2s-ui/api"
	"github.com/shenaba/2s-ui/config"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/middleware"
	"github.com/shenaba/2s-ui/network"
	"github.com/shenaba/2s-ui/service"

	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

//go:embed *
var content embed.FS

type Server struct {
	httpServer     *http.Server
	listener       net.Listener
	ctx            context.Context
	cancel         context.CancelFunc
	settingService service.SettingService
}

func NewServer() *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		ctx:    ctx,
		cancel: cancel,
	}
}

func (s *Server) initRouter() (*gin.Engine, error) {
	if config.IsDebug() {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.DefaultWriter = io.Discard
		gin.DefaultErrorWriter = io.Discard
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.Default()

	// Load the HTML template
	t := template.New("").Funcs(engine.FuncMap)
	template, err := t.ParseFS(content, "html/index.html")
	if err != nil {
		return nil, err
	}
	engine.SetHTMLTemplate(template)

	base_url, err := s.settingService.GetWebPath()
	if err != nil {
		return nil, err
	}

	webDomain, err := s.settingService.GetWebDomain()
	if err != nil {
		return nil, err
	}

	if webDomain != "" {
		// Pins every route, apiv2 included. Exempting apiv2 for masters dialing
		// a node by IP was considered and rejected: TLS gets there first (the
		// master sends no SNI, so an HTTPS node with a domain rejects the
		// handshake outright), which leaves only the plain-HTTP-with-a-domain
		// case — while the carve-out would cost an unpinned Host on every apiv2
		// route. A node with a web domain must be dialed by that domain; when
		// it is not, nodeGet/nodePost say so.
		engine.Use(middleware.DomainValidator(webDomain))
	}

	secret, err := s.settingService.GetSecret()
	if err != nil {
		return nil, err
	}

	// The websocket endpoint is excluded: an upgraded connection must not go
	// through the compressing response writer.
	engine.Use(gzip.Gzip(gzip.DefaultCompression, gzip.WithExcludedPaths([]string{base_url + "ws"})))
	assetsBasePath := base_url + "assets/"

	store := cookie.NewStore(secret)
	engine.Use(sessions.Sessions("s-ui", store))

	engine.Use(func(c *gin.Context) {
		uri := c.Request.RequestURI
		if strings.HasPrefix(uri, assetsBasePath) {
			c.Header("Cache-Control", "max-age=31536000")
		}
	})

	// Serve the assets folder
	assetsFS, err := fs.Sub(content, "html/assets")
	if err != nil {
		panic(err)
	}

	engine.StaticFS(assetsBasePath, http.FS(assetsFS))

	group_apiv2 := engine.Group(base_url + "apiv2")
	apiv2 := api.NewAPIv2Handler(group_apiv2)

	group_api := engine.Group(base_url + "api")
	api.NewAPIHandler(group_api, apiv2)

	// Push channel for the SPA. Static sibling of api/apiv2 — registering it
	// inside group_api would collide with the GET /:getAction wildcard.
	engine.GET(base_url+"ws", api.WsHandler)

	// Serve index.html as the entry point
	// Handle all other routes by serving index.html
	engine.NoRoute(func(c *gin.Context) {
		if c.Request.URL.Path == strings.TrimSuffix(base_url, "/") {
			c.Redirect(http.StatusTemporaryRedirect, base_url)
			return
		}
		if !strings.HasPrefix(c.Request.URL.Path, base_url) {
			c.String(404, "")
			return
		}
		if c.Request.URL.Path != base_url+"login" && !api.IsLogin(c) {
			c.Redirect(http.StatusTemporaryRedirect, base_url+"login")
			return
		}
		if c.Request.URL.Path == base_url+"login" && api.IsLogin(c) {
			c.Redirect(http.StatusTemporaryRedirect, base_url)
			return
		}
		c.HTML(http.StatusOK, "index.html", gin.H{"BASE_URL": base_url})
	})

	return engine, nil
}

func (s *Server) Start() (err error) {
	//This is an anonymous function, no function name
	defer func() {
		if err != nil {
			s.Stop()
		}
	}()

	engine, err := s.initRouter()
	if err != nil {
		return err
	}

	certFile, err := s.settingService.GetCertFile()
	if err != nil {
		return err
	}
	keyFile, err := s.settingService.GetKeyFile()
	if err != nil {
		return err
	}
	listen, err := s.settingService.GetListen()
	if err != nil {
		return err
	}
	port, err := s.settingService.GetPort()
	if err != nil {
		return err
	}
	certMode, err := s.settingService.GetWebCertMode()
	if err != nil {
		return err
	}
	listenAddr := net.JoinHostPort(listen, strconv.Itoa(port))
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	scheme := "http"
	nginxMode, _ := s.settingService.GetWebNginx()
	if runtime.GOOS != "windows" && nginxMode {
		// 已部署 nginx:由 nginx 终结 SSL,面板自身只跑 HTTP,不加载任何证书
		scheme = "http (behind nginx)"
	} else if certMode == "acme" {
		// Auto-issue/renew via Let's Encrypt (HTTP-01). On any failure we fall
		// back to plain HTTP so a misconfigured domain or blocked port 80 can
		// never lock the operator out of the panel.
		domain, _ := s.settingService.GetWebDomain()
		email, _ := s.settingService.GetWebAcmeEmail()
		if domain == "" {
			logger.Warning("web ACME enabled but webDomain is empty; serving HTTP")
		} else if tlsConfig, err := network.ACMETLSConfig(domain, email, config.GetCertFolderPath()); err != nil {
			logger.Error("web ACME certificate error, falling back to HTTP:", err)
		} else {
			listener = network.NewAutoHttpsListener(listener)
			listener = tls.NewListener(listener, tlsConfig)
			scheme = "https (ACME)"
		}
	} else if certFile != "" || keyFile != "" {
		webDomain, err := s.settingService.GetWebDomain()
		if err != nil {
			return err
		}
		c, err := network.NewTLSConfig(certFile, keyFile, webDomain)
		if err != nil {
			listener.Close()
			return err
		}
		listener = network.NewAutoHttpsListener(listener)
		listener = tls.NewListener(listener, c)
		scheme = "https"
	}

	logger.Info("web server run "+scheme+" on", listener.Addr())
	s.listener = listener

	s.httpServer = &http.Server{
		Handler: engine,
	}

	go func() {
		s.httpServer.Serve(listener)
	}()

	return nil
}

func (s *Server) Stop() error {
	var err error
	if s.httpServer != nil {
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
		err = s.httpServer.Shutdown(shutdownCtx)
		cancelShutdown()
		if err != nil {
			s.cancel()
			if s.listener != nil {
				_ = s.listener.Close()
			}
			return err
		}
	} else if s.listener != nil {
		err = s.listener.Close()
		if err != nil {
			s.cancel()
			return err
		}
	}
	s.cancel()
	return nil
}

func (s *Server) GetCtx() context.Context {
	return s.ctx
}
