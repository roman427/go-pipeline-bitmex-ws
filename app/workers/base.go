package workers

import (
	"context"

	jsoniter "github.com/json-iterator/go"
	"github.com/sirupsen/logrus"
)

const (
	maxPacketSize = 16 * 1024 * 1024

	DISCONNECTED = iota
	STARTED
	CONNECTING
	CONNECTED
	RECONNECTING
	INIT
	CLOSED
)

var jsonProcessor = jsoniter.ConfigCompatibleWithStandardLibrary

type BaseWorkers struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (bw *BaseWorkers) getlog() *logrus.Logger {
	log := bw.ctx.Value("logger").(*logrus.Logger)
	return log
}

type ProviderMsg struct {
}

type Worker interface {
	Run(context.Context) error
	Close(chan struct{}) error
}
