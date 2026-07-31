package util

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Log is the global application logger, initialized by InitLogger.
var Log *zap.Logger

// InitLogger initializes the global Log with JSON output at the given level.
func InitLogger(level string) {
	var lvl zapcore.Level
	switch level {
	case "debug":
		lvl = zapcore.DebugLevel
	case "warn":
		lvl = zapcore.WarnLevel
	case "error":
		lvl = zapcore.ErrorLevel
	default:
		lvl = zapcore.InfoLevel
	}

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(os.Stdout),
		lvl,
	)

	Log = zap.New(core, zap.AddCallerSkip(1))
}

// LogError logs an error message with the given error and fields, if Log has
// been initialized.
func LogError(msg string, err error, fields ...zap.Field) {
	if Log != nil {
		allFields := append(fields, zap.Error(err))
		Log.Error(msg, allFields...)
	}
}

// LogInfo logs an informational message with the given fields, if Log has
// been initialized.
func LogInfo(msg string, fields ...zap.Field) {
	if Log != nil {
		Log.Info(msg, fields...)
	}
}

// LogDebug logs a debug-level message with the given fields, if Log has been
// initialized.
func LogDebug(msg string, fields ...zap.Field) {
	if Log != nil {
		Log.Debug(msg, fields...)
	}
}
