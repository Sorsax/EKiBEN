package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type release struct {
	TagName string        `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func main() {
	var (
		repo        string
		assetName  string
		service    string
		targetDir  string
		logPath    string
		timeoutStr string
	)

	flag.StringVar(&repo, "repo", "Sorsax/EKiBEN", "github repo owner/name")
	flag.StringVar(&assetName, "asset", "ekiben-agent.zip", "release asset name")
	flag.StringVar(&service, "service", "EkibenAgent", "windows service name")
	flag.StringVar(&targetDir, "dir", "", "target install directory")
	flag.StringVar(&logPath, "log", "", "optional log file")
	flag.StringVar(&timeoutStr, "timeout", "60s", "http timeout")
	flag.Parse()

	if targetDir == "" {
		exe, err := os.Executable()
		if err != nil {
			die("resolve executable: %v", err)
		}
		targetDir = filepath.Dir(exe)
	}

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		die("create target dir: %v", err)
	}

	logger := newLogger(logPath)
	logger("starting updater for %s", repo)

	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		die("invalid timeout: %v", err)
	}

	assetURL, tag, err := latestAssetURL(repo, assetName, timeout)
	if err != nil {
		die("fetch latest release: %v", err)
	}
	logger("latest release: %s", tag)

	tmpDir, err := os.MkdirTemp("", "ekiben-agent-update-*")
	if err != nil {
		die("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	zipPath := filepath.Join(tmpDir, assetName)
	if err := downloadFile(assetURL, zipPath, timeout); err != nil {
		die("download asset: %v", err)
	}

	extractDir := filepath.Join(tmpDir, "extract")
	if err := unzip(zipPath, extractDir); err != nil {
		die("unzip asset: %v", err)
	}

	logger("stopping service %s", service)
	_ = exec.Command("sc.exe", "stop", service).Run()
	_ = exec.Command("sc.exe", "stop", service).Run()
	time.Sleep(2 * time.Second)

	err = copyDir(extractDir, targetDir, logger)
	if err != nil {
		die("copy files: %v", err)
	}

	logger("starting service %s", service)
	_ = exec.Command("sc.exe", "start", service).Run()

	logger("update complete")
}

func latestAssetURL(repo, assetName string, timeout time.Duration) (string, string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	client := &http.Client{Timeout: timeout}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "ekiben-agent-updater")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("github api status: %s", resp.Status)
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", err
	}

	for _, asset := range rel.Assets {
		if strings.EqualFold(asset.Name, assetName) {
			return asset.BrowserDownloadURL, rel.TagName, nil
		}
	}

	return "", rel.TagName, errors.New("asset not found in latest release")
}

func downloadFile(url, dest string, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ekiben-agent-updater")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download status: %s", resp.Status)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func unzip(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}

		cleanName := filepath.Clean(f.Name)
		if strings.Contains(cleanName, "..") || filepath.IsAbs(cleanName) {
			return fmt.Errorf("unsafe zip entry: %s", f.Name)
		}

		target := filepath.Join(dest, cleanName)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}

		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

func copyDir(src, dst string, logger func(string, ...any)) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.EqualFold(name, "agent.config.psd1") || strings.EqualFold(name, "traffic.log") {
			continue
		}
		if strings.EqualFold(name, "updater.exe") {
			// avoid overwriting the running updater; it can be updated next run
			continue
		}

		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)

		logger("copying %s", name)
		if err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func newLogger(path string) func(string, ...any) {
	if path == "" {
		return func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		}
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		}
	}

	return func(format string, args ...any) {
		_, _ = fmt.Fprintf(file, format+"\n", args...)
	}
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
