package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloud-bulldozer/kube-burner/pkg/version"
	"github.com/cloud-bulldozer/kube-burner/pkg/workloads"
	uid "github.com/satori/go.uuid"
	"github.com/spf13/cobra"
)

var cmd = &cobra.Command{
	Use:   binName,
	Short: "kube-burner OpenShift wrapper",
	Long:  `kube-burner-ocp is a kube-burner wrapper meant to be used against OpenShift based clusters and serve as a shortcut to trigger well-known workloads`,
}

var binName = filepath.Base(os.Args[0])

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of kube-burner-ocp",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Version:", version.Version)
		fmt.Println("Git Commit:", version.GitCommit)
		fmt.Println("Build Date:", version.BuildDate)
		fmt.Println("Go Version:", version.GoVersion)
		fmt.Println("OS/Arch:", version.OsArch)
	},
}

var completionCmd = &cobra.Command{
	Use:   "completion",
	Short: "Generates completion scripts for bash shell",
	Long: `To load completion in the current shell run
. <(kube-burner-ocp completion)

To configure your bash shell to load completions for each session execute:

# kube-burner-ocp completion > /etc/bash_completion.d/kube-burner-ocp
	`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.GenBashCompletion(os.Stdout)
	},
}

func main() {
	var logLevel string
	var wh workloads.WorkloadHelper
	esServer := cmd.PersistentFlags().String("es-server", "", "Elastic Search endpoint")
	esIndex := cmd.PersistentFlags().String("es-index", "", "Elastic Search index")
	qps := cmd.PersistentFlags().Int("qps", 20, "kube-burner QPS")
	burst := cmd.PersistentFlags().Int("burst", 20, "Kube-burner Burst")
	uuid := cmd.PersistentFlags().String("uuid", uid.NewV4().String(), "Benchmark UUID")
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Kube-burner log level")
	cmd.MarkFlagsRequiredTogether("es-server", "es-index")
	cmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		var envVars map[string]string = map[string]string{
			"ES_SERVER": *esServer,
			"ES_INDEX":  *esIndex,
			"QPS":       fmt.Sprintf("%d", *qps),
			"BURST":     fmt.Sprintf("%d", *burst),
			"UUID":      *uuid,
		}
		wh = workloads.NewWorkloadHelper(logLevel, envVars)
		err := wh.GatherMetadata()
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}
		wh.SetKubeBurnerFlags()
	}
	cmd.Flags().SortFlags = false
	cmd.AddCommand(
		workloads.NewClusterDensity(&wh),
		workloads.NewNodeDensity(&wh),
		completionCmd,
		versionCmd,
	)
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
