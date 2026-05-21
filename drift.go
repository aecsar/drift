package drift

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stripe/pg-schema-diff/pkg/diff"
	"github.com/stripe/pg-schema-diff/pkg/tempdb"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	dbName     = "migrate_main"
	dbUser     = "postgres"
	dbPassword = "password"

	dockerImage = "postgres:17-alpine"
)

type Config struct {
	SchemaFile    string
	MigrationsDir string
}

var config Config

func Generate(name string, cfg Config) error {
	config = cfg

	ctx := context.Background()

	container, connStr, err := startContainer(ctx)
	if err != nil {
		return err
	}
	defer terminateContainer(container)

	fmt.Println("Applying existing migrations...")
	if err := applyMigrations(ctx, connStr); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	fmt.Println("Reading target schema...")
	schemaSQL, err := os.ReadFile(config.SchemaFile)
	if err != nil {
		return fmt.Errorf("read schema file %#v: %w", config.SchemaFile, err)
	}

	fmt.Println("Generating migration diff...")
	migration, err := generateDiff(ctx, connStr, string(schemaSQL))
	if err != nil {
		return fmt.Errorf("generate diff: %w", err)
	}

	if migration == "" {
		fmt.Println("No changes detected. No migration file created.")
		return nil
	}

	outfile := filepath.Join(
		config.MigrationsDir,
		fmt.Sprintf("%s_%s.sql", time.Now().Format("20060102150405"), name),
	)

	if err := os.WriteFile(outfile, []byte(migration), 0644); err != nil {
		return fmt.Errorf("write migration file: %w", err)
	}

	fmt.Printf("Created: %s\n", outfile)
	return nil
}

func startContainer(ctx context.Context) (*postgres.PostgresContainer, string, error) {
	fmt.Println("Starting temporary PostgreSQL...")

	container, err := postgres.Run(
		ctx,
		dockerImage,
		postgres.WithDatabase(dbName),
		postgres.WithUsername(dbUser),
		postgres.WithPassword(dbPassword),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		return nil, "", fmt.Errorf("start container: %w", err)
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		terminateContainer(container)
		return nil, "", fmt.Errorf("build connection string: %w", err)
	}

	return container, connStr, nil
}

func terminateContainer(container *postgres.PostgresContainer) {
	if container == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := container.Terminate(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "failed to terminate temp postgres container: %v\n", err)
	}
}

func applyMigrations(ctx context.Context, connStr string) error {
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return fmt.Errorf("connect to temporary database: %w", err)
	}
	defer func() {
		if err := conn.Close(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "failed to close temporary database connection: %v\n", err)
		}
	}()

	entries, err := os.ReadDir(config.MigrationsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, filepath.Join(config.MigrationsDir, e.Name()))
		}
	}
	sort.Strings(files)

	for _, f := range files {
		fmt.Printf("  Applying: %s\n", f)
		sqlContent, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", f, err)
		}

		if _, err := conn.PgConn().Exec(ctx, string(sqlContent)).ReadAll(); err != nil {
			return fmt.Errorf("apply %s: %w", f, err)
		}
	}

	return nil
}

func generateDiff(ctx context.Context, connStr string, schemaSQL string) (string, error) {
	mainDB, err := openSQLDB(connStr)
	if err != nil {
		return "", fmt.Errorf("open main database: %w", err)
	}
	defer func() {
		if err := mainDB.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to close main database connection: %v\n", err)
		}
	}()

	connForDB := buildConnPoolFactory(connStr)

	tempDBFactory, err := tempdb.NewOnInstanceFactory(ctx, connForDB)
	if err != nil {
		return "", fmt.Errorf("create temp db factory: %w", err)
	}
	defer func() {
		if err := tempDBFactory.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to close temp database connection: %v\n", err)
		}
	}()

	plan, err := diff.Generate(ctx,
		diff.DBSchemaSource(mainDB),
		diff.DDLSchemaSource([]string{schemaSQL}),
		diff.WithTempDbFactory(tempDBFactory),
		diff.WithIncludeSchemas("public"),
		diff.WithNoConcurrentIndexOps(),
	)
	if err != nil {
		return "", fmt.Errorf("generate plan: %w", err)
	}

	if len(plan.Statements) == 0 {
		return "", nil
	}

	var sb strings.Builder
	for i, stmt := range plan.Statements {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(stmt.ToSQL())
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

func openSQLDB(connStr string) (*sql.DB, error) {
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	db.SetMaxOpenConns(5)
	return db, nil
}

func buildConnPoolFactory(baseConnStr string) tempdb.CreateConnPoolForDbFn {
	parsed, err := url.Parse(baseConnStr)
	if err != nil {
		panic(fmt.Sprintf("invalid base connection string: %v", err))
	}

	return func(_ context.Context, targetDB string) (*sql.DB, error) {
		target := *parsed
		target.Path = "/" + targetDB
		return openSQLDB(target.String())
	}
}
