// Copyright 2020 The Kube-burner Authors.
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

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rsevilla87/kube-burner/log"
	"github.com/rsevilla87/kube-burner/pkg/burner"
	"github.com/rsevilla87/kube-burner/pkg/indexers"
	"github.com/rsevilla87/kube-burner/pkg/measurements"
	"github.com/rsevilla87/kube-burner/pkg/prometheus"
	"github.com/rsevilla87/kube-burner/pkg/util"

	"github.com/spf13/cobra"
)

var binName = filepath.Base(os.Args[0])

var completionCmd = &cobra.Command{
	Use:   "completion",
	Short: "Generates completion scripts for bash shell",
	Long: `To load completion run
	. <(kube-burner completion)
	To configure your bash shell to load completions for each session execute:

	# kube-burner completion > /etc/bash_completion.d/kube-burner
	`,
	Args: cobra.MaximumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		return rootCmd.GenBashCompletion(os.Stdout)
	},
}

func initCmd() *cobra.Command {
	var c, url, metricsProfile string
	var username, password, uuid, token string
	var skipTLSVerify bool
	var prometheusStep time.Duration
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Launch benchmark",
		Args:  cobra.MaximumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			log.Info("🔥 Starting kube-burner")
			var p *prometheus.Prometheus
			_, err := os.Stat(c)
			if os.IsNotExist(err) {
				log.Fatalf(err.Error())
			}
			if url != "" {
				p, err = prometheus.NewPrometheusClient(url, token, username, password, metricsProfile, uuid, skipTLSVerify, prometheusStep)
				if err != nil {
					log.Fatal(err)
				}
			}
			steps(uuid, c, p)
		},
	}
	cmd.Flags().StringVarP(&c, "config", "c", "", "Config file path")
	cmd.Flags().StringVar(&uuid, "uuid", "", "Benchmark UUID")
	cmd.Flags().StringVarP(&url, "prometheus-url", "u", "", "Prometheus URL")
	cmd.Flags().StringVarP(&token, "token", "t", "", "Prometheus Bearer token")
	cmd.Flags().StringVar(&username, "username", "", "Prometheus username for authentication")
	cmd.Flags().StringVarP(&password, "password", "p", "", "Prometheus password for basic authentication")
	cmd.Flags().StringVarP(&metricsProfile, "metrics-profile", "m", "metrics.yaml", "Metrics profile file")
	cmd.Flags().BoolVar(&skipTLSVerify, "skip-tls-verify", true, "Verify prometheus TLS certificate")
	cmd.Flags().DurationVarP(&prometheusStep, "step", "s", 30*time.Second, "Prometheus step size")
	cmd.MarkFlagFilename(c)
	cmd.MarkFlagFilename(metricsProfile)
	cmd.MarkFlagRequired("config")
	cmd.MarkFlagRequired("uuid")
	return cmd
}

func destroyCmd() *cobra.Command {
	var c, uuid string
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Destroy old namespaces labeled with the given UUID.",
		Args:  cobra.MaximumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			selector := util.NewSelector()
			burner.ReadConfig(c)
			selector.Configure("", fmt.Sprintf("kube-burner-uuid=%s", uuid), "")
			if err := burner.CleanupNamespaces(burner.ClientSet, selector); err != nil {
				log.Fatal(err)
			}
		},
	}
	cmd.Flags().StringVar(&uuid, "uuid", "", "UUID")
	cmd.Flags().StringVarP(&c, "config", "c", "", "Config file path")
	cmd.MarkFlagRequired("config")
	cmd.MarkFlagRequired("uuid")
	return cmd
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   binName,
	Short: "Stress a kubernetes cluster",
	Long: `kube-burner is a tool that aims to stress a kubernetes cluster.
	
It doesn’t only provide some similar features as other tools like cluster-loader, but also
adds other features such as a simplified simplified usage, metrics collection and indexing capabilities`,
}

func Execute() {
	rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(
		initCmd(),
		destroyCmd(),
		completionCmd,
	)
	for _, c := range rootCmd.Commands() {
		logLevel := c.Flags().String("log-level", "info", "Allowed values: debug, info, warn, error, fatal")
		c.PreRun = func(cmd *cobra.Command, args []string) {
			log.Infof("Setting log level to %s", *logLevel)
			log.SetLogLevel(*logLevel)
		}
	}
	cobra.OnInitialize()
}

func steps(uuid, config string, p *prometheus.Prometheus) {
	var indexer *indexers.Indexer
	executorList := burner.NewExecutorList(config, uuid)
	if burner.Cfg.GlobalConfig.IndexerConfig.Enabled {
		indexer = indexers.NewIndexer(burner.Cfg.GlobalConfig.IndexerConfig)
	}
	for _, job := range executorList {
		log.Infof("Triggering job: %s with UUID %s", job.Config.Name, uuid)
		job.Cleanup()
		measurements.NewMeasurementFactory(burner.ClientSet, burner.Cfg.GlobalConfig, job.Config, uuid, indexer)
		measurements.Register(burner.Cfg.GlobalConfig.Measurements)
		measurements.Start()
		// Run execution
		job.Run()
		log.Infof("Job %s took %.2f seconds", job.Config.Name, job.End.Sub(job.Start).Seconds())
		if p != nil {
			log.Info("🔍 Scraping prometheus metrics")
			if err := p.ScrapeMetrics(job.Start, job.End, burner.Cfg, job.Config.Name, indexer); err != nil {
				log.Error(err)
			}
		}
		measurements.Stop()
		measurements.Index()
		if job.Config.JobPause > 0 {
			log.Infof("Pausing for %d milliseconds before next job", job.Config.JobPause)
			time.Sleep(time.Millisecond * time.Duration(job.Config.JobPause))
		}
	}
}
