package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"app/config"
	"app/workers"

	"github.com/sirupsen/logrus"
)

type Engine struct {
	config *config.Config

	ctx     context.Context
	basectx context.Context
	cancel  context.CancelFunc

	db   *PgxConnPool
	http *HttpControl

	workers map[string]workers.Worker

	log *logrus.Logger
}

func NewEngine(path string) *Engine {
	var err error
	eng := &Engine{
		config:  config.NewConfig(),
		workers: make(map[string]workers.Worker),
	}
	if err := eng.config.Load(path); err != nil {
		fmt.Println("NewEngine load config error: ", err)
		os.Exit(1)
	}

	eng.log = NewLoger("controler", eng.config.LogPath, eng.config.Debug)
	eng.ctx, eng.cancel = context.WithCancel(context.Background())

	eng.basectx = context.WithValue(eng.ctx, "engine", eng)
	eng.basectx = context.WithValue(eng.basectx, "logger", eng.log)
	eng.basectx = context.WithValue(eng.basectx, "onConnect", eng.onConnect)
	eng.basectx = context.WithValue(eng.basectx, "onDisconnect", eng.onDisconnect)
	eng.basectx = context.WithValue(eng.basectx, "onMessage", eng.onMessage)
	eng.basectx = context.WithValue(eng.basectx, "onError", eng.onError)

	eng.http = newHttpControl(eng.basectx)

	if eng.workers["bitmex"], err = workers.NewBitmexWorker(eng.basectx, &eng.config.Providers.Bitmex, 100); err != nil {
		eng.log.Error("Engine.NewBitmexWorker. error :", err)
		os.Exit(1)
	}

	// fmt.Println("Config: ", eng.config)
	eng.HandleInterrupt()
	return eng
}

func (eng *Engine) GetContext() context.Context {
	return eng.ctx
}

func (eng *Engine) GetDburl() string {
	// return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=verify-ca&pool_max_conns=%d",
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?pool_max_conns=%d",
		eng.config.Db.User, eng.config.Db.Password,
		eng.config.Db.Host, eng.config.Db.Port,
		eng.config.Db.Database, eng.config.Db.MaxConnections)
}

func (eng *Engine) SetPgxDB(db *PgxConnPool) {
	eng.db = db
}

func (eng *Engine) Run() error {

	eng.log.Debug("Engine.Run, start HttpControl service...")
	if err := eng.http.run(); err != nil {
		eng.log.Error("Engine.Run, Error start HttpControl service:", err)
		os.Exit(1)
	}

	wg := sync.WaitGroup{}
	for name, worker := range eng.workers {
		eng.log.Debug("Engine.Shutdown Close", name)
		// worker := *nworker.(*workers.Worker)
		// worker := nworker
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := worker.Run(eng.basectx); err != nil {
				eng.log.Error("Engine.Run error start worker: ", name, err)
			}
		}()
	}
	wg.Wait()

	// type Workers map[string]workers.Worker
	// xworkers := make(workers.Workers)
	// xworkers["bitmex"] = bmw

	/*
		if t := reflect.TypeOf(bmw); t.Kind() == reflect.Ptr {
			fmt.Println( "TypeOf 0:", t.Elem().Name())
		} else {
			fmt.Println( "TypeOf 1:", t.Name())
		}

	*/

	return nil
}

func (eng *Engine) onConnect() {
	eng.log.Info("onConnect")
}

func (eng *Engine) onDisconnect() {
	eng.log.Info("onDisconnect")
}

func (eng *Engine) onMessage(msg []byte) {
	eng.log.Info("onMessage:", string(msg))
}

func (eng *Engine) onError() {
	eng.log.Info("onError")
}

func (eng *Engine) Shutdown() {
	eng.log.Debug("Engine.Shutdown, close HttpControl service...")
	wg := sync.WaitGroup{}
	for name, nworker := range eng.workers {
		eng.log.Debug("Engine.Shutdown Close", name)
		// worker := *nworker.(*workers.Worker)
		// worker := nworker
		wg.Add(1)
		go func() {
			defer wg.Done()
			comlete := make(chan struct{})
			if err := nworker.Close(comlete); err != nil {
				eng.log.Error("Engine.Shutdown error close worker: ", name, err)
			}
			<-comlete
		}()
	}
	wg.Wait()

	if err := eng.http.close(); err != nil {
		eng.log.Error("Engine.Shutdown, error close HttpControl service: ", err)
	}

	eng.cancel()
}

func (eng *Engine) Complete() <-chan struct{} {
	return eng.ctx.Done()
}

func (eng *Engine) HandleInterrupt() {
	eng.log.Debug("Set handle interrupt...")
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		for {
			select {
			case sig := <-c:
				eng.log.Debug("Captured interrupt:", sig)
				eng.Shutdown()
				return
			}
		}
	}()
}
