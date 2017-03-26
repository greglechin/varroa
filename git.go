package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

const (
	git = "git"
)

var (
	existsCommand = []string{"rev-parse", "--is-inside-work-tree"}
	initCommand   = []string{"init", "--quiet"}
)

// Git is a very basic wrapper around git
// Using git2go would be better but requires libgit2 to be installed, which may not be an option on seedboxes
// This assumes git, however, is available. If not, NewGit will return nil.
type Git struct {
	user        string
	email       string
	path        string
	currentPath string
}

func NewGit(root, user, email string) *Git {
	// checking git is available
	_, err := exec.LookPath(git)
	if err != nil {
		logThis("Git is not available on this system", NORMAL)
		return nil
	}
	// check root is dir and exists
	if !DirectoryExists(root) {
		logThis("Git repository path does not exist", NORMAL)
		return nil
	}
	// remember current path
	path, err := os.Getwd()
	if err != nil {
		logThis("Cannot get current directory", NORMAL)
		return nil
	}
	return &Git{path: root, user: user, email: email, currentPath: path}
}

func (g *Git) goToRepositoryRoot() error {
	return os.Chdir(g.path)
}

func (g *Git) getBack() error {
	return os.Chdir(g.currentPath)
}

func (g *Git) Exists() bool {
	g.goToRepositoryRoot()
	defer g.getBack()

	cmdOut, err := exec.Command(git, existsCommand...).Output()
	if err != nil {
		return false
	}
	if strings.TrimSpace(string(cmdOut)) == "true" {
		return true
	}
	return false
}

func (g *Git) Init() error {
	g.goToRepositoryRoot()
	defer g.getBack()
	_, err := exec.Command(git, initCommand...).Output()
	if err != nil {
		return err
	}
	// set up identity
	_, err = exec.Command(git, "config", "user.name", g.user).Output()
	if err != nil {
		return err
	}
	_, err = exec.Command(git, "config", "user.email", g.email).Output()
	if err != nil {
		return err
	}
	return err
}

func (g *Git) Add(files ...string) error {
	g.goToRepositoryRoot()
	defer g.getBack()
	args := []string{"add"}
	args = append(args, files...)
	_, err := exec.Command(git, args...).Output()
	return err
}

func (g *Git) Commit(message string) error {
	g.goToRepositoryRoot()
	defer g.getBack()
	out, err := exec.Command(git, "commit", "-m", message).CombinedOutput()
	if err != nil {
		logThis("Error committing stats: "+string(out), NORMAL)
	}
	return err
}

func (g *Git) HasRemote(remote string) bool {
	g.goToRepositoryRoot()
	defer g.getBack()
	cmdOut, err := exec.Command(git, "remote").Output()
	if err != nil {
		return false
	}
	remotes := strings.Split(string(cmdOut), "\n")
	for _, r := range remotes {
		if r == remote {
			return true
		}
	}
	return false
}

func (g *Git) AddRemote(remoteName, remoteURL string) error {
	g.goToRepositoryRoot()
	defer g.getBack()
	_, err := exec.Command(git, "remote", "add", remoteName, remoteURL).Output()
	if err == nil {
		// activate credential storing
		credentials := fmt.Sprintf("%s/.git-credentials", g.currentPath)
		if _, cmdErr := exec.Command(git, "config", "credential.helper", fmt.Sprintf("store --file=%s", credentials)).CombinedOutput(); cmdErr != nil {
			return cmdErr
		}
	}
	return err
}

func (g *Git) Push(remoteName, remoteURL, remoteUser, remotePassword string) error {
	if !g.HasRemote(remoteName) {
		return errors.New("Unknown remote: " + remoteName)
	}
	g.goToRepositoryRoot()
	defer g.getBack()

	// write its contents: https://user:pass@example.com
	credentials := fmt.Sprintf("%s/.git-credentials", g.currentPath)
	fullURL := fmt.Sprintf("https://%s:%s@%s", url.PathEscape(remoteUser), url.PathEscape(remotePassword), strings.Replace(remoteURL, "https://", "", -1))
	if err := ioutil.WriteFile(credentials, []byte(fullURL), 0700); err != nil {
		return err
	}
	defer os.Remove(credentials)

	out, err := exec.Command(git, "push", remoteName, "master").CombinedOutput()
	if err != nil {
		logThis("Error pushing stats to gitlab-pages: "+string(out), NORMAL)
	}
	return err
}
