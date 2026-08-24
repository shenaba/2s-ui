package sub

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/shenaba/2s-ui/config"
	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/middleware"
	"github.com/shenaba/2s-ui/network"
	"github.com/shenaba/2s-ui/service"

	"github.com/gin-gonic/gin"
)

type Server struct {
	httpServer *http.Server
	listener   net.Listener
	ctx        context.Context
	cancel     context.CancelFunc

	service.SettingService
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

	subPath, err := s.SettingService.GetSubPath()
	if err != nil {
		return nil, err
	}

	subDomain, err := s.SettingService.GetSubDomain()
	if err != nil {
		return nil, err
	}

	if subDomain != "" {
		engine.Use(middleware.DomainValidator(subDomain))
	}

	g := engine.Group(subPath)
	if err := NewSubHandler(g); err != nil {
		return nil, err
	}

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

	certFile, err := s.SettingService.GetSubCertFile()
	if err != nil {
		return err
	}
	keyFile, err := s.SettingService.GetSubKeyFile()
	if err != nil {
		return err
	}
	listen, err := s.SettingService.GetSubListen()
	if err != nil {
		return err
	}
	port, err := s.SettingService.GetSubPort()
	if err != nil {
		return err
	}

	certMode, err := s.SettingService.GetSubCertMode()
	if err != nil {
		return err
	}
	listenAddr := net.JoinHostPort(listen, strconv.Itoa(port))
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}

	scheme := "http"
	nginxMode, _ := s.SettingService.GetSubNginx()
	if runtime.GOOS != "windows" && nginxMode {
		// 反向代理终结 TLS:订阅服务自身只跑 HTTP,不加载任何证书。
		// 与 web.go 一样放在最前面短路——它一开,证书字段就全都不该再看。
		scheme = "http (behind nginx)"
	} else if certMode == "acme" {
		// Auto-issue/renew via Let's Encrypt (HTTP-01); fall back to HTTP on
		// any failure so a bad domain or blocked port 80 never breaks the sub server.
		domain, _ := s.SettingService.GetSubDomain()
		email, _ := s.SettingService.GetSubAcmeEmail()
		if domain == "" {
			logger.Warning("Sub ACME enabled but subDomain is empty; serving HTTP")
		} else if tlsConfig, err := network.ACMETLSConfig(domain, email, config.GetCertFolderPath()); err != nil {
			logger.Error("Sub ACME certificate error, falling back to HTTP:", err)
		} else {
			listener = network.NewAutoHttpsListener(listener)
			listener = tls.NewListener(listener, tlsConfig)
			scheme = "https (ACME)"
		}
	} else if certFile != "" || keyFile != "" {
		subDomain, err := s.SettingService.GetSubDomain()
		if err != nil {
			return err
		}
		c, err := network.NewTLSConfig(certFile, keyFile, subDomain)
		if err != nil {
			listener.Close()
			return err
		}
		listener = network.NewAutoHttpsListener(listener)
		listener = tls.NewListener(listener, c)
		scheme = "https"
	}

	logger.Info("Sub server run "+scheme+" on", listener.Addr())
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
