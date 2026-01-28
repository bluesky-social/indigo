package cliutil

import (
	"io"

	ipfslog "github.com/ipfs/go-log/v2"
	"go.uber.org/zap/zapcore"
)

func SetIpfsWriter(out io.Writer, format string, level string) {
	var ze zapcore.Encoder
	switch format {
	case "json":
		ze = zapcore.NewJSONEncoder(zapcore.EncoderConfig{})
	case "text":
		ze = zapcore.NewConsoleEncoder(zapcore.EncoderConfig{})
	default:
		ze = zapcore.NewConsoleEncoder(zapcore.EncoderConfig{})
	}
	var zl zapcore.LevelEnabler
	switch level {
	case "debug":
		zl = zapcore.DebugLevel
	case "info":
		zl = zapcore.InfoLevel
	case "warn":
		zl = zapcore.WarnLevel
	case "error":
		zl = zapcore.ErrorLevel
	default:
		zl = zapcore.InfoLevel
	}
	nc := zapcore.NewCore(ze, zapcore.AddSync(out), zl)
	ipfslog.SetPrimaryCore(nc)
}
