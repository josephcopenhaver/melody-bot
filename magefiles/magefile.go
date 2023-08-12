//go:build mage
// +build mage

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/magefile/mage/mg" // mg contains helpful utility functions, like Deps
)

// init structured logging
//
//nolint:gochecknoinits
func init() {
	logLevel := slog.LevelInfo
	if s := strings.TrimSpace(os.Getenv("LOG_LEVEL")); s != "" {
		var v slog.Level
		if err := v.UnmarshalText([]byte(s)); err != nil {
			panic(err)
		}
		logLevel = v
	}

	var addSource bool
	if s := strings.TrimSpace(os.Getenv("LOG_SOURCE")); s != "" {
		v, err := strconv.ParseBool(s)
		if err != nil {
			panic(err)
		}
		addSource = v
	}

	var logHandler slog.Handler
	if s := strings.ToLower(strings.TrimSpace(os.Getenv("LOG_FORMAT"))); s == "json" {
		logHandler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			AddSource: addSource,
			Level:     logLevel,
		})
	} else {
		logHandler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			AddSource: addSource,
			Level:     logLevel,
		})
	}

	slog.SetDefault(slog.New(logHandler))
}

//
// START helpers tightly coupled to this project
//

func version(ctx context.Context) string {
	cmd := NewCmd().
		Fields("head -1 version.txt").
		CaptureOut()
	if err := cmd.Run(ctx); err != nil {
		panic(err)
	}

	return cmd.OutString()
}

type fileSigsSet struct {
	goModPre, goModPost fileSig
	goSumPre, goSumPost fileSig
}

var fileSigs fileSigsSet

func (fss *fileSigsSet) MarkPre(ctx context.Context) error {
	return fss.mark(ctx, &fss.goModPre, &fss.goSumPre)
}

func (fss *fileSigsSet) MarkPost(ctx context.Context) error {
	return fss.mark(ctx, &fss.goModPost, &fss.goSumPost)
}

func (fss *fileSigsSet) mark(_ context.Context, goMod, goSum *fileSig) error {
	if fname := "go.mod"; !fileObjExists(fname) {
		return errors.New("missing go.mod file")
	} else if err := goMod.ComputeSig(fname); err != nil {
		return err
	}

	if fname := "go.sum"; !fileObjExists(fname) {
		goSum.Set("")
	} else if err := goSum.ComputeSig(fname); err != nil {
		return err
	}

	return nil
}

func (fss *fileSigsSet) Validate(ctx context.Context) error {
	var errResp error

	if s := fss.goModPre.Get(); s == "" {
		errResp = errors.Join(errResp, errors.New("go.mod file did not exist before build or still does not exist"))
	} else if s2 := fss.goModPost.Get(); s != s2 {
		errResp = errors.Join(errResp, errors.New("go.mod file changed when resolving dependencies; you should run 'go mod vendor' and 'go mod tidy'"))
	}

	if s := fss.goSumPre.Get(); s == "" {
		errResp = errors.Join(errResp, errors.New("go.sum file did not exist before build or still does not exist"))
	} else if s2 := fss.goSumPost.Get(); s != s2 {
		errResp = errors.Join(errResp, errors.New("go.sum file changed when resolving dependencies; you should run 'go mod vendor' and 'go mod tidy'"))
	}

	if errResp != nil {
		ciStr := os.Getenv("CI")
		var ci bool
		if ciStr != "" {
			if ok, err := strconv.ParseBool(ciStr); err == nil && ok {
				ci = true
			}
		}

		if !ci {
			slog.WarnContext(ctx,
				"failed to sync dependencies",
				"error", errResp,
				"CI", ciStr,
			)
			return nil
		}

		slog.ErrorContext(ctx,
			"failed to sync dependencies",
			"error", errResp,
			"CI", ciStr,
		)

		return errResp
	}

	return nil
}

func baseComposeCmd() *Cmd {

	cwd := os.Getenv("PWD")
	if cwd == "" {
		panic(errors.New("PWD not defined"))
	}

	defaultLayers := []string{"networks", "default"}
	allLayers := defaultLayers

	var sb strings.Builder
	for _, v := range allLayers {

		if sb.Len() != 0 && len(v) > 0 {
			if _, err := sb.WriteRune(os.PathListSeparator); err != nil {
				panic(err)
			}
		}

		if _, err := sb.WriteString(cwd); err != nil {
			panic(err)
		}
		if _, err := sb.WriteString("/docker/"); err != nil {
			panic(err)
		}
		if _, err := sb.WriteString(v); err != nil {
			panic(err)
		}
		if _, err := sb.WriteString("/docker-compose.yml"); err != nil {
			panic(err)
		}
	}

	return NewCmd("docker-compose").
		AppendEnv("COMPOSE_FILE=" + sb.String())
}

func vars(ctx context.Context) {
	type defaultedVar struct {
		Name, Default string
		Resolver      func(context.Context) (string, error)
	}

	const projectName = "josephcopenhaver--melody-bot"
	const prefix = projectName + "--"

	pwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	vars := []defaultedVar{
		{"COMPOSE_DOCKER_CLI_BUILD", "1", nil},
		{"DOCKER_BUILDKIT", "1", nil},
		{"ENV", "empty", nil},
		{"NETWORK_PREFIX_INFRASTRUCTURE", prefix, nil},
		{"NETWORK_PREFIX_FRONTEND", prefix, nil},
		{"COMPOSE_PROJECT_NAME", projectName, nil},
		{"COMPOSE_IGNORE_ORPHANS", "false", nil},
		{"PWD", pwd, nil},
		{"OS", "", func(ctx context.Context) (string, error) {
			cmd := NewCmd("uname", "-s").CaptureOut()
			if err := cmd.Run(ctx); err != nil {
				return "", err
			}

			return cmd.OutString(strings.TrimSpace, strings.ToLower), nil
		}},
		{"ARCH", "", func(ctx context.Context) (string, error) {
			cmd := NewCmd("uname", "-m").CaptureOut()
			if err := cmd.Run(ctx); err != nil {
				return "", err
			}

			s := cmd.OutString(strings.TrimSpace, strings.ToLower)
			switch s {
			case "x86_64":
				return "amd64", nil
			case "aarch64":
				return "arm64", nil
			default:
				return s, nil
			}
		}},
		{"GOLANGCILINT_VERSION", "v1.54.1", nil},
		{"GOLANGCILINT_BIN", "", func(ctx context.Context) (string, error) {
			return filepath.Join("magefiles/cache/golangci-lint", fmt.Sprintf("%s-%s-%s", strings.TrimLeft(os.Getenv("GOLANGCILINT_VERSION"), "v"), os.Getenv("OS"), os.Getenv("ARCH")), "golangci-lint"), nil
		}},
		{"STATICCHECK_VERSION", "2023.1.3", nil},
		{"STATICCHECK_BIN", "", func(ctx context.Context) (string, error) {
			return filepath.Join("magefiles/cache/staticcheck", fmt.Sprintf("%s-%s-%s", os.Getenv("STATICCHECK_VERSION"), os.Getenv("OS"), os.Getenv("ARCH")), "staticcheck"), nil
		}},
	}

	for _, dv := range vars {
		if _, ok := os.LookupEnv(dv.Name); !ok {
			if f := dv.Resolver; f != nil {
				dv.Resolver = nil
				if v, err := f(ctx); err != nil {
					panic(fmt.Errorf("failed to resolve env value for %s: %w", dv.Name, err))
				} else {
					dv.Default = v
				}
			}
			if err := os.Setenv(dv.Name, dv.Default); err != nil {
				panic(fmt.Errorf("failed to set default value for %s to '%s': %w", dv.Name, dv.Default, err))
			}
		}
	}
}

func initSecrets(_ context.Context) error {

	if fname := filepath.Join("secrets", os.Getenv("ENV")+".env"); !fileObjExists(fname) {
		f, err := os.Create(fname)
		if err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}

	return nil
}

func down(ctx context.Context, removeVolumes bool) error {

	// TODO: teardown based on network membership and then services rather than just services
	// bc down fails if there is an active shell... feature or bug?

	composeArgs := []string{
		"down", "",
	}

	if removeVolumes {
		composeArgs[len(composeArgs)-1] = "-v"
	} else {
		composeArgs = composeArgs[:len(composeArgs)-1]
	}

	if err := baseComposeCmd().ReplaceArgs(composeArgs...).Run(ctx); err != nil {
		slog.ErrorContext(ctx,
			"command failed",
			"error", err,
		)
		return err
	}

	return nil
}

func shellComposeFileEnv(cwd string) string {
	return "COMPOSE_FILE=" + strings.Join([]string{
		filepath.Join(cwd, "docker/networks/docker-compose.yml"),
		filepath.Join(cwd, "docker/shell/docker-compose.yml"),
	}, string(os.PathListSeparator))
}

//
// END helpers tightly coupled to this project
//

//
// START TARGETS
//

func Build(ctx context.Context) error {
	mg.Deps(InstallDeps)

	slog.InfoContext(ctx,
		"building",
	)
	if err := NewCmd().Fields("mkdir -p build/bin").Run(ctx); err != nil {
		return err
	}

	// verify the version of mage being used matches everywhere
	cmd := NewCmd("bash", "-c", `set -euxo pipefail && go list -m github.com/magefile/mage | head -1 | sed -E 's/^.*\s+([^\s]+)\s*$/\1/'`).CaptureOut()
	if err := cmd.Run(ctx); err != nil {
		return err
	}

	if s := os.Getenv("MAGE_VERSION"); s == "" || cmd.OutString(strings.TrimSpace) != s {
		return errors.New("failed to verify mage version is set consistently")
	}

	cmd = NewCmd().
		Fields("go build -o build/bin -tags netgo -ldflags").
		Arg("-linkmode=external -extldflags=-static -X main.GitSHA=" + commitSha(ctx, "") + " -X main.Version=" + version(ctx)).
		Arg("./cmd/...").
		AppendEnvMap(map[string]string{
			"GOEXPERIMENT": "loopvar", // temp until standard in go1.22+ https://github.com/golang/go/wiki/LoopvarExperiment
			"CGO_CFLAGS":   "-O3",
			"CGO_ENABLED":  "1",
		})
	if err := cmd.Run(ctx); err != nil {
		return err
	}

	slog.InfoContext(ctx,
		"done building",
	)
	return nil
}

// with CGO deps, there is no "clean" way to install and manage them
// ref: https://github.com/golang/go/issues/26366
const JpcopeOpusVersion = "17c317f9c9e9545df42c4ffc0bb9252ee6261868"

func InstallDeps(ctx context.Context) error {
	slog.InfoContext(ctx,
		"installing deps",
	)

	// establish a baseline for files that should remain unchanged through dependency install process
	if err := fileSigs.MarkPre(ctx); err != nil {
		return err
	}

	cmd := NewCmd("go", "mod", "vendor")
	if err := cmd.Run(ctx); err != nil {
		return err
	}

	if fileObjExists("vendor/github.com/josephcopenhaver/gopus/.git") {
		return errors.New("vendor/github.com/josephcopenhaver/gopus/.git should not exist")
	}

	if !dirExists("vendor/github.com/josephcopenhaver/gopus") || !dirExists("vendor/github.com/josephcopenhaver/gopus/.git") || !dirExists("vendor/github.com/josephcopenhaver/gopus/opus-1.1.2") || commitSha(ctx, "gopus") != JpcopeOpusVersion {
		if !dirExists("vendor-ext/github.com/josephcopenhaver/gopus") || !dirExists("vendor-ext/github.com/josephcopenhaver/gopus/.git") || commitSha(ctx, "vendor-ext/github.com/josephcopenhaver/gopus") != JpcopeOpusVersion {
			if dirExists("vendor-ext/github.com/josephcopenhaver") {
				if err := os.RemoveAll("vendor-ext/github.com/josephcopenhaver/gopus"); err != nil {
					return err
				}
			}

			// install git repo using a specific commit only
			if err := NewCmd("bash", "-c", "set -euxo pipefail && mkdir -p vendor-ext/github.com/josephcopenhaver/gopus && cd vendor-ext/github.com/josephcopenhaver/gopus && git init && git remote add origin https://github.com/josephcopenhaver/gopus.git && git fetch origin "+JpcopeOpusVersion+" && git reset --hard FETCH_HEAD").Run(ctx); err != nil {
				return err
			}
		}

		slog.InfoContext(ctx,
			"rsync running",
			"dst", "vendor-ext/github.com/josephcopenhaver/gopus",
		)

		if err := NewCmd("rsync", "-a", "--delete", "vendor-ext/github.com/josephcopenhaver/gopus/", "vendor/github.com/josephcopenhaver/gopus/").Run(ctx); err != nil {
			return err
		}
	}

	if err := fileSigs.MarkPost(ctx); err != nil {
		return err
	}

	if err := fileSigs.Validate(ctx); err != nil {
		return err
	}

	slog.InfoContext(ctx,
		"done installing deps",
	)
	return nil
}

func Clean(ctx context.Context) error {
	mg.Deps(vars)

	slog.InfoContext(ctx,
		"cleaning",
	)
	if err := os.RemoveAll("build"); err != nil {
		return err
	}

	if os.Getenv("IN_DOCKER_CONTAINER") != "" {
		return nil
	}

	if err := down(ctx, true); err != nil {
		return err
	}

	if err := os.RemoveAll(".docker-volumes"); err != nil {
		return err
	}

	slog.InfoContext(ctx,
		"done cleaning",
	)
	return nil
}

func Up(ctx context.Context) error {
	mg.Deps(vars)

	if s := os.Getenv("ENV"); s == "" || s == "empty" {
		if err := os.Setenv("ENV", "test"); err != nil {
			return err
		}
	}

	if err := initSecrets(ctx); err != nil {
		return err
	}

	baseCmd := baseComposeCmd()

	if v, err := strconv.ParseBool(os.Getenv("NOBUILD")); err != nil || !v {
		if err := baseCmd.Clone().ReplaceArgs("build").Run(ctx); err != nil {
			return err
		}
	}

	if err := baseCmd.Clone().ReplaceArgs("up", "-d").Run(ctx); err != nil {
		slog.ErrorContext(ctx,
			"command failed",
			"error", err,
		)
		return err
	}

	return nil
}

func Down(ctx context.Context) error {
	mg.Deps(vars)

	return down(ctx, os.Getenv("REMOVE_VOLUMES") != "")
}

func BuildAllImages(ctx context.Context) error {
	mg.Deps(vars)

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// ensure deterministic image build order by referencing the layers build image order file
	f, err := os.Open("docker/layers")
	if err != nil {
		return err
	}
	defer f.Close()

	baseComposeFiles := []string{
		filepath.Join(cwd, "docker/networks/docker-compose.yml"),
	}

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		layer := strings.TrimSpace(sc.Text())
		if layer == "" {
			continue
		}

		composeFile := filepath.Join(cwd, "docker", layer, "docker-compose.yml")

		cmd := NewCmd().
			Fields("docker-compose build").
			AppendEnvMap(map[string]string{
				"COMPOSE_FILE": strings.Join(append(append([]string(nil), baseComposeFiles...), composeFile), string(os.PathListSeparator)),
			})

		if err := cmd.Run(ctx); err != nil {
			return err
		}
	}

	return nil
}

func Shell(ctx context.Context) error {
	mg.Deps(vars)

	network := os.Getenv("NETWORK")
	if network == "" {
		network = "infrastructure"
	}

	switch network {
	case "infrastructure":
		network = os.Getenv("NETWORK_PREFIX_FRONTEND") + network
	case "frontend":
		network = os.Getenv("NETWORK_PREFIX_INFRASTRUCTURE") + network
	default:
		return errors.New("invalid network selection")
	}

	cwd := os.Getenv("PWD")

	// init shell files
	{
		if fname := ".devcontainer/cache/bash_history"; !fileObjExists(fname) {
			f, err := os.Create(fname)
			if err != nil {
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}

		if fname := ".devcontainer/cache/bashrc.local"; !fileObjExists(fname) {
			f, err := os.Create(fname)
			if err != nil {
				return err
			}
			cleanup := func() {
				f.Close()
			}
			defer func() {
				if f := cleanup; f != nil {
					cleanup = nil
					f()
				}
			}()

			if _, err := f.WriteString("export PS1='`printf \"%02X\" $?`:\\w `git branch 2> /dev/null | grep -E \"^[*]\" | sed -E \"s/^\\* +([^ ]+) *$/(\\1) /\"`\\$ '\n"); err != nil {
				return err
			}

			cleanup = nil
			if err := f.Close(); err != nil {
				return err
			}
		}
	}

	composeFile := shellComposeFileEnv(cwd)
	baseCmd := NewCmd("docker-compose").AppendEnv(composeFile)

	if v, err := strconv.ParseBool(os.Getenv("NOBUILD")); err != nil || !v {
		if err := baseCmd.Clone().ReplaceArgs("build", "shell").Run(ctx); err != nil {
			return err
		}
	}

	// setting stdin is required to avoid "the input device is not a TTY" errors
	//
	// this command ensures that the networks are created, nothing more
	if err := baseCmd.Clone().ReplaceArgs().Fields("run --rm --entrypoint bash shell -c").Arg("exit 0").Stdin(os.Stdin).Run(ctx); err != nil {
		return err
	}

	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		panic(errors.New("HOME not defined"))
	}

	if err := NewCmd().Fields("mkdir -p").Arg(filepath.Join(homeDir, ".aws/cli/cache")).Run(ctx); err != nil {
		return err
	}

	if err := NewCmd().Fields("mkdir -p").Arg(filepath.Join(cwd, ".devcontainer/cache")).Run(ctx); err != nil {
		return err
	}

	gitsha := os.Getenv("GIT_SHA")
	if gitsha == "" {
		gitsha = "latest"
	}
	return NewCmd().
		Fields("docker run --rm -it").
		Args("--network", network).
		Args("--env-file", filepath.Join(cwd, ".devcontainer/env")).
		Fields("-e AWS_ACCESS_KEY_ID -e AWS_SECRET_ACCESS_KEY -e AWS_DEFAULT_REGION -e AWS_REGION").
		Fields("-e AWS_DEFAULT_OUTPUT -e AWS_PROFILE -e AWS_SDK_LOAD_CONFIG").
		Fields("-e IN_DOCKER_CONTAINER=true").
		Fields("-w /workspace").
		Args("-v", cwd+":/workspace").
		Args("-v", filepath.Join(homeDir, ".aws")+":/root/.aws:ro").
		Args("-v", filepath.Join(homeDir, ".aws/cli/cache")+":/root/.aws/cli/cache:rw").
		Args("-v", filepath.Join(homeDir, ".ssh")+":/root/.ssh:ro").
		Args("-v", filepath.Join(cwd, ".devcontainer/cache/bashrc.local")+":/root/.bashrc.local").
		Args("-v", filepath.Join(cwd, ".devcontainer/cache/bash_history")+":/root/.bash_history").
		Fields("--entrypoint bash").
		Arg("josephcopenhaver/melody-bot--shell:" + gitsha).
		AppendEnv(composeFile).Exec()
}

func Logs(ctx context.Context) error {
	mg.Deps(vars)

	args := strings.Fields("logs -f")
	if s := os.Getenv("SERVICES"); s != "" {
		args = append(args, strings.Fields(strings.TrimSpace(s))...)
	}

	if err := baseComposeCmd().ReplaceArgs(args...).Run(ctx); err != nil {
		slog.ErrorContext(ctx,
			"command failed",
			"error", err,
		)
		return err
	}

	return nil
}

func Test(ctx context.Context) error {

	const testCmd = `export GOEXPERIMENT='loopvar' && go test ./... && go test -race ./...`

	if os.Getenv("IN_DOCKER_CONTAINER") != "" {
		return NewCmd().Fields("bash -c").Arg(testCmd).Run(ctx)
	}

	mg.Deps(vars)

	cwd := os.Getenv("PWD")

	composeFile := shellComposeFileEnv(cwd)
	baseCmd := NewCmd("docker-compose").AppendEnv(composeFile)

	// setting stdin is required to avoid "the input device is not a TTY" errors
	//
	// this command ensures that the networks are created, nothing more
	if err := baseCmd.Clone().ReplaceArgs().Fields("run --rm --entrypoint bash shell -c").Arg("exit 0").Stdin(os.Stdin).Run(ctx); err != nil {
		return err
	}

	if err := os.Setenv("ENV", "test"); err != nil {
		return err
	}

	// TODO: specify test env file and network

	gitsha := os.Getenv("GIT_SHA")
	if gitsha == "" {
		gitsha = "latest"
	}
	return NewCmd().
		Fields("docker run --rm").
		Fields("-e IN_DOCKER_CONTAINER=true").
		Fields("-w /workspace").
		Args("-v", cwd+":/workspace").
		Fields("--entrypoint bash").
		Arg("josephcopenhaver/melody-bot--shell:" + gitsha).
		Arg("-c").
		Arg("mage test").
		AppendEnv(composeFile).Run(ctx)
}

//nolint:gocyclo
func installLinter(ctx context.Context) error {

	mg.Deps(vars)

	const rootCacheDir = "./magefiles/cache"

	// install golangci-lint
	{
		lintCacheDir := filepath.Join(rootCacheDir, "golangci-lint")
		if err := os.MkdirAll(lintCacheDir, 0775); err != nil {
			return err
		}

		dstBin := os.Getenv("GOLANGCILINT_BIN")
		dstBinDir := filepath.Dir(dstBin)
		verOsPlat := filepath.Base(dstBinDir)

		// example url: https://github.com/golangci/golangci-lint/releases/download/v1.54.1/golangci-lint-1.54.1-freebsd-amd64.tar.gz

		fname := fmt.Sprintf("golangci-lint-%s.tar.gz", verOsPlat)
		dstFile := filepath.Join(lintCacheDir, fname)
		if !fileObjExists(dstFile) {
			if fileObjExists(dstFile + ".tmp") {
				if err := os.Remove(dstFile + ".tmp"); err != nil {
					return err
				}
			}

			url := fmt.Sprintf("https://github.com/golangci/golangci-lint/releases/download/v%s/%s", strings.TrimLeft(os.Getenv("GOLANGCILINT_VERSION"), "v"), fname)
			if err := NewCmd("curl", "-fsSL", url, "-o", dstFile+".tmp").Run(ctx); err != nil {
				return err
			}

			if err := os.Rename(dstFile+".tmp", dstFile); err != nil {
				return err
			}
		}

		if !fileObjExists(dstBin) {
			decompressedDir := filepath.Join(filepath.Dir(dstBinDir), "golangci-lint-"+filepath.Base(dstBinDir))

			if dirExists(decompressedDir) {
				if err := os.RemoveAll(decompressedDir); err != nil {
					return err
				}
			}

			if err := NewCmd("tar", "-xf", dstFile, "-C", lintCacheDir).Run(ctx); err != nil {
				return err
			}

			if err := os.Rename(decompressedDir, dstBinDir); err != nil {
				return err
			}

			if err := os.Remove(dstFile); err != nil {
				return err
			}
		}

		if err := NewCmd(os.Getenv("GOLANGCILINT_BIN"), "version").Run(ctx); err != nil {
			slog.ErrorContext(ctx,
				"command failed",
				"error", err,
			)
			return err
		}
	}

	// install staticcheck
	//
	// example url: https://github.com/dominikh/go-tools/releases/download/2023.1.3/staticcheck_darwin_arm64.tar.gz
	{

		lintCacheDir := filepath.Join(rootCacheDir, "staticcheck")
		if err := os.MkdirAll(lintCacheDir, 0775); err != nil {
			return err
		}

		dstBin := os.Getenv("STATICCHECK_BIN")
		dstBinDir := filepath.Dir(dstBin)
		verOsPlat := filepath.Base(dstBinDir)

		fname := fmt.Sprintf(verOsPlat + ".tar.gz")
		dstFile := filepath.Join(lintCacheDir, fname)
		if !fileObjExists(dstFile) {
			if fileObjExists(dstFile + ".tmp") {
				if err := os.Remove(dstFile + ".tmp"); err != nil {
					return err
				}
			}

			url := fmt.Sprintf("https://github.com/dominikh/go-tools/releases/download/%s/staticcheck_%s_%s.tar.gz", os.Getenv("STATICCHECK_VERSION"), os.Getenv("OS"), os.Getenv("ARCH"))
			if err := NewCmd("curl", "-fsSL", url, "-o", dstFile+".tmp").Run(ctx); err != nil {
				return err
			}

			if err := os.Rename(dstFile+".tmp", dstFile); err != nil {
				return err
			}
		}

		if !fileObjExists(dstBin) {
			decompressedDir := filepath.Join(filepath.Dir(dstBinDir), "staticcheck")

			if dirExists(decompressedDir) {
				if err := os.RemoveAll(decompressedDir); err != nil {
					return err
				}
			}

			if err := NewCmd("tar", "-xf", dstFile, "-C", lintCacheDir).Run(ctx); err != nil {
				return err
			}

			if err := os.Rename(decompressedDir, dstBinDir); err != nil {
				return err
			}

			if err := os.Remove(dstFile); err != nil {
				return err
			}
		}

		if err := NewCmd(os.Getenv("STATICCHECK_BIN"), "-version").Run(ctx); err != nil {
			slog.ErrorContext(ctx,
				"command failed",
				"error", err,
			)
			return err
		}
	}

	return nil
}

func Lint(ctx context.Context) error {
	mg.Deps(installLinter)

	if err := NewCmd(os.Getenv("GOLANGCILINT_BIN"), "run", "--skip-dirs", `^(?:vendor-ext)/.*`).Run(ctx); err != nil {
		slog.ErrorContext(ctx,
			"command failed",
			"error", err,
		)
		return err
	}

	// TODO: upgrade to version of staticcheck built with go1.21 and uncomment
	//
	// min|max|clear builtins are not recognized and linters seem to embed parts of the compiler language model instead of
	// referencing something that potentially exists in the installed version of go's language model
	//
	// if err := NewCmd(os.Getenv("STATICCHECK_BIN"), "-tags", "mage,unit,integration", "./...").Run(ctx); err != nil {
	// 	slog.ErrorContext(ctx,
	// 		"command failed",
	// 		"error", err,
	// 	)
	// 	return err
	// }

	return nil
}
