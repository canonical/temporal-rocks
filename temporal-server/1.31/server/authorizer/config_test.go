package authorizer_test

import (
	"os"
	"path/filepath"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/canonical/temporal-rocks/temporal-server/1.31/server/authorizer"
)

const charmAuthConfig = `
log:
  stdout: true
auth:
  enabled: true
  adminGroups: red,green
  openAccessNamespaces: open-ns
  googleClientID: client.apps.googleusercontent.com
  ofga:
    apiScheme: http
    apiHost: "openfga-k8s.model.svc.cluster.local"
    apiPort: "8080"
    token: "secret"
    storeID: "store"
    authModelID: "model"
`

func writeFile(c *qt.C, dir, name, content string) string {
	path := filepath.Join(dir, name)
	c.Assert(os.WriteFile(path, []byte(content), 0o600), qt.IsNil)
	return path
}

func TestLoadAuth(t *testing.T) {
	c := qt.New(t)

	tests := []struct {
		desc     string
		files    map[string]string
		env      string
		zone     string
		expected authorizer.Auth
	}{{
		desc:  "charm config with auth enabled",
		files: map[string]string{"charm.yaml": charmAuthConfig},
		env:   "charm",
		expected: authorizer.Auth{
			Enabled:              true,
			AdminGroups:          "red,green",
			OpenAccessNamespaces: "open-ns",
			GoogleClientID:       "client.apps.googleusercontent.com",
			OFGA: authorizer.AuthorizationConfig{
				APIScheme:   "http",
				APIHost:     "openfga-k8s.model.svc.cluster.local",
				APIPort:     "8080",
				BearerToken: "secret",
				StoreID:     "store",
				AuthModelID: "model",
			},
		},
	}, {
		desc:     "config without auth block leaves auth disabled",
		files:    map[string]string{"charm.yaml": "log:\n  stdout: true\n"},
		env:      "charm",
		expected: authorizer.Auth{},
	}, {
		desc:     "auth explicitly disabled",
		files:    map[string]string{"charm.yaml": "auth:\n  enabled: false\n  adminGroups: \n"},
		env:      "charm",
		expected: authorizer.Auth{},
	}, {
		desc: "env file overrides base file",
		files: map[string]string{
			"base.yaml":  "auth:\n  enabled: false\n  adminGroups: base\n",
			"charm.yaml": "auth:\n  enabled: true\n",
		},
		env:      "charm",
		expected: authorizer.Auth{Enabled: true, AdminGroups: "base"},
	}, {
		desc: "zone file overrides env file",
		files: map[string]string{
			"charm.yaml":      "auth:\n  enabled: true\n",
			"charm_zone.yaml": "auth:\n  enabled: false\n",
		},
		env:      "charm",
		zone:     "zone",
		expected: authorizer.Auth{},
	}, {
		desc:     "templated config is rendered",
		files:    map[string]string{"charm.yaml": "# enable-template\nauth:\n  enabled: {{ \"true\" }}\n"},
		env:      "charm",
		expected: authorizer.Auth{Enabled: true},
	}}

	for _, test := range tests {
		c.Run(test.desc, func(c *qt.C) {
			dir := c.TempDir()
			for name, content := range test.files {
				writeFile(c, dir, name, content)
			}

			files, err := authorizer.ConfigFiles(dir, test.env, test.zone)
			c.Assert(err, qt.IsNil)
			auth, err := authorizer.LoadAuth(files)
			c.Assert(err, qt.IsNil)
			c.Assert(*auth, qt.DeepEquals, test.expected)
		})
	}
}

func TestConfigFilesMissing(t *testing.T) {
	c := qt.New(t)

	_, err := authorizer.ConfigFiles(c.TempDir(), "charm", "")
	c.Assert(err, qt.ErrorMatches, "no config files found in directory: .*")
}

func TestLoadAuthInvalidYAML(t *testing.T) {
	c := qt.New(t)

	path := writeFile(c, c.TempDir(), "charm.yaml", "auth:\n  enabled: [not-a-bool\n")
	_, err := authorizer.LoadAuth([]string{path})
	c.Assert(err, qt.ErrorMatches, "could not parse auth config in .*")
}
