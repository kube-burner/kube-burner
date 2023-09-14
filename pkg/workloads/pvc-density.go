package workloads

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var dynamicStorageProvisioners = map[string]string{
	"aws":        "kubernetes.io/aws-ebs",
	"cinder":     "kuberenetes.io/cinder",
	"azure-disk": "kubernetes.io/azure-disk",
	"azure-file": "kubernetes.io/azure-file",
	"gce":        "kubernetes.io/gce-pd",
	"ibm":        "powervs.csi.ibm.com",
	"vsphere":    "kubernetes.io/vsphere-volume",
}

// NewPVCDensity holds pvc-density workload
func NewPVCDensity(wh *WorkloadHelper) *cobra.Command {

	var iterations int
	var storageProvisioners []string
	var claimSize string
	var containerImage string
	provisioner := "aws"

	cmd := &cobra.Command{
		Use:          "pvc-density",
		Short:        "Runs pvc-density workload",
		SilenceUsage: true,
		PreRun: func(cmd *cobra.Command, args []string) {
			wh.Metadata.Benchmark = cmd.Name()
			os.Setenv("JOB_ITERATIONS", fmt.Sprint(iterations))
			os.Setenv("CONTAINER_IMAGE", containerImage)
			os.Setenv("CLAIM_SIZE", fmt.Sprint(claimSize))

			for key := range dynamicStorageProvisioners {
				storageProvisioners = append(storageProvisioners, key)
			}
			re := regexp.MustCompile(`(?sm)^(cinder|azure\-disk|azure\-file|gce|ibm|vsphere|aws)$`)
			if !re.MatchString(provisioner) {
				log.Fatal(fmt.Errorf("%s does not match one of %s", provisioner, storageProvisioners))
			}

			os.Setenv("STORAGE_PROVISIONER", fmt.Sprint(dynamicStorageProvisioners[provisioner]))
		},
		Run: func(cmd *cobra.Command, args []string) {
			wh.run(cmd.Name(), MetricsProfileMap[cmd.Name()])
		},
	}

	cmd.Flags().IntVar(&iterations, "iterations", 0, fmt.Sprintf("%v iterations", iterations))
	cmd.Flags().StringVar(&provisioner, "provisioner", provisioner, fmt.Sprintf(
		"[%s]", strings.Join(storageProvisioners, " ")))
	cmd.Flags().StringVar(&claimSize, "claim-size", "256Mi", "claim-size=256Mi")
	cmd.Flags().StringVar(&containerImage, "container-image", "gcr.io/google_containers/pause:3.1", "Container image")

	return cmd
}
