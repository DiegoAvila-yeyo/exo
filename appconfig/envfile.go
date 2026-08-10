package appconfig

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

func LoadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		if key == "" {
			continue
		}
		if os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, parts[1]); err != nil {
			return err
		}
	}

	return scanner.Err()
}
