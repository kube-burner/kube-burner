package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kube-burner/kube-burner/pkg/util"
)

func main() {
	// Create the template data for rendering
	templateData := map[string]any{
		"Iteration": 1,
		"Replica":   1,
		"uuid":      "test-uuid",
		"runid":     "test-run",
	}

	// Read and render the CRD template
	crdBytes, err := os.ReadFile("../k8s/objectTemplates/crd-template.yml")
	if err != nil {
		fmt.Printf("Error reading CRD template: %v\n", err)
		os.Exit(1)
	}
	
	// Render the CRD template
	crdRendered, err := util.RenderTemplate(crdBytes, templateData, util.MissingKeyError, nil)
	if err != nil {
		fmt.Printf("Error rendering CRD template: %v\n", err)
		os.Exit(1)
	}

	// Read and render the CR template
	crBytes, err := os.ReadFile("../k8s/objectTemplates/cr-template.yml")
	if err != nil {
		fmt.Printf("Error reading CR template: %v\n", err)
		os.Exit(1)
	}
	
	// Render the CR template
	crRendered, err := util.RenderTemplate(crBytes, templateData, util.MissingKeyError, nil)
	if err != nil {
		fmt.Printf("Error rendering CR template: %v\n", err)
		os.Exit(1)
	}
	
	// Create output directory
	err = os.MkdirAll("rendered", 0755)
	if err != nil {
		fmt.Printf("Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	// Write rendered files
	err = os.WriteFile(filepath.Join("rendered", "crd.yaml"), crdRendered, 0644)
	if err != nil {
		fmt.Printf("Error writing rendered CRD: %v\n", err)
		os.Exit(1)
	}

	err = os.WriteFile(filepath.Join("rendered", "cr.yaml"), crRendered, 0644)
	if err != nil {
		fmt.Printf("Error writing rendered CR: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Successfully generated test YAML files in the 'rendered' directory")
}
