package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// Each GitHub release of this repository is a release of the CLI. A test replaces the address and the program.
var (
	releases   = "https://github.com/ho-nl/deployyy/releases"
	executable = os.Executable
)

func download(ctx context.Context, address string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", address, response.Status)
	}
	return io.ReadAll(response.Body)
}

func updateCheckFile() (string, error) {
	dir, err := configDir()
	return filepath.Join(dir, "update-check"), err
}

// Starts `deployyy update` in the background, at most one time each day.
func startUpdate() {
	file, err := updateCheckFile()
	if err != nil || version == "dev" {
		return
	}
	if checked, err := os.Stat(file); err == nil && time.Since(checked.ModTime()) < 24*time.Hour {
		return
	}
	program, err := executable()
	if err != nil {
		return
	}
	update := exec.Command(program, "update")
	update.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	_ = update.Start()
}

func updateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Install the newest release",
		Long:  "Replaces the deployyy program with the newest release. Each other command starts this update in the background, at most one time each day.",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			ctx := command.Context()
			if file, err := updateCheckFile(); err == nil {
				_ = os.WriteFile(file, nil, 0o600)
			}

			// GitHub redirects /releases/latest to the page of the newest tag.
			request, err := http.NewRequestWithContext(ctx, http.MethodHead, releases+"/latest", nil)
			if err != nil {
				return err
			}
			response, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
			if err != nil {
				return err
			}
			response.Body.Close()
			latest := path.Base(response.Header.Get("Location"))
			if response.StatusCode != http.StatusFound || latest == "." {
				return fmt.Errorf("%s/latest answered %s", releases, response.Status)
			}
			if latest == version {
				fmt.Fprintf(os.Stderr, "deployyy %s is the newest release\n", version)
				return nil
			}

			name := fmt.Sprintf("deployyy-%s-%s", runtime.GOOS, runtime.GOARCH)
			program, err := download(ctx, fmt.Sprintf("%s/download/%s/%s", releases, latest, name))
			if err != nil {
				return err
			}
			sums, err := download(ctx, fmt.Sprintf("%s/download/%s/SHA256SUMS", releases, latest))
			if err != nil {
				return err
			}
			sum := sha256.Sum256(program)
			if !strings.Contains(string(sums), hex.EncodeToString(sum[:])+"  "+name+"\n") {
				return errors.New("The checksum of the download is wrong")
			}

			installed, err := executable()
			if err != nil {
				return err
			}
			if installed, err = filepath.EvalSymlinks(installed); err != nil {
				return err
			}
			// A rename in one folder replaces the program in one step.
			fresh := installed + ".update"
			if err := os.WriteFile(fresh, program, 0o755); err != nil {
				return err
			}
			if err := os.Rename(fresh, installed); err != nil {
				os.Remove(fresh)
				return err
			}
			fmt.Fprintf(os.Stderr, "Updated deployyy from %s to %s\n", version, latest)
			return nil
		},
	}
}
