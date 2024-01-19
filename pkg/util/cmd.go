// Copyright 2024 The Kube-burner Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"fmt"
	"path"
	"runtime"

	"github.com/cloud-bulldozer/go-commons/version"
	log "github.com/sirupsen/logrus"

	"github.com/spf13/cobra"
)

// Bootstraps kube-burner cmd with some common flags
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

// Configures kube-burner's logging level
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
