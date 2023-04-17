package log

import (
	"fmt"
	"path"
	"runtime"

	"github.com/sirupsen/logrus"
)

func init() {
	logrus.SetReportCaller(true)
	formatter := &logrus.TextFormatter{
		TimestampFormat: "2006-01-02 15:04:05",
		FullTimestamp:   true,
		DisableColors:   true,
		CallerPrettyfier: func(f *runtime.Frame) (function string, file string) {
			return "", fmt.Sprintf("%s:%d", path.Base(f.File), f.Line)
		},
	}
	logrus.SetFormatter(formatter)
}
