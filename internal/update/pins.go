package update

import (
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// Pin is one service's image as the stack's compose file declares it. Comparing
// it with the running container's image shows a stack that has been updated on
// disk but not yet brought up.
type Pin struct {
	Project   string `json:"project"`
	Service   string `json:"service"`
	Container string `json:"container"`
	Declared  string `json:"declared"`
}

// ReadPins parses a compose file's services.*.image. Unresolved ${...}
// references are returned verbatim; there are none in the mesh stacks today.
func ReadPins(composePath string) ([]Pin, error) {
	b, err := os.ReadFile(composePath)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Name     string `yaml:"name"`
		Services map[string]struct {
			Image         string `yaml:"image"`
			ContainerName string `yaml:"container_name"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	project := doc.Name
	if project == "" {
		project = filepath.Base(filepath.Dir(composePath))
	}
	var pins []Pin
	for name, svc := range doc.Services {
		if svc.Image == "" {
			continue
		}
		pins = append(pins, Pin{Project: project, Service: name, Container: svc.ContainerName, Declared: svc.Image})
	}
	sort.Slice(pins, func(i, j int) bool { return pins[i].Service < pins[j].Service })
	return pins, nil
}
