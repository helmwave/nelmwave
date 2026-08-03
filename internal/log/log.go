// Package log configures the application-wide zap logger.
//
// Format defaults to a colored console encoder when stderr is a TTY and to
// JSON otherwise (e.g. in CI). Both can be overridden via flags — see New.
package log

import (
	"fmt"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/term"
)

// Format selects the log encoder.
type Format string

const (
	// FormatAuto picks console on a TTY, JSON otherwise.
	FormatAuto Format = "auto"
	// FormatConsole forces the colored console encoder.
	FormatConsole Format = "console"
	// FormatJSON forces the JSON encoder.
	FormatJSON Format = "json"
)

// Options controls logger construction.
type Options struct {
	// Level is one of debug|info|warn|error.
	Level string
	// Format is auto|console|json.
	Format Format
}

// New builds a *zap.Logger from opts. The caller owns the logger and should
// defer logger.Sync().
func New(opts Options) (*zap.Logger, error) {
	level, err := zapcore.ParseLevel(opts.Level)
	if err != nil {
		return nil, fmt.Errorf("parse log level %q: %w", opts.Level, err)
	}

	format := opts.Format
	if format == FormatAuto || format == "" {
		if term.IsTerminal(int(os.Stderr.Fd())) {
			format = FormatConsole
		} else {
			format = FormatJSON
		}
	}

	var encCfg zapcore.EncoderConfig
	var encoder zapcore.Encoder
	switch format {
	case FormatConsole:
		encCfg = zap.NewDevelopmentEncoderConfig()
		encCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encCfg.EncodeTime = zapcore.TimeEncoderOfLayout("15:04:05")
		encoder = zapcore.NewConsoleEncoder(encCfg)
	case FormatJSON:
		encCfg = zap.NewProductionEncoderConfig()
		encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
		encoder = zapcore.NewJSONEncoder(encCfg)
	default:
		return nil, fmt.Errorf("unknown log format %q", opts.Format)
	}

	core := zapcore.NewCore(encoder, zapcore.Lock(os.Stderr), level)
	return zap.New(core), nil
}
