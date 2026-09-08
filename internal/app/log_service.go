package app

import "log/slog"

// LogService lets the frontend forward console output to the Go log, which is
// the only place we can see it when running the packaged binary.
type LogService struct{}

func (LogService) ServiceName() string { return "LogService" }

func (LogService) Log(level string, msg string) {
	switch level {
	case "error":
		slog.Error("frontend: " + msg)
	case "warn":
		slog.Warn("frontend: " + msg)
	default:
		slog.Info("frontend: " + msg)
	}
}
