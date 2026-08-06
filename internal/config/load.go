package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

// envVarRegex matches ${VAR_NAME} or $VAR_NAME patterns
var envVarRegex = regexp.MustCompile(`\$\{([^}]+)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// secretFileRegex matches @/path/to/file patterns for reading secrets from files
var secretFileRegex = regexp.MustCompile(`@/([^\s"']+)`)

// LoadFromFile reads a JSON configuration file and returns the parsed Config,
// starting from the defaults and overlaying the file contents.
// Environment variables in the form ${VAR_NAME} or $VAR_NAME are substituted.
// File references in the form @/path/to/file read the file content as the value.
func LoadFromFile(path string) (*Config, error) {
	// Check file permissions - config files with secrets should not be world-readable
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	mode := fi.Mode()
	if mode.Perm()&0o077 != 0 {
		return nil, fmt.Errorf("config file %s has insecure permissions (mode %o); should be 0600 or 0640", path, mode.Perm())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Substitute environment variables
	data = substituteEnvVars(data)

	// Substitute file-based secrets
	data = substituteSecretFiles(data, filepath.Dir(path))

	// Validate against JSON schema
	if err := validateSchema(data); err != nil {
		return nil, fmt.Errorf("config schema validation failed: %w", err)
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validateSchema validates the configuration against the JSON schema
func validateSchema(data []byte) error {
	// Load the schema from the embedded file or file system
	schemaLoader := gojsonschema.NewReferenceLoader("file://config.schema.json")
	documentLoader := gojsonschema.NewBytesLoader(data)

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return fmt.Errorf("schema validation error: %w", err)
	}

	if !result.Valid() {
		var errs []string
		for _, err := range result.Errors() {
			errs = append(errs, fmt.Sprintf("%s: %s", err.Field(), err.Description()))
		}
		return fmt.Errorf("schema validation failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

// substituteEnvVars replaces ${VAR_NAME} or $VAR_NAME with environment variable values
func substituteEnvVars(data []byte) []byte {
	return envVarRegex.ReplaceAllFunc(data, func(match []byte) []byte {
		matchStr := string(match)
		var varName string

		if strings.HasPrefix(matchStr, "${") && strings.HasSuffix(matchStr, "}") {
			varName = matchStr[2 : len(matchStr)-1]
		} else if strings.HasPrefix(matchStr, "$") {
			varName = matchStr[1:]
		}

		if value := os.Getenv(varName); value != "" {
			return []byte(value)
		}

		// Return original if env var not set
		return match
	})
}

// substituteSecretFiles replaces @/path/to/file with the file contents
// This supports Docker secrets and similar secret management systems
func substituteSecretFiles(data []byte, baseDir string) []byte {
	return secretFileRegex.ReplaceAllFunc(data, func(match []byte) []byte {
		matchStr := string(match)
		// Extract path from @/path/to/file
		filePath := matchStr[2:]

		// Handle relative paths relative to config file directory
		if !filepath.IsAbs(filePath) {
			filePath = filepath.Join(baseDir, filePath)
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			// Return original if file not found
			return match
		}

		// Trim whitespace (newlines, etc.)
		return []byte(strings.TrimSpace(string(content)))
	})
}
