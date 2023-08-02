//go:build mage
// +build mage

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/magefile/mage/mg" // mg contains helpful utility functions, like Deps
)

//
// START helpers tightly coupled to this project
//

func version(ctx context.Context) string {
	cmd := NewCmd(CmdB().Fields("head -1 version.txt").New()...).
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

func (fss *fileSigsSet) mark(ctx context.Context, goMod, goSum *fileSig) error {
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

func (fss *fileSigsSet) Validate() error {
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
			fmt.Println("WARNING: " + errResp.Error())
			return nil
		}

		return errResp
	}

	return nil
}

func baseComposeCmd() *Cmd {

	cwd := os.Getenv("PWD")
	if cwd == "" {
		panic(errors.New("PWD not defined"))
	}

	return NewCmd("docker-compose").
		AppendEnv("COMPOSE_FILE=" + strings.Join([]string{
			filepath.Join(cwd, "docker/networks/docker-compose.yml"),
			filepath.Join(cwd, "docker/default/docker-compose.yml"),
		}, string(os.PathListSeparator)))
}

func vars() {
	type defaultedVar struct {
		Name, Default string
	}

	const projectName = "josephcopenhaver--melody-bot"
	const prefix = projectName + "--"

	pwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	vars := []defaultedVar{
		{"COMPOSE_DOCKER_CLI_BUILD", "1"},
		{"DOCKER_BUILDKIT", "1"},
		{"ENV", "empty"},
		{"NETWORK_PREFIX_INFRASTRUCTURE", prefix},
		{"NETWORK_PREFIX_FRONTEND", prefix},
		{"COMPOSE_PROJECT_NAME", projectName},
		{"COMPOSE_IGNORE_ORPHANS", "false"},
		{"PWD", pwd},
	}

	for _, dv := range vars {
		if os.Getenv(dv.Name) == "" {
			if err := os.Setenv(dv.Name, dv.Default); err != nil {
				panic(fmt.Errorf("Failed to set default value for %s to '%s': %w", dv.Name, dv.Default, err))
			}
		}
	}
}

func initSecrets(ctx context.Context) error {

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

	composeArgs := []string{
		"down", "",
	}

	if removeVolumes {
		composeArgs[len(composeArgs)-1] = "-v"
	} else {
		composeArgs = composeArgs[:len(composeArgs)-1]
	}

	if err := baseComposeCmd().ReplaceArgs(composeArgs...).Run(ctx); err != nil {
		return err
	}

	return nil
}

//
// END helpers tightly coupled to this project
//

//
// START TARGETS
//

func Build(ctx context.Context) error {
	mg.Deps(InstallDeps)

	fmt.Println("Building...")
	if err := NewCmd(CmdB().Fields("mkdir -p build/bin").New()...).Run(ctx); err != nil {
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

	cmd = NewCmd(
		CmdB().
			Fields("go build -o build/bin -tags netgo -ldflags").
			Arg("-extldflags=-static -X main.GitSHA=" + commitSha(ctx, "") + " -X main.Version=" + version(ctx)).
			Arg("./cmd/...").
			New()...,
	).
		AppendEnvMap(map[string]string{
			"CGO_CFLAGS":  "-O3",
			"CGO_ENABLED": "1",
		})
	if err := cmd.Run(ctx); err != nil {
		return err
	}

	fmt.Println("Done Building")
	return nil
}

// with CGO deps, there is no "clean" way to install and manage them
// ref: https://github.com/golang/go/issues/26366
const JpcopeOpusVersion = "17c317f9c9e9545df42c4ffc0bb9252ee6261868"

func InstallDeps(ctx context.Context) error {
	fmt.Println("Installing Deps...")

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

		fmt.Println("rsync'ing vendor-ext/github.com/josephcopenhaver/gopus")

		if err := NewCmd("rsync", "-a", "--delete", "vendor-ext/github.com/josephcopenhaver/gopus/", "vendor/github.com/josephcopenhaver/gopus/").Run(ctx); err != nil {
			return err
		}
	}

	if err := fileSigs.MarkPost(ctx); err != nil {
		return err
	}

	if err := fileSigs.Validate(); err != nil {
		return err
	}

	fmt.Println("Done Installing Deps")
	return nil
}

func Clean(ctx context.Context) error {
	mg.Deps(vars)

	fmt.Println("Cleaning...")
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

	fmt.Println("Done Cleaning")
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

	if err := baseCmd.Clone().ReplaceArgs("build").Run(ctx); err != nil {
		return err
	}

	if err := baseCmd.Clone().ReplaceArgs("up", "-d").Run(ctx); err != nil {
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

		cmd := NewCmd(CmdB().Fields("docker-compose build").New()...).
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

	composeFile := "COMPOSE_FILE=" + strings.Join([]string{
		filepath.Join(cwd, "docker/networks/docker-compose.yml"),
		filepath.Join(cwd, "docker/shell/docker-compose.yml"),
	}, string(os.PathListSeparator))

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
			cleanup := f.Close
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

	baseCmd := NewCmd("docker-compose").AppendEnv(composeFile)

	if err := baseCmd.Clone().ReplaceArgs("build", "shell").Run(ctx); err != nil {
		return err
	}

	// setting stdin is required to avoid "the input device is not a TTY" errors
	//
	// this command ensures that the networks are created, nothing more
	if err := baseCmd.Clone().ReplaceArgs(CmdB().Fields("run --rm --entrypoint bash shell -c").Arg("exit 0").New()...).Stdin(os.Stdin).Run(ctx); err != nil {
		return err
	}

	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		panic(errors.New("HOME not defined"))
	}

	if err := NewCmd(CmdB().Fields("mkdir -p").Arg(filepath.Join(homeDir, ".aws/cli/cache")).New()...).Run(ctx); err != nil {
		return err
	}

	if err := NewCmd(CmdB().Fields("mkdir -p").Arg(filepath.Join(cwd, ".devcontainer/cache")).New()...).Run(ctx); err != nil {
		return err
	}

	gitsha := os.Getenv("GIT_SHA")
	if gitsha == "" {
		gitsha = "latest"
	}
	return NewCmd(CmdB().
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
		Args("-v", filepath.Join(cwd, ".devcontainer/cache/go")+":/go").
		Args("-v", filepath.Join(cwd, ".devcontainer/cache/bashrc.local")+":/root/.bashrc.local").
		Args("-v", filepath.Join(cwd, ".devcontainer/cache/bash_history")+":/root/.bash_history").
		Fields("--entrypoint bash").
		Arg("josephcopenhaver/melody-bot--shell:" + gitsha).
		New()...).
		AppendEnv(composeFile).Exec()
}

func Logs(ctx context.Context) error {
	mg.Deps(vars)

	argsb := CmdB().Fields("logs -f")
	if s := os.Getenv("SERVICES"); s != "" {
		argsb.Fields(s)
	}

	if err := baseComposeCmd().ReplaceArgs(argsb.New()...).Run(ctx); err != nil {
		return err
	}

	return nil
}
