// ABOUTME: Resolves explicit PACT repositories and discovers safe ancestor candidates.
// ABOUTME: Stops at any .pact entry so store validation owns unsafe-path diagnostics.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func normalizeRepositoryArgs(args []string, mode repositoryMode, workingDir string) ([]string, error) {
	if mode == repositoryNone {
		return args, nil
	}
	result := append([]string(nil), args...)
	commandArgs := argumentsBeforeSentinel(result)
	for position, argument := range commandArgs {
		if argument == "--repo" {
			if position+1 == len(commandArgs) {
				return result, nil
			}
			path, err := resolveFromWorkingDir(workingDir, result[position+1])
			if err != nil {
				return nil, err
			}
			result[position+1] = path
			return result, nil
		}
		if value, found := strings.CutPrefix(argument, "--repo="); found {
			path, err := resolveFromWorkingDir(workingDir, value)
			if err != nil {
				return nil, err
			}
			result[position] = "--repo=" + path
			return result, nil
		}
	}
	if mode == repositoryCreate {
		return append([]string{"--repo", workingDir}, result...), nil
	}
	repo, err := discoverRepository(workingDir)
	if err != nil {
		return nil, err
	}
	return append([]string{"--repo", repo}, result...), nil
}

func resolveFromWorkingDir(workingDir, value string) (string, error) {
	if filepath.IsAbs(value) {
		return filepath.Clean(value), nil
	}
	if workingDir == "" {
		return "", fmt.Errorf("working directory is required")
	}
	return filepath.Clean(filepath.Join(workingDir, value)), nil
}

func discoverRepository(workingDir string) (string, error) {
	current, err := filepath.Abs(workingDir)
	if err != nil {
		return "", err
	}
	current, err = filepath.EvalSymlinks(current)
	if err != nil {
		return "", err
	}
	for {
		_, err := os.Lstat(filepath.Join(current, ".pact"))
		switch {
		case err == nil:
			return current, nil
		case !errors.Is(err, fs.ErrNotExist):
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return current, nil
		}
		current = parent
	}
}
