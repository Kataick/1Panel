package router

import (
	"context"
	"fmt"
	"github.com/1Panel-dev/1Panel/cmd/server/docs"
	"github.com/1Panel-dev/1Panel/cmd/server/web"
	"github.com/1Panel-dev/1Panel/core/global"
	"github.com/1Panel-dev/1Panel/core/i18n"
	"github.com/1Panel-dev/1Panel/core/middleware"
	rou "github.com/1Panel-dev/1Panel/core/router"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	swaggerfiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
)

var (
	Router *gin.Engine
)

func setWebStatic(rootRouter *gin.RouterGroup) {
	rootRouter.StaticFS("/public", http.FS(web.Favicon))
	rootRouter.Static("/api/v1/images", "./uploads")
	rootRouter.Use(func(c *gin.Context) {
		c.Next()
	})
	rootRouter.GET("/assets/*filepath", func(c *gin.Context) {
		c.Writer.Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%d", 3600))
		staticServer := http.FileServer(http.FS(web.Assets))
		staticServer.ServeHTTP(c.Writer, c.Request)
	})
	rootRouter.GET("/", func(c *gin.Context) {
		staticServer := http.FileServer(http.FS(web.IndexHtml))
		staticServer.ServeHTTP(c.Writer, c.Request)
	})
}

func Routers() *gin.Engine {
	Router = gin.Default()
	Router.Use(middleware.OperationLog())
	if global.CONF.System.IsDemo {
		Router.Use(middleware.DemoHandle())
	}

	Router.Use(i18n.UseI18n())
	Router.Use(middleware.Proxy())

	swaggerRouter := Router.Group("1panel")
	docs.SwaggerInfo.BasePath = "/api/v1"
	swaggerRouter.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerfiles.Handler))
	PublicGroup := Router.Group("")
	{
		PublicGroup.GET("/health", func(c *gin.Context) {
			c.JSON(200, "ok")
		})
		PublicGroup.Use(gzip.Gzip(gzip.DefaultCompression))
		setWebStatic(PublicGroup)
	}
	PrivateGroup := Router.Group("/api/v1")
	PrivateGroup.Use(middleware.WhiteAllow())
	PrivateGroup.Use(middleware.BindDomain())
	PrivateGroup.Use(middleware.GlobalLoading())
	for _, router := range rou.RouterGroupApp {
		router.InitRouter(PrivateGroup)
	}

	// 使用 unix 代理
	sockPath := "/tmp/agent.sock"
	if _, err := os.Stat(sockPath); err != nil {
		panic(err)
	}
	dialUnix := func(proto, addr string) (conn net.Conn, err error) {
		return net.Dial("unix", sockPath)
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialUnix(network, addr)
		},
	}
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = "unix"
		},
		Transport: transport,
	}
	Router.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api") {
			proxy.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}
		c.Next()
	})

	Router.NoRoute(func(c *gin.Context) {
		c.Writer.WriteHeader(http.StatusOK)
		_, _ = c.Writer.Write(web.IndexByte)
		c.Writer.Header().Add("Accept", "text/html")
		c.Writer.Flush()
	})

	return Router
}
