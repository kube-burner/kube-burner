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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/alerting"
	"github.com/cloud-bulldozer/kube-burner/pkg/burner"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/version"

	"github.com/cloud-bulldozer/kube-burner/pkg/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"

	uid "github.com/satori/go.uuid"
	"github.com/spf13/cobra"
)

var binName = filepath.Base(os.Args[0])

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   binName,
	Short: "Burn a kubernetes cluster",
	Long: `Kube-burner 🔥

Tool aimed at stressing a kubernetes cluster by creating or deleting lots of objects.`,
}

// versionCmd represents the version command
var versionCmd = &cobra.Command{
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

var completionCmd = &cobra.Command{
	Use:   "completion",
	Short: "Generates completion scripts for bash shell",
	Long: `To load completion in the current shell run
. <(kube-burner completion)

To configure your bash shell to load completions for each session execute:

# kube-burner completion > /etc/bash_completion.d/kube-burner
	`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return rootCmd.GenBashCompletion(os.Stdout)
	},
}

func initCmd() *cobra.Command {
	var err error
	var url, metricsProfile, alertProfile, configFile string
	var username, password, uuid, token, configMap, namespace string
	var skipTLSVerify bool
	var prometheusStep time.Duration
	var prometheusClient *prometheus.Prometheus
	var alertM *alerting.AlertManager
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Launch benchmark",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			log.Infof("🔥 Starting kube-burner (%s@%s) with UUID %s", version.Version, version.GitCommit, uuid)
			if configMap != "" {
				if configFile != "" {
					log.Fatal("The flags --config and --configmap can't be specified together")
				}
			}
			if configMap == "" && configFile == "" {
				log.Fatal("Either --configmap or --config flags are required")
			}
			if configMap != "" {
				metricsProfile, alertProfile, err = config.FetchConfigMap(configMap, namespace)
				if err != nil {
					log.Fatal(err.Error())
				}
				// We assume configFile is config.yml
				configFile = "config.yml"
			}
			err = config.Parse(configFile, true)
			if err != nil {
				log.Fatal(err.Error())
			}
			if url != "" {
				prometheusClient, err = prometheus.NewPrometheusClient(url, token, username, password, uuid, skipTLSVerify, prometheusStep)
				if err != nil {
					log.Fatal(err)
				}
				// If indexer is enabled or writeTofile is enabled we read the profile
				if config.ConfigSpec.GlobalConfig.IndexerConfig.Enabled || config.ConfigSpec.GlobalConfig.WriteToFile {
					if err := prometheusClient.ReadProfile(metricsProfile); err != nil {
						log.Fatal(err)
					}
				}
				if alertProfile != "" {
					if alertM, err = alerting.NewAlertManager(alertProfile, prometheusClient); err != nil {
						log.Fatalf("Error creating alert manager: %s", err)
					}
				}
			}
			steps(uuid, prometheusClient, alertM)
		},
	}
	cmd.Flags().StringVar(&uuid, "uuid", uid.NewV4().String(), "Benchmark UUID")
	cmd.Flags().StringVarP(&url, "prometheus-url", "u", "", "Prometheus URL")
	cmd.Flags().StringVarP(&token, "token", "t", "", "Prometheus Bearer token")
	cmd.Flags().StringVar(&username, "username", "", "Prometheus username for authentication")
	cmd.Flags().StringVarP(&password, "password", "p", "", "Prometheus password for basic authentication")
	cmd.Flags().StringVarP(&metricsProfile, "metrics-profile", "m", "metrics.yml", "Metrics profile file or URL")
	cmd.Flags().StringVarP(&alertProfile, "alert-profile", "a", "", "Alert profile file or URL")
	cmd.Flags().BoolVar(&skipTLSVerify, "skip-tls-verify", true, "Verify prometheus TLS certificate")
	cmd.Flags().DurationVarP(&prometheusStep, "step", "s", 30*time.Second, "Prometheus step size")
	cmd.Flags().StringVarP(&configFile, "config", "c", "", "Config file path or URL")
	cmd.Flags().StringVarP(&configMap, "configmap", "", "", "Configmap holding all the configuration: config.yml, metrics.yml and alerts.yml. metrics and alerts are optional")
	cmd.Flags().StringVarP(&namespace, "namespace", "", "default", "Namespace where the configmap is")
	cmd.Flags().SortFlags = false
	return cmd
}

func destroyCmd() *cobra.Command {
	var uuid, configFile string
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Destroy old namespaces labeled with the given UUID.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			if configFile != "" {
				err := config.Parse(configFile, false)
				if err != nil {
					log.Fatal(err.Error())
				}
			}
			selector := util.NewSelector()
			selector.Configure("", fmt.Sprintf("kube-burner-uuid=%s", uuid), "")
			clientSet, _, err := config.GetClientSet(0, 0)
			if err != nil {
				log.Fatalf("Error creating clientSet: %s", err)
			}
			burner.CleanupNamespaces(clientSet, selector)
		},
	}
	cmd.Flags().StringVar(&uuid, "uuid", "", "UUID")
	cmd.MarkFlagRequired("uuid")
	return cmd
}

func indexCmd() *cobra.Command {
	var url, metricsProfile, configFile string
	var start, end int64
	var username, password, uuid, token string
	var skipTLSVerify bool
	var prometheusStep time.Duration
	var indexer *indexers.Indexer
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Index kube-burner metrics",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			err := config.Parse(configFile, false)
			if err != nil {
				log.Fatal(err.Error())
			}
			if config.ConfigSpec.GlobalConfig.IndexerConfig.Enabled {
				indexer, err = indexers.NewIndexer()
				if err != nil {
					log.Fatal(err.Error())
				}
			}
			p, err := prometheus.NewPrometheusClient(url, token, username, password, uuid, skipTLSVerify, prometheusStep)
			if err != nil {
				log.Fatal(err)
			}
			if err := p.ReadProfile(metricsProfile); err != nil {
				log.Fatal(err)
			}
			startTime := time.Unix(start, 0)
			endTime := time.Unix(end, 0)
			log.Infof("Indexing metrics with UUID %s", uuid)
			if err := p.ScrapeMetrics(startTime, endTime, indexer); err != nil {
				log.Error(err)
			}
			if config.ConfigSpec.GlobalConfig.WriteToFile && config.ConfigSpec.GlobalConfig.CreateTarball {
				err = prometheus.CreateTarball(config.ConfigSpec.GlobalConfig.MetricsDirectory)
				if err != nil {
					log.Fatal(err.Error())
				}
			}
		},
	}
	cmd.Flags().StringVar(&uuid, "uuid", uid.NewV4().String(), "Benchmark UUID")
	cmd.Flags().StringVarP(&url, "prometheus-url", "u", "", "Prometheus URL")
	cmd.Flags().StringVarP(&token, "token", "t", "", "Prometheus Bearer token")
	cmd.Flags().StringVar(&username, "username", "", "Prometheus username for authentication")
	cmd.Flags().StringVarP(&password, "password", "p", "", "Prometheus password for basic authentication")
	cmd.Flags().StringVarP(&metricsProfile, "metrics-profile", "m", "metrics.yml", "Metrics profile file")
	cmd.Flags().BoolVar(&skipTLSVerify, "skip-tls-verify", true, "Verify prometheus TLS certificate")
	cmd.Flags().DurationVarP(&prometheusStep, "step", "s", 30*time.Second, "Prometheus step size")
	cmd.Flags().Int64VarP(&start, "start", "", time.Now().Unix()-3600, "Epoch start time")
	cmd.Flags().Int64VarP(&end, "end", "", time.Now().Unix(), "Epoch end time")
	cmd.Flags().StringVarP(&configFile, "config", "c", "", "Config file path or URL")
	cmd.MarkFlagRequired("prometheus-url")
	cmd.MarkFlagRequired("config")
	cmd.Flags().SortFlags = false
	return cmd
}

func importCmd() *cobra.Command {
	var configFile, tarball string
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import metrics tarball",
		Run: func(cmd *cobra.Command, args []string) {
			err := config.Parse(configFile, false)
			if err != nil {
				log.Fatal(err.Error())
			}
			indexer, err := indexers.NewIndexer()
			if err != nil {
				log.Fatal(err.Error())
			}
			err = prometheus.ImportTarball(tarball, config.ConfigSpec.GlobalConfig.IndexerConfig.DefaultIndex, indexer)
			if err != nil {
				log.Fatal(err.Error())
			}
		},
	}
	cmd.Flags().StringVarP(&configFile, "config", "c", "", "Config file path or URL")
	cmd.Flags().StringVar(&tarball, "tarball", "", "Metrics tarball file")
	cmd.MarkFlagRequired("config")
	return cmd
}

func alertCmd() *cobra.Command {
	var url, alertProfile string
	var start, end int64
	var username, password, uuid, token string
	var skipTLSVerify bool
	var alertM *alerting.AlertManager
	var prometheusStep time.Duration
	cmd := &cobra.Command{
		Use:   "check-alerts",
		Short: "Evaluate alerts for the given time range",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			p, err := prometheus.NewPrometheusClient(url, token, username, password, uuid, skipTLSVerify, prometheusStep)
			if err != nil {
				log.Fatal(err)
			}
			startTime := time.Unix(start, 0)
			endTime := time.Unix(end, 0)
			if alertM, err = alerting.NewAlertManager(alertProfile, p); err != nil {
				log.Fatalf("Error creating alert manager: %s", err)
			}
			rc := alertM.Evaluate(startTime, endTime)
			log.Info("👋 Exiting kube-burner")
			os.Exit(rc)
		},
	}
	cmd.Flags().StringVarP(&url, "prometheus-url", "u", "", "Prometheus URL")
	cmd.Flags().StringVarP(&token, "token", "t", "", "Prometheus Bearer token")
	cmd.Flags().StringVar(&username, "username", "", "Prometheus username for authentication")
	cmd.Flags().StringVarP(&password, "password", "p", "", "Prometheus password for basic authentication")
	cmd.Flags().StringVarP(&alertProfile, "alert-profile", "a", "alerts.yaml", "Alert profile file or URL")
	cmd.Flags().BoolVar(&skipTLSVerify, "skip-tls-verify", true, "Verify prometheus TLS certificate")
	cmd.Flags().DurationVarP(&prometheusStep, "step", "s", 30*time.Second, "Prometheus step size")
	cmd.Flags().Int64VarP(&start, "start", "", time.Now().Unix()-3600, "Epoch start time")
	cmd.Flags().Int64VarP(&end, "end", "", time.Now().Unix(), "Epoch end time")
	cmd.MarkFlagRequired("prometheus-url")
	cmd.MarkFlagRequired("alert-profile")
	cmd.Flags().SortFlags = false
	return cmd
}

func steps(uuid string, p *prometheus.Prometheus, alertM *alerting.AlertManager) {
	verification := true
	var rc int
	var err error
	var indexer *indexers.Indexer
	if config.ConfigSpec.GlobalConfig.IndexerConfig.Enabled {
		indexer, err = indexers.NewIndexer()
		if err != nil {
			log.Fatal(err.Error())
		}
	}
	_, restConfig, err := config.GetClientSet(0, 0)
	if err != nil {
		log.Fatalf("Error creating k8s clientSet: %s", err)
	}
	measurements.NewMeasurementFactory(restConfig, uuid, indexer)
	jobList := burner.NewExecutorList(uuid)
	// Iterate through job list
	for jobPosition, job := range jobList {
		jobList[jobPosition].Start = time.Now().UTC()
		log.Infof("Triggering job: %s", job.Config.Name)
		measurements.SetJobConfig(&job.Config)
		switch job.Config.JobType {
		case config.CreationJob:
			job.Cleanup()
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
			rc = measurements.Stop()
			// Verification failed
			if job.Config.VerifyObjects && !verification {
				log.Error("Object verification failed")
				rc = 1
			}
		case config.DeletionJob:
			job.RunDeleteJob()
		}
		if job.Config.JobPause > 0 {
			log.Infof("Pausing for %v before finishing job", job.Config.JobPause)
			time.Sleep(job.Config.JobPause)
		}
		jobList[jobPosition].End = time.Now().UTC()
		elapsedTime := jobList[jobPosition].End.Sub(jobList[jobPosition].Start).Seconds()
		log.Infof("Job %s took %.2f seconds", job.Config.Name, elapsedTime)
	}
	if config.ConfigSpec.GlobalConfig.IndexerConfig.Enabled {
		for _, job := range jobList {
			elapsedTime := job.End.Sub(job.Start).Seconds()
			err := burner.IndexMetadataInfo(indexer, uuid, elapsedTime, job.Config, job.Start)
			if err != nil {
				log.Errorf(err.Error())
			}
		}
	}
	if p != nil {
		log.Infof("Waiting %v extra before scraping prometheus", p.Step*2)
		time.Sleep(p.Step * 2)
		// Update end time of last job
		jobList[len(jobList)-1].End = time.Now().UTC()
		// If alertManager is configured
		if alertM != nil {
			log.Infof("Evaluating alerts")
			rc = alertM.Evaluate(jobList[0].Start, jobList[len(jobList)-1].End)
		}
		// If prometheus is enabled query metrics from the start of the first job to the end of the last one
		if len(p.MetricProfile) > 0 {
			if err := p.ScrapeJobsMetrics(jobList, indexer); err != nil {
				log.Fatal(err.Error())
			}
			if config.ConfigSpec.GlobalConfig.WriteToFile && config.ConfigSpec.GlobalConfig.CreateTarball {
				err = prometheus.CreateTarball(config.ConfigSpec.GlobalConfig.MetricsDirectory)
				if err != nil {
					log.Fatal(err.Error())
				}
			}
		}
	}
	log.Infof("Finished execution with UUID: %s", uuid)
	log.Info("👋 Exiting kube-burner")
	os.Exit(rc)
}

// executes rootCmd
func main() {
	rootCmd.AddCommand(
		versionCmd,
		initCmd(),
		destroyCmd(),
		indexCmd(),
		alertCmd(),
		importCmd(),
	)
	logLevel := rootCmd.PersistentFlags().String("log-level", "info", "Allowed values: trace, debug, info, warn, error, fatal")
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		log.SetLogLevel(*logLevel)
	}
	rootCmd.AddCommand(completionCmd)
	cobra.OnInitialize()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
