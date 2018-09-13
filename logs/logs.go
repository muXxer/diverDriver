package logs

import (
	"os"

	"github.com/op/go-logging"
)

var LOG_FORMAT = "%{color}[%{level:.4s}] %{time:15:04:05.000000} %{id:06x} [%{shortpkg}] %{longfunc} -> %{color:reset}%{message}"
var Log = logging.MustGetLogger("diverDriver")

func Setup() {
	backend1 := logging.NewLogBackend(os.Stdout, "", 0)
	logging.SetFormatter(logging.MustStringFormatter(LOG_FORMAT))
	logging.SetBackend(backend1)
}

func SetLogLevel(logLevel string) {
	level, err := logging.LogLevel(logLevel)
	if err == nil {
		logging.SetLevel(level, "diverDriver")
	} else {
		Log.Warningf("Could not set log level to %v: %v", logLevel, err)
		Log.Warning("Using default log level")
	}
}
