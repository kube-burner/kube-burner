package reader

import (
	"fmt"
	"io"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/util"
	"github.com/kube-burner/kube-burner/pkg/util/fileutils"
	"github.com/kube-burner/kube-burner/pkg/util/overlay"
)

type Reader struct {
	BaseReader         io.Reader
	OverlayFilePaths   []string
	EmbedCfg           *fileutils.EmbedConfiguration
	UserDataFileReader io.Reader
	AllowMissingKeys   bool
	AdditionalVars     map[string]any
	processedContent   []byte
	readOffset         int
}

// NewReader creates a new Reader with the given base and overlay paths
func NewReader(baseLocation string, overlayFilePaths []string, embedCfg *fileutils.EmbedConfiguration, userDataFileReader io.Reader, allowMissingKeys bool, additionalVars map[string]any) (*Reader, error) {
	baseReader, err := fileutils.GetWorkloadReader(baseLocation, embedCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to read base configuration: %w", err)
	}
	return &Reader{
		BaseReader:         baseReader,
		OverlayFilePaths:   overlayFilePaths,
		EmbedCfg:           embedCfg,
		UserDataFileReader: userDataFileReader,
		AllowMissingKeys:   allowMissingKeys,
		AdditionalVars:     additionalVars,
	}, nil
}

// Read reads data from the base configuration and applies overlays
func (o *Reader) Read(p []byte) (n int, err error) {
	if o.processedContent != nil {
		n = copy(p, o.processedContent[o.readOffset:])
		o.readOffset += n
		if o.readOffset >= len(o.processedContent) {
			return n, io.EOF
		}
		return n, nil
	}
	inputData, err := config.GetInputData(o.UserDataFileReader, o.AdditionalVars)
	if err != nil {
		return 0, err
	}
	templateOptions := util.MissingKeyError
	if o.AllowMissingKeys {
		templateOptions = util.MissingKeyZero
	}
	baseContent, err := io.ReadAll(o.BaseReader)
	if err != nil {
		return 0, fmt.Errorf("failed to read base configuration: %w", err)
	}
	renderedBaseContent, err := util.RenderTemplate(baseContent, inputData, templateOptions, []string{})
	if err != nil {
		return 0, fmt.Errorf("failed to render base configuration with template data: %w", err)
	}
	for _, overlayPath := range o.OverlayFilePaths {
		overlayReader, err := fileutils.GetWorkloadReader(overlayPath, o.EmbedCfg)
		if err != nil {
			return 0, fmt.Errorf("failed to read overlay %s: %w", overlayPath, err)
		}
		overlayContent, err := io.ReadAll(overlayReader)
		if err != nil {
			return 0, fmt.Errorf("failed to read overlay %s: %w", overlayPath, err)
		}
		renderedOverlayContent, err := util.RenderTemplate(overlayContent, inputData, templateOptions, []string{})
		if err != nil {
			return 0, fmt.Errorf("failed to render overlay %s with template data: %w", overlayPath, err)
		}
		renderedBaseContent, err = overlay.MergeYAML(renderedBaseContent, renderedOverlayContent)
		if err != nil {
			return 0, fmt.Errorf("failed to merge overlay %s with base configuration: %w", overlayPath, err)
		}
	}
	o.processedContent = renderedBaseContent
	o.readOffset = 0
	return o.Read(p)
}
