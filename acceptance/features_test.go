package acceptance

import (
	"os"
	"testing"

	"github.com/cucumber/godog"
)

func TestFeatures(t *testing.T) {
	opts := godog.Options{
		Format:   "pretty",
		Paths:    []string{"features"},
		TestingT: t,
	}

	status := godog.TestSuite{
		Name:                "Lambda Bridge BDD",
		ScenarioInitializer: InitializeScenario,
		Options:             &opts,
	}.Run()

	if status != 0 {
		t.Fatalf("BDD tests failed with status %d", status)
	}
}

func TestMain(m *testing.M) {
	opts := godog.Options{
		Format: getFormatFromEnv(),
		Paths:  []string{"features"},
	}

	status := godog.TestSuite{
		Name:                "Lambda Bridge BDD",
		ScenarioInitializer: InitializeScenario,
		Options:             &opts,
	}.Run()

	os.Exit(status)
}

func getFormatFromEnv() string {
	format := os.Getenv("GODOG_FORMAT")
	if format == "" {
		format = "pretty"
	}
	return format
}
