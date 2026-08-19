package config

import "testing"

func TestStandardPathsForSupportedPlatforms(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}

	tests := []struct {
		name        string
		platform    string
		home        string
		environment map[string]string
		configFile  string
		database    string
	}{
		{
			name: "linux XDG", platform: "linux", home: "/home/test",
			environment: map[string]string{"XDG_CONFIG_HOME": "/config", "XDG_DATA_HOME": "/data"},
			configFile:  "/config/agent-context-probe/config.yaml",
			database:    "/data/agent-context-probe/requests.db",
		},
		{
			name: "macOS", platform: "darwin", home: "/Users/test",
			environment: map[string]string{},
			configFile:  "/Users/test/Library/Application Support/Agent Context Probe/config.yaml",
			database:    "/Users/test/Library/Application Support/Agent Context Probe/requests.db",
		},
		{
			name: "Windows", platform: "windows", home: `C:\Users\test`,
			environment: map[string]string{"APPDATA": `C:\Users\test\AppData\Roaming`, "LOCALAPPDATA": `C:\Users\test\AppData\Local`},
			configFile:  `C:\Users\test\AppData\Roaming\Agent Context Probe\config.yaml`,
			database:    `C:\Users\test\AppData\Local\Agent Context Probe\requests.db`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths, err := standardPathsFor(test.platform, test.home, env(test.environment))
			if err != nil {
				t.Fatalf("resolve paths: %v", err)
			}
			if paths.ConfigFile != test.configFile || paths.Database != test.database {
				t.Fatalf("unexpected paths: %+v", paths)
			}
		})
	}
}

func TestRelativeXDGPathsAreIgnored(t *testing.T) {
	paths, err := standardPathsFor("linux", "/home/test", func(key string) string {
		return map[string]string{"XDG_CONFIG_HOME": "relative-config", "XDG_DATA_HOME": "relative-data"}[key]
	})
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	if paths.ConfigFile != "/home/test/.config/agent-context-probe/config.yaml" {
		t.Fatalf("unexpected config fallback: %s", paths.ConfigFile)
	}
	if paths.Database != "/home/test/.local/share/agent-context-probe/requests.db" {
		t.Fatalf("unexpected data fallback: %s", paths.Database)
	}
}
