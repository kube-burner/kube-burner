// Copyright 2021 The Kube-burner Authors.
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

package metrics

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cloud-bulldozer/go-commons/indexers"
	log "github.com/sirupsen/logrus"
)

func CreateTarball(indexerConfig indexers.IndexerConfig) error {
	tarball, err := os.Create(indexerConfig.TarballName)
	if err != nil {
		return fmt.Errorf("Could not create tarball file: %v", err)
	}
	gzipWriter := gzip.NewWriter(tarball)
	tarWriter := tar.NewWriter(gzipWriter)
	// defer is LIFO
	defer tarball.Close()
	defer gzipWriter.Close()
	defer tarWriter.Close()
	err = filepath.Walk(indexerConfig.MetricsDirectory, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		hdr, _ := tar.FileInfoHeader(info, info.Name())
		err = tarWriter.WriteHeader(hdr)
		if err != nil {
			return fmt.Errorf("Could not write file header into tarball: %v", err)
		}
		m, _ := os.Open(path)
		defer m.Close()
		_, err = io.Copy(tarWriter, m)
		if err != nil {
			return fmt.Errorf("Could not write file into tarball: %v", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	log.Infof("Metrics tarball generated at %s", tarball.Name())
	return nil
}

func ImportTarball(tarball string, indexer *indexers.Indexer) error {
	log.Infof("Importing tarball: %v", tarball)
	var rawData bytes.Buffer
	tarballFile, err := os.Open(tarball)
	if err != nil {
		return fmt.Errorf("Could not open tarball file: %v", err)
	}
	defer tarballFile.Close()
	gzipReader, err := gzip.NewReader(tarballFile)
	if err != nil {
		return fmt.Errorf("Could not create gzip reader: %v", err)
	}
	tr := tar.NewReader(gzipReader)
	for {
		var metrics []interface{}
		hdr, err := tr.Next()
		// io.EOF returned at the end of file
		if err == io.EOF {
			break
		}
		_, err = io.Copy(&rawData, tr)
		json.Unmarshal(rawData.Bytes(), &metrics)
		rawData.Reset()
		if err != nil {
			return fmt.Errorf("Tarball read error: %v", err)
		}
		log.Infof("Reading metrics from %s", hdr.Name)
		resp, err := (*indexer).Index(metrics, indexers.IndexingOpts{})
		if err != nil {
			return err
		}
		log.Info(resp)
	}
	return nil
}
