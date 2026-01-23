package repository

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func RunMigrations(pool *pgxpool.Pool, dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return fmt.Errorf("failed to glob migration files: %w", err)
	}

	sort.Strings(files)

	for _, file := range files {
		log.Printf("Running migration: %s", file)
		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", file, err)
		}

		_, err = pool.Exec(context.Background(), string(content))
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				log.Printf("Migration %s already run or partially run: %v", file, err)
				continue
			}
			return fmt.Errorf("failed to execute migration %s: %w", file, err)
		}
	}

	return nil
}
