package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx"
	rotatelogs "github.com/lestrrat/go-file-rotatelogs"
	"github.com/sirupsen/logrus"
)

// https://github.com/francoispqt/onelog fast

// Todo Split log: "github.com/regorov/logwriter" "github.com/rifflock/lfshook"
// Help Direct write to file: f, _ := os.OpenFile(log_path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
func NewLoger(name, log_path string, debug bool) *logrus.Logger {
	fmt.Println("\t configure logger...")
	var log = logrus.New()
	log.Formatter = new(logrus.TextFormatter)
	log.Level = logrus.InfoLevel
	if debug {
		log.Level = logrus.DebugLevel
	}
	log.Out = os.Stderr
	fmt.Println("\t configure logger...", log_path, "; debug: ", debug)
	if log_path != "" {
		log.Formatter = new(logrus.JSONFormatter)
		writer, err := rotatelogs.New(
			fmt.Sprintf("%s/%s.%s", log_path, name, "%Y%m%d%H%M"),
			rotatelogs.WithLinkName(fmt.Sprintf("%s/%s", log_path, name)),
			rotatelogs.WithMaxAge(time.Duration(86400)*time.Second),
			rotatelogs.WithRotationTime(time.Duration(604800)*time.Second),
		)
		if err != nil {
			fmt.Println("Error open log file: ", err)
			panic("Error open log file")
		}
		log.Out = writer
	}
	return log
}

type Logger struct {
	l logrus.FieldLogger
}

func NewPgxLogger(l logrus.FieldLogger) *Logger {
	return &Logger{l: l}
}

func (l *Logger) Log(ctx context.Context, level pgx.LogLevel, msg string, data map[string]interface{}) {
	var logger logrus.FieldLogger
	if data != nil {
		logger = l.l.WithFields(data)
	} else {
		logger = l.l
	}

	switch level {
	case pgx.LogLevelTrace:
		logger.WithField("PGX_LOG_LEVEL", level).Debug(msg)
	case pgx.LogLevelDebug:
		logger.Debug(msg)
	case pgx.LogLevelInfo:
		logger.Info(msg)
	case pgx.LogLevelWarn:
		logger.Warn(msg)
	case pgx.LogLevelError:
		logger.Error(msg)
	default:
		logger.WithField("INVALID_PGX_LOG_LEVEL", level).Error(msg)
	}
}
