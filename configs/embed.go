package configs

import "embed"

// Embedded holds the environment configuration files that need to ship with the binary.
//
//go:embed config.dev.yml config.qa.yml config.prod.yml
var Embedded embed.FS
