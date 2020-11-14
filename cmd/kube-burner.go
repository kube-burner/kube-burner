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

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/burner"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"k8s.io/client-go/kubernetes"

	"github.com/cloud-bulldozer/kube-burner/pkg/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"

	"github.com/spf13/cobra"
)

var binName = filepath.Base(os.Args[0])

var completionCmd = &cobra.Command{
	Use:   "completion",
	Short: "Generates completion scripts for bash shell",
	Long: `To load completion in the current shell run
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
	var url, metricsProfile, configFile string
	var username, password, uuid, token string
	var skipTLSVerify bool
	var prometheusStep time.Duration
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Launch benchmark",
		Args:  cobra.MaximumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			log.Infof("🔥 Starting kube-burner with UUID %s", uuid)
			err := config.Parse(configFile, true)
			if err != nil {
				log.Fatalf("Error parsing configuration: %s", err)
			}
			var p *prometheus.Prometheus
			if url != "" {
				p, err = prometheus.NewPrometheusClient(url, token, username, password, metricsProfile, uuid, skipTLSVerify, prometheusStep)
				if err != nil {
					log.Fatalf("Error setting up Prometheus client: %s", err)
				}
			}
			steps(uuid, p, prometheusStep)
		},
	}
	cmd.Flags().StringVar(&uuid, "uuid", "", "Benchmark UUID")
	cmd.Flags().StringVarP(&url, "prometheus-url", "u", "", "Prometheus URL")
	cmd.Flags().StringVarP(&token, "token", "t", "", "Prometheus Bearer token")
	cmd.Flags().StringVar(&username, "username", "", "Prometheus username for authentication")
	cmd.Flags().StringVarP(&password, "password", "p", "", "Prometheus password for basic authentication")
	cmd.Flags().StringVarP(&metricsProfile, "metrics-profile", "m", "metrics.yaml", "Metrics profile file")
	cmd.Flags().BoolVar(&skipTLSVerify, "skip-tls-verify", true, "Verify prometheus TLS certificate")
	cmd.Flags().DurationVarP(&prometheusStep, "step", "s", 30*time.Second, "Prometheus step size")
	cmd.Flags().StringVarP(&configFile, "config", "c", "", "Config file path")
	cmd.MarkFlagFilename("config")
	cmd.MarkFlagRequired("config")
	cmd.MarkFlagFilename("metrics-profile")
	return cmd
}

func destroyCmd() *cobra.Command {
	var uuid, configFile string
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Destroy old namespaces labeled with the given UUID.",
		Args:  cobra.MaximumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			if configFile != "" {
				err := config.Parse(configFile, false)
				if err != nil {
					log.Fatalf("Error parsing configuration: %s", err)
				}
			}
			selector := util.NewSelector()
			selector.Configure("", fmt.Sprintf("kube-burner-uuid=%s", uuid), "")
			restConfig, err := config.GetRestConfig(0, 0)
			if err != nil {
				log.Fatalf("Error creating restConfig for kube-burner: %s", err)
			}
			clientSet := kubernetes.NewForConfigOrDie(restConfig)
			burner.CleanupNamespaces(clientSet, selector)
		},
	}
	cmd.Flags().StringVar(&uuid, "uuid", "", "UUID")
	return cmd
}

func indexCmd() *cobra.Command {
	var url, metricsProfile, configFile string
	var start, end int64
	var username, password, uuid, token string
	var skipTLSVerify bool
	var prometheusStep time.Duration
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Index metrics from the given time range",
		Args:  cobra.MaximumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			err := config.Parse(configFile, false)
			if err != nil {
				log.Fatalf("Error parsing configuration: %s", err)
			}
			var indexer *indexers.Indexer
			if config.ConfigSpec.GlobalConfig.IndexerConfig.Enabled {
				indexer = indexers.NewIndexer()
			} else {
				log.Fatal("Indexing is disabled in the configuration")
			}
			p, err := prometheus.NewPrometheusClient(url, token, username, password, metricsProfile, uuid, skipTLSVerify, prometheusStep)
			if err != nil {
				log.Fatalf("Error setting up Prometheus client: %s", err)
			}
			startTime := time.Unix(start, 0)
			endTime := time.Unix(end, 0)
			log.Infof("Indexing metrics with UUID %s", uuid)
			if err := p.ScrapeMetrics(startTime, endTime, indexer); err != nil {
				log.Error(err)
			}
		},
	}
	cmd.Flags().StringVar(&uuid, "uuid", "", "Benchmark UUID")
	cmd.Flags().StringVarP(&url, "prometheus-url", "u", "", "Prometheus URL")
	cmd.Flags().StringVarP(&token, "token", "t", "", "Prometheus Bearer token")
	cmd.Flags().StringVar(&username, "username", "", "Prometheus username for authentication")
	cmd.Flags().StringVarP(&password, "password", "p", "", "Prometheus password for basic authentication")
	cmd.Flags().StringVarP(&metricsProfile, "metrics-profile", "m", "metrics.yaml", "Metrics profile file")
	cmd.Flags().BoolVar(&skipTLSVerify, "skip-tls-verify", true, "Verify prometheus TLS certificate")
	cmd.Flags().DurationVarP(&prometheusStep, "step", "s", 30*time.Second, "Prometheus step size")
	cmd.Flags().Int64VarP(&start, "start", "", time.Now().Unix()-3600, "Epoch start time")
	cmd.Flags().Int64VarP(&end, "end", "", time.Now().Unix(), "Epoch end time")
	cmd.Flags().StringVarP(&configFile, "config", "c", "", "Config file path")
	cmd.MarkFlagFilename("metrics-profile")
	cmd.MarkFlagRequired("prometheus-url")
	cmd.MarkFlagFilename("config")
	cmd.MarkFlagRequired("config")
	return cmd
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   binName,
	Short: "Burn a kubernetes cluster",
	Long: `Kube-burner 🔥

Tool aimed at stressing a kubernetes cluster by creating or deleting lot of objects.`,
}

// Execute executes rootCmd
func Execute() {
	rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(
		initCmd(),
		destroyCmd(),
		indexCmd(),
	)
	for _, c := range rootCmd.Commands() {
		logLevel := c.Flags().String("log-level", "info", "Allowed values: debug, info, warn, error, fatal")
		c.PreRun = func(cmd *cobra.Command, args []string) {
			log.Infof("Setting log level to %s", *logLevel)
			log.SetLogLevel(*logLevel)
		}
		c.MarkFlagRequired("uuid")
	}
	rootCmd.AddCommand(completionCmd)
	cobra.OnInitialize()
}

func steps(uuid string, p *prometheus.Prometheus, prometheusStep time.Duration) {
	start := time.Now().UTC()
	verification := true
	var rc int
	var indexer *indexers.Indexer
	if config.ConfigSpec.GlobalConfig.IndexerConfig.Enabled {
		indexer = indexers.NewIndexer()
	}
	for _, job := range burner.NewExecutorList(uuid) {
		// Run execution
		switch job.Config.JobType {
		case config.CreationJob:
			job.Cleanup()
			measurements.NewMeasurementFactory(burner.RestConfig, job.Config, uuid, indexer)
			measurements.Register()
			measurements.Start()
			job.RunCreateJob()
			if job.Config.VerifyObjects {
				verification = job.Verify()
				// If verification failed and ErrorOnVerify is enabled. Exit with error, otherwise continue
				if !verification && job.Config.ErrorOnVerify {
					log.Fatal("Object verification failed. Exiting")
				}
			}
			// We stop and index measurements per job
			measurements.Stop()
			// Verification failed
			if job.Config.VerifyObjects && !verification {
				log.Error("Object verification failed")
				rc = 1
			}
		case config.DeletionJob:
			job.RunDeleteJob()
		}
		elapsedTime := job.End.Sub(job.Start).Seconds()
		log.Infof("Job %s took %.2f seconds", job.Config.Name, elapsedTime)
		if config.ConfigSpec.GlobalConfig.IndexerConfig.Enabled {
			burner.IndexMetadataInfo(indexer, uuid, elapsedTime, job.Config)
		}
		if job.Config.JobPause > 0 {
			log.Infof("Pausing for %v before next job", job.Config.JobPause)
			time.Sleep(job.Config.JobPause)
		}
	}
	// If prometheus is enabled query metrics from the start of the first job to the end of the last one
	if p != nil {
		log.Infof("Waiting %v extra before scraping prometheus metrics", prometheusStep*4)
		time.Sleep(prometheusStep * 4)
		if err := p.ScrapeMetrics(start, time.Now().UTC(), indexer); err != nil {
			log.Error(err)
		}
	}
	log.Info("👋 Exiting kube-burner")
	os.Exit(rc)
}
