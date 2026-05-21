package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/aecsar/drift/drift"
)

var (
	defaultSchemaFilePath    = "./schema.sql"
	defaultMigrationsDirPath = "./migrations"
)

func main() {
	name := flag.String("name", "", "migration name (required)")
	schemaFile := flag.String(
		"schema", defaultSchemaFilePath, "path to schema file (default ./schema.sql)",
	)
	migrationsDir := flag.String(
		"migrations",
		defaultMigrationsDirPath,
		"path to migrations directory (default ./migrations)",
	)

	flag.Parse()

	if *name == "" {
		fmt.Fprintln(
			os.Stderr,
			"flag -name is required. Usage: migrate -name <migration_name>",
		)

		os.Exit(1)
	}

	if err := drift.Generate(*name, drift.Config{
		SchemaFile:    *schemaFile,
		MigrationsDir: *migrationsDir,
	}); err != nil {
		log.Fatal(err)
	}
}
