//go:build !windows

package main

import (
	"context"
	"errors"
)

func runSupervisor(context.Context, workerConfig) error {
	return errors.New("ad-remote supervision is Windows-only")
}

func watchSupervisor(workerConfig) {}
