package version

import (
	"encoding/json"
	"fmt"
	"os"
)

// Эти переменные переопределяются при сборке через -ldflags.
var (
	Release   = "v0.0.1"
	BuildDate = "unknown"
	GitHash   = "dev"
)

// Info содержит информацию о версии сервиса.
type Info struct {
	Release   string `json:"release"`
	BuildDate string `json:"buildDate"`
	GitHash   string `json:"gitHash"`
}

// Print выводит информацию о версии в stdout в формате JSON.
func Print() {
	info := Info{
		Release:   Release,
		BuildDate: BuildDate,
		GitHash:   GitHash,
	}

	if err := json.NewEncoder(os.Stdout).Encode(info); err != nil {
		fmt.Fprintf(os.Stderr, "error while encoding version info: %v\n", err)
		os.Exit(1)
	}
}

// String возвращает строковое представление версии.
func String() string {
	return fmt.Sprintf("%s (built: %s, commit: %s)", Release, BuildDate, GitHash)
}
