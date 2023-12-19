package util

import (
	"fmt"
	"path"
	"runtime"

	"github.com/cloud-bulldozer/go-commons/version"
	log "github.com/sirupsen/logrus"

	"github.com/spf13/cobra"
)

func SetupCmd(cmd *cobra.Command) {
	cmd.PersistentFlags().String("log-level", "info", "Allowed values: debug, info, warn, error, fatal")
	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version number of kube-burner",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Version:", version.Version)
			fmt.Println("Git Commit:", version.GitCommit)
			fmt.Println("Build Date:", version.BuildDate)
			fmt.Println("Go Version:", version.GoVersion)
			fmt.Println("OS/Arch:", version.OsArch)
		},
	})
}

func ConfigureLogging(cmd *cobra.Command) {
	logLevel, _ := cmd.Flags().GetString("log-level")
	log.SetReportCaller(true)
	formatter := &log.TextFormatter{
		TimestampFormat: "2006-01-02 15:04:05",
		FullTimestamp:   true,
		DisableColors:   true,
		CallerPrettyfier: func(f *runtime.Frame) (function string, file string) {
			return "", fmt.Sprintf("%s:%d", path.Base(f.File), f.Line)
		},
	}
	log.SetFormatter(formatter)
	lvl, err := log.ParseLevel(logLevel)
	if err != nil {
		log.Fatalf("Unknown log level %s", logLevel)
	}
	log.SetLevel(lvl)
}
