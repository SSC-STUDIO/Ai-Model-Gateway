package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ai-model-gateway/internal/release"
	"ai-model-gateway/internal/updater"
)

func runBundle(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: aigw bundle <build|verify>")
	}
	switch args[0] {
	case "build":
		fs := flag.NewFlagSet("bundle build", flag.ContinueOnError)
		root := fs.String("root", ".", "bundle root")
		out := fs.String("out", filepath.Join(".", release.ManifestFileName), "manifest output path")
		gitCommit := fs.String("git-commit", "", "git commit")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		manifest, err := release.BuildManifest(release.BuildOptions{
			Root:      *root,
			GitCommit: firstNonEmpty(*gitCommit, gitCommitFromGit(*root)),
		})
		if err != nil {
			return err
		}
		if err := release.SaveManifest(*out, manifest); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "manifest written to %s\n", *out)
		return nil
	case "verify":
		fs := flag.NewFlagSet("bundle verify", flag.ContinueOnError)
		root := fs.String("root", ".", "bundle root")
		manifestPath := fs.String("manifest", filepath.Join(".", release.ManifestFileName), "manifest path")
		format := fs.String("format", "text", "output format (text|json)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		manifest, err := release.LoadManifest(*manifestPath)
		if err != nil {
			return err
		}
		report := release.VerifyManifest(*root, manifest)
		if err := writeOutput(stdout, *format, report, func() error {
			if report.OK {
				fmt.Fprintln(stdout, "bundle verified")
			} else {
				fmt.Fprintln(stdout, "bundle verification failed")
				for _, issue := range report.Issues {
					fmt.Fprintf(stdout, "- %s\n", issue)
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if !report.OK {
			return fmt.Errorf("bundle verification failed")
		}
		return nil
	default:
		return fmt.Errorf("unknown bundle subcommand: %s", args[0])
	}
}

func runUpdate(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: aigw update <check|fetch|apply|rollback>")
	}
	switch args[0] {
	case "check":
		fs := flag.NewFlagSet("update check", flag.ContinueOnError)
		repository := fs.String("repo", updater.DefaultRepository, "GitHub repository owner/name")
		apiBaseURL := fs.String("api-base-url", updater.DefaultAPIBaseURL, "GitHub API base URL")
		platform := fs.String("platform", "", "platform override, defaults to GOOS/GOARCH")
		format := fs.String("format", "text", "output format (text|json)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, err := updater.CheckLatest(context.Background(), updater.Options{
			CurrentVersion: Version,
			Repository:     *repository,
			APIBaseURL:     *apiBaseURL,
			Platform:       *platform,
		})
		if err != nil {
			return err
		}
		return writeOutput(stdout, *format, result, func() error {
			if result.UpdateAvailable {
				fmt.Fprintf(stdout, "update available: %s -> %s\n", result.CurrentVersion, result.LatestVersion)
			} else {
				fmt.Fprintf(stdout, "already current: %s\n", result.CurrentVersion)
			}
			if result.AssetName != "" {
				fmt.Fprintf(stdout, "asset: %s\n", result.AssetName)
			}
			if result.ReleaseURL != "" {
				fmt.Fprintf(stdout, "release: %s\n", result.ReleaseURL)
			}
			if result.Message != "" {
				fmt.Fprintf(stdout, "message: %s\n", result.Message)
			}
			return nil
		})
	case "fetch":
		fs := flag.NewFlagSet("update fetch", flag.ContinueOnError)
		repository := fs.String("repo", updater.DefaultRepository, "GitHub repository owner/name")
		apiBaseURL := fs.String("api-base-url", updater.DefaultAPIBaseURL, "GitHub API base URL")
		platform := fs.String("platform", "", "platform override, defaults to GOOS/GOARCH")
		outDir := fs.String("out", filepath.Join(".gateway-runtime", "update", "downloads"), "download directory")
		force := fs.Bool("force", false, "download even when no newer release is available")
		format := fs.String("format", "text", "output format (text|json)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, err := updater.FetchLatest(context.Background(), updater.Options{
			CurrentVersion: Version,
			Repository:     *repository,
			APIBaseURL:     *apiBaseURL,
			Platform:       *platform,
			DownloadDir:    *outDir,
		}, *force)
		if err != nil {
			return err
		}
		return writeOutput(stdout, *format, result, func() error {
			fmt.Fprintf(stdout, "downloaded %s\n", result.AssetName)
			fmt.Fprintf(stdout, "archive: %s\n", result.ArchivePath)
			fmt.Fprintf(stdout, "bundle: %s\n", result.BundleDir)
			fmt.Fprintf(stdout, "version: %s\n", result.Manifest.ProductVersion)
			fmt.Fprintf(stdout, "verify: %v\n", result.Verify.OK)
			return nil
		})
	case "apply":
		fs := flag.NewFlagSet("update apply", flag.ContinueOnError)
		bundleRoot := fs.String("bundle", "", "bundle root")
		installDir := fs.String("install-dir", ".", "install directory")
		stateDir := fs.String("state-dir", filepath.Join(".gateway-runtime", "update"), "update state directory")
		dryRun := fs.Bool("dry-run", false, "show the plan without copying files")
		format := fs.String("format", "text", "output format (text|json)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*bundleRoot) == "" {
			return fmt.Errorf("-bundle is required")
		}
		result, err := updater.ApplyBundle(updater.ApplyOptions{
			BundleRoot: *bundleRoot,
			InstallDir: *installDir,
			StateDir:   *stateDir,
			DryRun:     *dryRun,
		})
		if err != nil {
			return err
		}
		return writeOutput(stdout, *format, result, func() error {
			fmt.Fprintf(stdout, "preflight ok: bundle=%s version=%s\n", result.BundleRoot, result.ProductVersion)
			fmt.Fprintf(stdout, "backup target: %s\n", result.BackupDir)
			if result.DryRun {
				fmt.Fprintln(stdout, "dry-run: no files copied")
				return nil
			}
			fmt.Fprintln(stdout, "update applied")
			return nil
		})
	case "rollback":
		fs := flag.NewFlagSet("update rollback", flag.ContinueOnError)
		installDir := fs.String("install-dir", ".", "install directory")
		stateDir := fs.String("state-dir", filepath.Join(".gateway-runtime", "update"), "update state directory")
		format := fs.String("format", "text", "output format (text|json)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, err := updater.Rollback(updater.RollbackOptions{InstallDir: *installDir, StateDir: *stateDir})
		if err != nil {
			return err
		}
		return writeOutput(stdout, *format, result, func() error {
			fmt.Fprintf(stdout, "rolled back from %s\n", result.BackupDir)
			return nil
		})
	default:
		return fmt.Errorf("unknown update subcommand: %s", args[0])
	}
}

func copyPathIfExists(src string, dst string) error {
	if _, err := os.Stat(src); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return copyPath(src, dst)
}

func copyPath(src string, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(src, path)
			if err != nil {
				return err
			}
			target := filepath.Join(dst, rel)
			if d.IsDir() {
				return os.MkdirAll(target, 0755)
			}
			return copyFile(path, target)
		})
	}
	return copyFile(src, dst)
}

func copyFile(src string, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0644
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func backupInstallPayload(installDir string, backupDir string, manifest release.Manifest) error {
	paths := []string{release.ManifestFileName}
	for _, binary := range manifest.Binaries {
		paths = append(paths, binary.Path)
	}
	if strings.TrimSpace(manifest.AdminDistHash) != "" {
		paths = append(paths, filepath.ToSlash(filepath.Join("web", "admin", "dist")))
	}
	for _, rel := range paths {
		if err := copyPathIfExists(filepath.Join(installDir, filepath.FromSlash(rel)), filepath.Join(backupDir, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	return nil
}

func copyBundlePayload(bundleRoot string, installDir string, manifest release.Manifest) error {
	if err := copyFile(filepath.Join(bundleRoot, release.ManifestFileName), filepath.Join(installDir, release.ManifestFileName)); err != nil {
		return err
	}
	for _, binary := range manifest.Binaries {
		src := filepath.Join(bundleRoot, filepath.FromSlash(binary.Path))
		dst := filepath.Join(installDir, filepath.FromSlash(binary.Path))
		if err := copyFile(src, dst); err != nil {
			return err
		}
	}
	if strings.TrimSpace(manifest.AdminDistHash) != "" {
		src := filepath.Join(bundleRoot, "web", "admin", "dist")
		dst := filepath.Join(installDir, "web", "admin", "dist")
		if err := replaceDir(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func replaceDir(src string, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(dst)+".tmp-")
	if err != nil {
		return err
	}
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.RemoveAll(tmp)
		}
	}()
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := copyPath(src, tmp); err != nil {
		return err
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	cleanupTmp = false
	return nil
}

func restoreInstallPayload(backupDir string, installDir string) error {
	return filepath.WalkDir(backupDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(backupDir, path)
		if err != nil {
			return err
		}
		return copyFile(path, filepath.Join(installDir, rel))
	})
}

func latestBackup(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "", fmt.Errorf("no update backups found in %s", root)
	}
	return filepath.Join(root, names[len(names)-1]), nil
}

func gitCommitFromGit(root string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
