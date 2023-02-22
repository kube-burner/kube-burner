package util

import (
	"os"

	"github.com/cloud-bulldozer/kube-burner/log"
	"gopkg.in/yaml.v3"
)

func ReadUserMetadata(inputFile string) (map[string]interface{}, error) {
	log.Infof("Reading provided user metadata from %s", inputFile)
	userMetadata := make(map[string]interface{})
	f, err := os.Open(inputFile)
	if err != nil {
		return userMetadata, err
	}
	yamlDec := yaml.NewDecoder(f)
	err = yamlDec.Decode(&userMetadata)
	return userMetadata, err
}
