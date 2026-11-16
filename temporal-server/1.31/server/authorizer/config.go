package authorizer

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"gopkg.in/yaml.v3"
)

// Auth contains all the configuration and secrets required to perform
// authentication and authorization functionalities in the Temporal Server
// service. It is read from the top-level `auth` block of the server config,
// which upstream Temporal ignores.
type Auth struct {
	Enabled              bool                `yaml:"enabled"`
	OFGA                 AuthorizationConfig `yaml:"ofga"`
	AdminGroups          string              `yaml:"adminGroups"`
	OpenAccessNamespaces string              `yaml:"openAccessNamespaces"`
	GoogleClientID       string              `yaml:"googleClientID"`
}

// AuthorizationConfig holds the configuration required for communicating with
// the authorization service.
type AuthorizationConfig struct {
	// APIScheme is either "http" or "https".
	APIScheme string `yaml:"apiScheme"`
	// APIHost is the host URL of the authorization service minus the scheme.
	APIHost string `yaml:"apiHost"`
	// APIPort is the port on the host machine where the authorization service is
	// running.
	APIPort string `yaml:"apiPort"`
	// BearerToken is the token attached to the authorization header for requests
	// made to the OpenFGA store.
	BearerToken string `yaml:"token"`
	// StoreID is the ID of the auth store defined in the authorization service
	// that must be used for auth checks.
	StoreID string `yaml:"storeID"`
	// AuthModelID is the ID of the defined authorization model that is
	// currently being used for authorization checks.
	AuthModelID string `yaml:"authModelID"`
}

type authFile struct {
	Auth Auth `yaml:"auth"`
}

const (
	baseConfigFile = "base.yaml"
	enableTemplate = "enable-template"
	// Same limit upstream uses when looking for the enable-template marker.
	commentSearchLimit = 1024
)

// ConfigFiles returns the config files upstream's legacy loader reads for the
// given directory, environment and zone, in increasing order of precedence.
func ConfigFiles(configDir, env, zone string) ([]string, error) {
	candidates := []string{
		filepath.Join(configDir, baseConfigFile),
		filepath.Join(configDir, env+".yaml"),
	}
	if zone != "" {
		candidates = append(candidates, filepath.Join(configDir, env+"_"+zone+".yaml"))
	}

	var files []string
	for _, candidate := range candidates {
		_, err := os.Stat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("error accessing config file %s: %w", candidate, err)
		}
		files = append(files, candidate)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no config files found in directory: %s", configDir)
	}
	return files, nil
}

// LoadAuth reads the `auth` block from the given config files, applying them
// in order so that later files override earlier ones, as upstream does.
func LoadAuth(files []string) (*Auth, error) {
	cfg := authFile{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("could not read config file %s: %w", f, err)
		}
		data, err = renderTemplate(data, filepath.Base(f))
		if err != nil {
			return nil, fmt.Errorf("could not render config file %s: %w", f, err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("could not parse auth config in %s: %w", f, err)
		}
	}
	return &cfg.Auth, nil
}

// renderTemplate mirrors upstream's handling of "# enable-template" config files
// so the auth block sees the same rendered content as the rest of the config.
func renderTemplate(data []byte, name string) ([]byte, error) {
	scanner := bufio.NewScanner(io.LimitReader(bytes.NewReader(data), commentSearchLimit))
	templating := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") && strings.Contains(line, enableTemplate) {
			templating = true
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !templating {
		return data, nil
	}

	tpl, err := template.New(name).Funcs(sprig.FuncMap()).Parse(string(data))
	if err != nil {
		return nil, err
	}
	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, nil); err != nil {
		return nil, err
	}
	return rendered.Bytes(), nil
}
