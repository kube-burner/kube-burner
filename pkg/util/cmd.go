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
	"io"
	"os"
	"path"
	"runtime"
	"time"

	"github.com/cloud-bulldozer/go-commons/v2/version"
	log "github.com/sirupsen/logrus"

	"github.com/spf13/cobra"
)

// Bootstraps kube-burner cmd with some common flags
func SetupCmd(cmd *cobra.Command) {
	cmd.PersistentFlags().String("log-level", "info", "Allowed values: debug, info, warn, error, fatal")

	// Add version check to the root command's PersistentPreRun
	originalPersistentPreRun := cmd.PersistentPreRun
	cmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		// Run the original PersistentPreRun if it exists
		if originalPersistentPreRun != nil {
			originalPersistentPreRun(cmd, args)
		}

		// Check for updates
		if cmd.Use != "version" {
			// Run version check in background
			done := make(chan bool, 1)
			go func() {
				defer func() { done <- true }()
				if err := CheckLatestVersion(); err != nil {
					// Silently ignore errors
				}
			}()

			// Wait briefly for the version check to complete, but don't block indefinitely
			select {
			case <-done:
				// Version check completed
			case <-time.After(2 * time.Second):
				// Continue without waiting longer
			}
		}
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version number of kube-burner",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Version:", version.Version)
			fmt.Println("Git Commit:", version.GitCommit)
			fmt.Println("Build Date:", version.BuildDate)
			fmt.Println("Go Version:", version.GoVersion)
			fmt.Println("OS/Arch:", version.OsArch)
		},
	}
	cmd.AddCommand(versionCmd)
}

// Configures kube-burner's file logging
func SetupFileLogging(uuid string) {
	logFileName := fmt.Sprintf("kube-burner-%s.log", uuid)
	file, err := os.Create(logFileName)
	if err != nil {
		log.Fatalf("Failed to create log file: %v", err)
	}
	mw := io.MultiWriter(os.Stdout, file)
	log.SetOutput(mw)
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
