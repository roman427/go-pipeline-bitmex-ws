package app

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"app/config"

	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"
	"nhooyr.io/websocket"
	// "github.com/pnvasko/jsonrpc"
)

type HttpControl struct {
	mu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc

	http *http.Server

	// control 	func(interface{}) interface{}
}

func newHttpControl(ctx context.Context) *HttpControl {
	srv := &HttpControl{}
	srv.ctx, srv.cancel = context.WithCancel(ctx)
	cfg := srv.getconfig()

	srv.http = &http.Server{Addr: fmt.Sprintf(":%d", cfg.Http.Port), Handler: srv.newRouter()}

	return srv
}

func (srv *HttpControl) run() error {
	cfg := srv.getconfig()
	logs := srv.getlog()

	go func() {
		defer srv.cancel()
		logs.Info("Http contol listening on: ", cfg.Http.Port)
		if err := srv.http.ListenAndServe(); err != http.ErrServerClosed {
			logs.Fatalf("HttpControl.run. ListenAndServe():", err)
		}
	}()
	return nil
}

func (srv *HttpControl) close() error {
	logs := srv.getlog()

	ctx, cancel := context.WithTimeout(srv.ctx, 5*time.Second)
	defer cancel()

	if err := srv.http.Shutdown(ctx); err != nil {
		logs.Error("HttpControl.close error:", err)
	}
	<-srv.ctx.Done()

	return nil
}

func (srv *HttpControl) getlog() *logrus.Logger {
	eng := srv.ctx.Value("engine").(*Engine)
	return eng.log
}

func (srv *HttpControl) getconfig() *config.Config {
	eng := srv.ctx.Value("engine").(*Engine)
	return eng.config
}

func (srv *HttpControl) newRouter() http.Handler {
	r := chi.NewRouter()

	r.Get("/", srv.serveFiles)
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/", srv.apiInfo)
		r.Get("/rpc", srv.wsUpgrade)
	})

	return r
}

func (srv *HttpControl) apiInfo(w http.ResponseWriter, r *http.Request) {}

func (srv *HttpControl) wsUpgrade(w http.ResponseWriter, r *http.Request) {
	log := srv.getlog()

	ctx, cancel := context.WithCancel(r.Context())
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Error("HttpControl.wsUpgrade error: ", err)
	}
	defer func() {
		cancel()
		if err := conn.Close(websocket.StatusInternalError, "the sky is falling"); err != nil {
			log.Error("HttpControl.wsUpgrade close websocketerror: ", err)
		}
	}()

	fmt.Println(ctx)
}

func (srv *HttpControl) serveFiles(w http.ResponseWriter, r *http.Request) {
	cfg := srv.getconfig()
	fmt.Println(r.URL.Path)

	var path string
	if r.URL.Path == "/" {
		path = fmt.Sprintf("%s/%s", cfg.Http.StaticPath, "index.html")
	} else {
		path = fmt.Sprintf("%s/%s", cfg.Http.StaticPath, r.URL.Path)
	}
	fmt.Println(path)
	http.ServeFile(w, r, path)
}
